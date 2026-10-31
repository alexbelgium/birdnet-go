/**
 * Tiered data for the species workspace.
 *  T0 inventory: renders the table.
 *  T1 stats / memberships / range: loaded only for visible columns; membership
 *     toggles stay disabled until memberships load.
 *  T2 best recordings: loaded lazily for visible rows in serialized batches.
 * Every request is abortable; a newer request of a group replaces the one in flight.
 */
import { SvelteMap, SvelteSet } from 'svelte/reactivity';
import { invalidateSpeciesHistory } from '$lib/desktop/features/dashboard/components/daily-summary/SpeciesHistoryModal.svelte';
import { hydrateExcludedSpecies } from '$lib/stores/excludedSpecies.svelte';
import { getLogger } from '$lib/utils/logger';
import * as api from './api';
import type { DataGroup } from './columns';
import { createRequestSlot, isAbortError, type RequestSlot } from './requestSlot';
import type {
  BestRecording,
  MembershipKind,
  Memberships,
  WorkspaceSpecies,
  WorkspaceSpeciesStats,
} from './types';

const logger = getLogger('speciesWorkspace');

export const BEST_BATCH_SIZE = 25;

export type GroupStatus = 'idle' | 'loading' | 'ready' | 'error';

type TierGroup = Exclude<DataGroup, 'best'>;

export type WorkspaceApi = Pick<
  typeof api,
  | 'fetchSpecies'
  | 'fetchStats'
  | 'fetchMemberships'
  | 'fetchRangeScores'
  | 'fetchBestRecordings'
  | 'putMembership'
>;

