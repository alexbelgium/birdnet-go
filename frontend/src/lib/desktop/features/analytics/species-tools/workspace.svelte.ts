/* eslint-disable security/detect-object-injection -- Membership and column keys are validated string unions. */
import { untrack } from 'svelte';
import { get } from 'svelte/store';
import { SvelteMap, SvelteSet } from 'svelte/reactivity';
import { fetchWithCSRF } from '$lib/utils/api';
import { settingsStore, type RangeFilterSpeciesEntry } from '$lib/stores/settings';
import { localizeSpeciesName } from '$lib/utils/speciesDisplay';
import { getLocale, t } from '$lib/i18n';
import { getStoredValue, setStoredValue } from '$lib/utils/storage';
import { loggers } from '$lib/utils/logger';
import {
  defaultColumns,
  isColumn,
  membershipNames,
  metricColumns,
  type Column,
  type Membership,
  type Recording,
  type SortKey,
  type SpeciesRow,
} from './types';

const endpoint = '/api/v2/analytics/species/tools';
const preferenceKey = 'species-tools-layout-v1';
const memberPaths = { confirmed: 'confirmed', included: 'included', excluded: 'ignored' };
const togglePaths = { confirmed: 'confirm', included: 'include', excluded: 'ignore' };
interface ReviewStat {
  scientificName: string;
  verified: number;
  rejected: number;
}
interface Preferences {
  columns: Column[];
  sort: SortKey;
  ascending: boolean;
}
const defaults: Preferences = { columns: defaultColumns, sort: 'count', ascending: false };
function validPreferences(value: unknown): value is Preferences {
  if (!value || typeof value !== 'object') return false;
  const candidate = value as Partial<Preferences>;
  return (
    Array.isArray(candidate.columns) &&
    candidate.columns.every(isColumn) &&
    new Set(candidate.columns).size === candidate.columns.length &&
    (candidate.sort === 'name' ||
      (isColumn(candidate.sort) && candidate.columns.includes(candidate.sort))) &&
    typeof candidate.ascending === 'boolean'
  );
}