export function createWorkspaceData(deps: WorkspaceApi = api) {
  let species = $state<WorkspaceSpecies[]>([]);
  const status = $state<Record<DataGroup, GroupStatus>>({
    inventory: 'idle',
    stats: 'idle',
    memberships: 'idle',
    range: 'idle',
    best: 'idle',
  });
  const stats = new SvelteMap<string, WorkspaceSpeciesStats>();
  const ranges = new SvelteMap<string, number>();
  const best = new SvelteMap<string, BestRecording | null>();
  const bestFailed = new SvelteSet<string>();
  let memberships = $state<Memberships>({ confirmed: [], included: [], excluded: [] });
  const togglePending = new SvelteSet<string>();

  const slots: Record<TierGroup, RequestSlot> = {
    inventory: createRequestSlot(),
    stats: createRequestSlot(),
    memberships: createRequestSlot(),
    range: createRequestSlot(),
  };
  // Best-recording batches run one at a time; invalidation bumps the generation
  // so results of a batch started before it are discarded.
  let bestQueue: string[] = [];
  const bestInFlight = new Set<string>();
  let bestRunning = false;
  let bestGeneration = 0;
  let bestController: AbortController | null = null;
  // Species whose in-flight best-recording result predates a refreshSpecies call.
  const staleBest = new Set<string>();
  // Bumped by every full load so an older single-species patch cannot overwrite it.
  const loadSeq: Record<TierGroup, number> = { inventory: 0, stats: 0, memberships: 0, range: 0 };
  const refreshControllers = new Set<AbortController>();

  async function load(group: TierGroup) {
    loadSeq[group]++;
    status[group] = 'loading';
    try {
      await slots[group].run(async signal => {
        switch (group) {
          case 'inventory':
            species = await deps.fetchSpecies(signal);
            break;
          case 'stats': {
            const rows = await deps.fetchStats(signal);
            stats.clear();
            for (const row of rows) stats.set(row.scientificName, row);
            break;
          }
          case 'memberships':
            memberships = await deps.fetchMemberships(signal);
            break;
          case 'range': {
            const scores = await deps.fetchRangeScores(signal);
            ranges.clear();
            for (const [name, score] of scores) ranges.set(name, score);
            break;
          }
        }
      });
      status[group] = 'ready';
    } catch (error) {
      // A superseded request leaves the status to the request that replaced it.
      if (isAbortError(error)) return;
      status[group] = 'error';
      logger.error('Species workspace request failed', error, { group });
    }
  }

  async function drainBest() {
    if (bestRunning) return;
    bestRunning = true;
    try {
      while (bestQueue.length > 0) {
        const generation = bestGeneration;
        const batch = bestQueue.splice(0, BEST_BATCH_SIZE);
        for (const name of batch) bestInFlight.add(name);
        const controller = new AbortController();
        bestController = controller;
        status.best = 'loading';
        try {
          const result = await deps.fetchBestRecordings(batch, controller.signal);
          if (generation !== bestGeneration) continue;
          for (const name of batch) {
            if (staleBest.delete(name)) {
              bestQueue.push(name);
              continue;
            }
            best.set(name, result[name] ?? null);
            bestFailed.delete(name);
          }
        } catch (error) {
          if (generation !== bestGeneration || isAbortError(error)) continue;
          for (const name of batch) {
            if (staleBest.delete(name)) bestQueue.push(name);
            else bestFailed.add(name);
          }
          logger.error('Best recording lookup failed', error, { count: batch.length });
        } finally {
          for (const name of batch) bestInFlight.delete(name);
        }
      }
      status.best = bestFailed.size > 0 ? 'error' : 'ready';
    } finally {
      bestController = null;
      bestRunning = false;
    }
  }

  function abortRefreshes() {
    for (const controller of refreshControllers) controller.abort();
    refreshControllers.clear();
  }

  /**
   * Patches one group for one species from a scoped request. A full load started
   * meanwhile wins; a failed patch falls back to a full reload.
   */
  async function patch<T>(
    group: 'inventory' | 'stats',
    fetchOne: (_signal: AbortSignal) => Promise<T>,
    apply: (_result: T) => void
  ) {
    const seq = loadSeq[group];
    const controller = new AbortController();
    refreshControllers.add(controller);
    try {
      const result = await fetchOne(controller.signal);
      if (seq === loadSeq[group]) apply(result);
    } catch (error) {
      if (isAbortError(error) || seq !== loadSeq[group]) return;
      logger.error('Species workspace refresh failed', error, { group });
      if (status[group] !== 'idle') void load(group);
    } finally {
      refreshControllers.delete(controller);
    }
  }

  return {
    get species() {
      return species;
    },
    get memberships() {
      return memberships;
    },
    status,
    stats,
    ranges,
    best,
    bestFailed,
    togglePending,

    /** Loads each needed tier group that is not loaded yet; aborts groups no longer needed. */
    ensure(groups: ReadonlySet<DataGroup>) {
      for (const group of Object.keys(slots) as TierGroup[]) {
        if (groups.has(group)) {
          if (status[group] === 'idle') void load(group);
        } else if (status[group] === 'loading') {
          slots[group].abort();
          status[group] = 'idle';
        }
      }
    },

    retry(group: DataGroup) {
      if (group === 'best') {
        this.requestBest([...bestFailed]);
        return;
      }
      void load(group);
    },

    /** Queues best-recording lookups for species not loaded or queued yet. */
    requestBest(names: readonly string[]) {
      const queued = new Set(bestQueue);
      for (const name of names) {
        if (!best.has(name) && !queued.has(name) && !bestInFlight.has(name)) {
          bestQueue.push(name);
          queued.add(name);
        }
      }
      void drainBest();
    },

    /** Membership of a species, or null while the lists are not loaded. */
    isMember(kind: MembershipKind, scientificName: string): boolean | null {
      if (status.memberships !== 'ready') return null;
      const target = scientificName.toLowerCase();
      return memberships[kind].some(name => name.toLowerCase() === target);
    },

    async setMembership(kind: MembershipKind, scientificName: string, present: boolean) {
      const key = `${kind}:${scientificName}`;
      if (togglePending.has(key)) return;
      togglePending.add(key);
      try {
        const result = await deps.putMembership(kind, scientificName, present);
        const others = memberships[kind].filter(
          name => name.toLowerCase() !== result.scientificName.toLowerCase()
        );
        memberships = {
          ...memberships,
          [kind]: result.present ? [...others, result.scientificName] : others,
        };
        // Detection rows elsewhere read the shared exclude store.
        if (kind === 'excluded') void hydrateExcludedSpecies(true);
      } finally {
        togglePending.delete(key);
      }
    },

    /**
     * Reloads one species after its detections changed: its inventory row (when the
     * inventory is loaded), its stats and its best recording. Other species keep
     * their data, and memberships and range scores, which detections do not
     * affect, are not reloaded.
     */
    async refreshSpecies(name: string) {
      invalidateSpeciesHistory();
      if (bestInFlight.has(name)) {
        staleBest.add(name);
      } else {
        best.delete(name);
        bestFailed.delete(name);
        this.requestBest([name]);
      }
      const jobs: Promise<void>[] = [];
      if (status.inventory === 'loading') void load('inventory');
      else if (status.inventory === 'ready') {
        jobs.push(
          patch(
            'inventory',
            signal => deps.fetchSpecies(signal, name),
            rows => {
              const row = rows.find(r => r.scientificName === name);
              const index = species.findIndex(s => s.scientificName === name);
              if (row && index >= 0) species[index] = row;
              else if (row) species = [...species, row];
              else if (index >= 0) species = species.filter((_, i) => i !== index);
            }
          )
        );
      }
      if (status.stats === 'loading') void load('stats');
      else {
        jobs.push(
          patch(
            'stats',
            signal => deps.fetchStats(signal, name),
            rows => {
              const row = rows.find(r => r.scientificName === name);
              if (row) stats.set(name, row);
              else stats.delete(name);
            }
          )
        );
      }
      await Promise.all(jobs);
    },

    /** Drops cached data after detections changed; callers then call ensure() again. */
    invalidate() {
      abortRefreshes();
      staleBest.clear();
      for (const slot of Object.values(slots)) slot.abort();
      for (const group of Object.keys(status) as DataGroup[]) status[group] = 'idle';
      bestGeneration++;
      bestController?.abort();
      bestQueue = [];
      bestInFlight.clear();
      best.clear();
      bestFailed.clear();
      stats.clear();
      invalidateSpeciesHistory();
    },

    dispose() {
      abortRefreshes();
      for (const slot of Object.values(slots)) slot.abort();
      bestGeneration++;
      bestController?.abort();
      bestQueue = [];
    },
  };
}

export type WorkspaceData = ReturnType<typeof createWorkspaceData>;