/** Page-owned loaders: removing a column aborts its independent request group. */
export function createWorkspace() {
  let preferences = $state(getStoredValue(preferenceKey, defaults, validPreferences));
  let rows = $state<SpeciesRow[]>([]);
  let order = $state<string[]>([]);
  let members = $state<Record<Membership, string[]>>({ confirmed: [], included: [], excluded: [] });
  const reviews = new SvelteMap<string, ReviewStat>();
  const ranges = new SvelteMap<string, number>();
  const recordings = new SvelteMap<string, Recording | null>();
  const pending = new SvelteMap<string, boolean>();
  const errors = new SvelteMap<string, string>();
  const ready = new SvelteSet<string>();
  let sortPending = true;
  let interacting = false;
  const controllers = new Map<string, AbortController>();
  let selectedSpecies = '';

  async function request(group: string, work: (signal: AbortSignal) => Promise<() => void>) {
    if (controllers.has(group) || ready.has(group)) return;
    const controller = new AbortController();
    controllers.set(group, controller);
    pending.set(group, true);
    errors.delete(group);
    try {
      const commit = await work(controller.signal);
      if (!controller.signal.aborted) {
        commit();
        ready.add(group);
        if (sortPending && group === preferences.sort) applyPendingSort();
      }
    } catch (error) {
      if (!controller.signal.aborted) {
        errors.set(group, t('analytics.species.manage.loadFailed'));
        loggers.analytics.error('Species workspace request failed', { group, error });
      }
    } finally {
      if (controllers.get(group) === controller) {
        pending.delete(group);
        controllers.delete(group);
      }
    }
  }

  function member(kind: Membership, row: SpeciesRow): boolean | null {
    if (!ready.has(kind)) return null;
    return members[kind].some(name =>
      [row.scientific_name, row.common_name].some(
        alias => alias.toLowerCase() === name.toLowerCase()
      )
    );
  }
  function value(row: SpeciesRow, column: SortKey): number | string | null {
    if (column === 'name') return localizeSpeciesName(row.scientific_name, row.common_name);
    if (column === 'correct') {
      const stat = reviews.get(row.scientific_name);
      return stat && stat.verified + stat.rejected > 0
        ? stat.verified / (stat.verified + stat.rejected)
        : null;
    }
    if (column === 'range') return ranges.get(row.scientific_name) ?? null;
    if (column === 'best') return recordings.get(row.scientific_name)?.confidence ?? null;
    if (membershipNames.some(kind => kind === column)) {
      const result = member(column as Membership, row);
      return result === null ? null : Number(result);
    }
    return row[column as keyof SpeciesRow] ?? null;
  }
  function sortRows() {
    const key = preferences.sort;
    order = [...rows]
      .sort((a, b) => {
        const av = value(a, key),
          bv = value(b, key);
        if (av === null || bv === null)
          return av === bv
            ? a.scientific_name.localeCompare(b.scientific_name)
            : av === null
              ? 1
              : -1;
        const comparison =
          typeof av === 'number' && typeof bv === 'number'
            ? av - bv
            : String(av).localeCompare(String(bv), getLocale());
        return (
          (preferences.ascending ? 1 : -1) * comparison ||
          a.scientific_name.localeCompare(b.scientific_name)
        );
      })
      .map(row => row.scientific_name);
  }
  function applyPendingSort() {
    if (interacting || !sortPending) return;
    if (preferences.sort !== 'name' && !ready.has(preferences.sort)) return;
    sortRows();
    sortPending = false;
  }
  function setInteracting(active: boolean) {
    interacting = active;
    if (!active) applyPendingSort();
  }
  function sort(key: SortKey) {
    preferences = {
      ...preferences,
      sort: key,
      ascending: key === preferences.sort ? !preferences.ascending : key === 'name',
    };
    setStoredValue(preferenceKey, preferences);
    sortPending = true;
    // An explicit sort is intentional even while the pointer is over the table.
    if (key === 'name' || ready.has(key)) {
      sortRows();
      sortPending = false;
    }
  }
  function mergeMetrics(metrics: SpeciesRow[]) {
    const byName = new Map(metrics.map(row => [row.scientific_name, row]));
    rows = rows.map(row => ({ ...row, ...byName.get(row.scientific_name) }));
  }
  async function loadMembers(kind: Membership) {
    await request(kind, async signal => {
      const data = await fetchWithCSRF<{ species?: string[] }>(
        `/api/v2/detections/${memberPaths[kind]}`,
        { signal }
      );
      return () => {
        members = { ...members, [kind]: data.species ?? [] };
      };
    });
  }
  async function loadRecordings(names: string[]) {
    const missing = names.filter(name => !recordings.has(name));
    await request('best', async signal => {
      for (let i = 0; i < missing.length; i += 25) {
        const query = new URLSearchParams();
        missing.slice(i, i + 25).forEach(name => query.append('species', name));
        const data = await fetchWithCSRF<Record<string, Recording | null>>(
          `${endpoint}/recordings?${query}`,
          { signal }
        );
        if (signal.aborted) break;
        for (const [name, recording] of Object.entries(data)) recordings.set(name, recording);
      }
      return () => {};
    });
  }
  async function loadNeeded() {
    if (!ready.has('inventory')) return;
    const wanted = new Set<string>(preferences.columns);
    if (selectedSpecies) {
      membershipNames.forEach(kind => wanted.add(kind));
      metricColumns.forEach(column => wanted.add(column));
      wanted.add('best');
    }
    for (const [group, controller] of controllers) {
      if (group !== 'inventory' && !wanted.has(group)) {
        controller.abort();
        controllers.delete(group);
        pending.delete(group);
      }
    }
    const jobs: Promise<void>[] = [];
    for (const column of metricColumns)
      if (wanted.has(column))
        jobs.push(
          request(column, async signal => {
            const data = await fetchWithCSRF<SpeciesRow[]>(`${endpoint}?fields=${column}`, {
              signal,
            });
            return () => mergeMetrics(data);
          })
        );
    for (const kind of membershipNames) if (wanted.has(kind)) jobs.push(loadMembers(kind));
    if (wanted.has('correct'))
      jobs.push(
        request('correct', async signal => {
          const data = await fetchWithCSRF<ReviewStat[]>('/api/v2/analytics/species/review-stats', {
            signal,
          });
          return () => data.forEach(stat => reviews.set(stat.scientificName, stat));
        })
      );
    if (wanted.has('range'))
      jobs.push(
        request('range', async signal => {
          const birdnet = get(settingsStore).formData.birdnet;
          const data = await fetchWithCSRF<{ species?: RangeFilterSpeciesEntry[] }>(
            '/api/v2/range/species/test',
            {
              method: 'POST',
              signal,
              body: JSON.stringify({
                latitude: birdnet.latitude,
                longitude: birdnet.longitude,
                threshold: birdnet.rangeFilter.threshold,
              }),
            }
          );
          return () =>
            (data.species ?? []).forEach(row => {
              if (row.scientificName && typeof row.score === 'number')
                ranges.set(row.scientificName, row.score);
            });
        })
      );
    if (wanted.has('best')) {
      const names = preferences.columns.includes('best')
        ? rows.map(row => row.scientific_name)
        : [selectedSpecies];
      if (names.some(name => !recordings.has(name))) ready.delete('best');
      jobs.push(loadRecordings(names));
    }
    await Promise.all(jobs);
  }
  async function load() {
    await request('inventory', async signal => {
      const fields = metricColumns.filter(column => preferences.columns.includes(column));
      const data = await fetchWithCSRF<SpeciesRow[]>(`${endpoint}?fields=${fields.join(',')}`, {
        signal,
      });
      return () => {
        rows = data;
        fields.forEach(field => ready.add(field));
        if (!order.length) {
          order = data.map(row => row.scientific_name);
          applyPendingSort();
        } else {
          const existing = new Set(order);
          order = [
            ...order,
            ...data
              .filter(row => !existing.has(row.scientific_name))
              .map(row => row.scientific_name),
          ];
        }
      };
    });
    await loadNeeded();
  }
  function saveColumns(columns: Column[]) {
    preferences = {
      ...preferences,
      columns,
      sort:
        preferences.sort === 'name' || columns.includes(preferences.sort)
          ? preferences.sort
          : 'name',
    };
    setStoredValue(preferenceKey, preferences);
    sortPending = true;
    applyPendingSort();
    void loadNeeded();
  }
  function selectSpecies(name: string) {
    selectedSpecies = name;
    untrack(() => {
      void loadNeeded();
    });
  }
  async function deletionTarget(row: SpeciesRow): Promise<SpeciesRow> {
    if (row.count !== undefined) return row;
    const data = await fetchWithCSRF<SpeciesRow[]>(`${endpoint}?fields=count`);
    mergeMetrics(data);
    const target = data.find(candidate => candidate.scientific_name === row.scientific_name);
    if (target?.count === undefined) throw new Error(t('analytics.species.manage.loadFailed'));
    return { ...row, count: target.count };
  }
  async function toggle(kind: Membership, row: SpeciesRow) {
    await loadMembers(kind);
    if (member(kind, row) === null || pending.has(`toggle-${kind}`)) return;
    const old = members[kind];
    const entry = old.find(name =>
      [row.scientific_name, row.common_name].some(
        alias => name.toLowerCase() === alias.toLowerCase()
      )
    );
    pending.set(`toggle-${kind}`, true);
    errors.delete(kind);
    try {
      await fetchWithCSRF(`/api/v2/detections/${togglePaths[kind]}`, {
        method: 'POST',
        body: JSON.stringify({ common_name: entry ?? (row.common_name || row.scientific_name) }),
      });
      members = {
        ...members,
        [kind]: entry
          ? old.filter(name => name !== entry)
          : [...old, row.common_name || row.scientific_name],
      };
    } catch (error) {
      errors.set(kind, t('analytics.species.manage.membershipFailed'));
      loggers.analytics.error('Species membership update failed', error);
    } finally {
      pending.delete(`toggle-${kind}`);
    }
  }
  async function remove(row: SpeciesRow) {
    const skipped: string[] = [];
    for (let pass = 0; pass < 1000; pass++) {
      const data = await fetchWithCSRF<{ remaining: number; skipped_ids?: string[] }>(
        '/api/v2/detections/species/delete',
        {
          method: 'POST',
          body: JSON.stringify({ scientific_name: row.scientific_name, exclude_ids: skipped }),
        }
      );
      skipped.push(...(data.skipped_ids ?? []));
      if (!data.remaining) {
        await refresh();
        return;
      }
    }
    throw new Error(t('analytics.species.manage.deleteFailed'));
  }
  async function refresh() {
    controllers.forEach(controller => controller.abort());
    controllers.clear();
    pending.clear();
    ready.clear();
    recordings.clear();
    reviews.clear();
    ranges.clear();
    await load();
  }
  function dispose() {
    controllers.forEach(controller => controller.abort());
    controllers.clear();
  }
  return {
    get rows() {
      return rows;
    },
    get orderedRows() {
      const map = new Map(rows.map(row => [row.scientific_name, row]));
      return order.map(name => map.get(name)).filter((row): row is SpeciesRow => !!row);
    },
    get preferences() {
      return preferences;
    },
    pending,
    errors,
    recordings,
    reviews,
    value,
    member,
    loadMembers,
    load,
    sort,
    sortRows,
    setInteracting,
    deletionTarget,
    saveColumns,
    selectSpecies,
    toggle,
    remove,
    refresh,
    dispose,
    retry: loadNeeded,
  };
}
export type Workspace = ReturnType<typeof createWorkspace>;
