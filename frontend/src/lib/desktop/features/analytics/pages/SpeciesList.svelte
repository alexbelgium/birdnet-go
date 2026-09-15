<script module lang="ts">
  // Range-filter scores require a full geo evaluation, so cache them for the
  // browser session. Module-scoped so the cache survives navigating away from
  // and back to this page within the same SPA session.
  let cachedRangeScores: Map<string, number> | null = null;
  let cachedRangeScoresKey: string | null = null;
</script>

<script lang="ts">
  import { t, getLocale } from '$lib/i18n';
  import { settingsActions, birdnetSettings } from '$lib/stores/settings';
  import Checkbox from '$lib/desktop/components/forms/Checkbox.svelte';
  import Modal from '$lib/desktop/components/ui/Modal.svelte';
  import SortableHeader from '$lib/desktop/components/ui/SortableHeader.svelte';
  import { api } from '$lib/utils/api';
  import { getLocalDateString, parseLocalDateString } from '$lib/utils/date';
  import { formatNumber } from '$lib/utils/formatters';
  import { loggers } from '$lib/utils/logger';
  import { localizeSpeciesName } from '$lib/utils/speciesDisplay';
  import { buildAppUrl } from '$lib/utils/urlHelpers';
  import { navigation } from '$lib/stores/navigation.svelte';
  import { Trash2 } from '@lucide/svelte';
  import { onMount } from 'svelte';
  import { SvelteMap, SvelteSet } from 'svelte/reactivity';
  import { get } from 'svelte/store';

  const logger = loggers.analytics;

  interface SpeciesData {
    common_name: string;
    scientific_name: string;
    count: number;
    avg_confidence: number;
    max_confidence: number;
    first_heard: string;
    last_heard: string;
    thumbnail_url?: string;
  }

  interface ReviewStat {
    scientificName: string;
    commonName?: string;
    total: number;
    verified: number;
    rejected: number;
  }

  type ManageSortKey =
    | 'name'
    | 'count'
    | 'max_confidence'
    | 'last_seen'
    | 'excluded'
    | 'included'
    | 'correct'
    | 'range'
    | 'confirmed';

  // Defense-in-depth cap on the species-delete round-trip loop. The backend's
  // exclude_ids handshake guarantees termination well before this limit.
  const MAX_DELETE_PASSES = 1000;

  const MANAGE_SORT_COLUMNS: { field: ManageSortKey; defaultAsc: boolean }[] = [
    { field: 'name', defaultAsc: true },
    { field: 'count', defaultAsc: false },
    { field: 'max_confidence', defaultAsc: false },
    { field: 'last_seen', defaultAsc: false },
    { field: 'excluded', defaultAsc: false },
    { field: 'included', defaultAsc: false },
    { field: 'correct', defaultAsc: false },
    { field: 'range', defaultAsc: false },
    { field: 'confirmed', defaultAsc: false },
  ];

  let isLoading = $state(true);
  let speciesData = $state<SpeciesData[]>([]);

  // Membership sets keyed by either the species' common or scientific name,
  // matching the existing settings endpoints' alias-aware contract.
  let excludedSet = new SvelteSet<string>();
  let includedSet = new SvelteSet<string>();
  let confirmedSet = new SvelteSet<string>();
  let togglingExcluded = new SvelteSet<string>();
  let togglingIncluded = new SvelteSet<string>();
  let togglingConfirmed = new SvelteSet<string>();

  let reviewStats = new SvelteMap<string, ReviewStat>();
  let rangeScores = new SvelteMap<string, number>();
  let rangeLoading = $state(false);
  let manageDataLoaded = $state(false);
  let manageSortKey = $state<ManageSortKey>('count');
  let manageSortDirection = $state<'asc' | 'desc'>('desc');

  let deleteTarget = $state<SpeciesData | null>(null);
  let showDeleteModal = $state(false);
  let deleteInFlight = $state(false);
  let deleteError = $state<string | null>(null);
  let deleteProgress = $state<{ deleted: number; remaining: number } | null>(null);
  let membershipError = $state<string | null>(null);

  function showManageView() {
    void Promise.all([fetchData(), fetchManageData()]);
  }

  onMount(showManageView);

  async function fetchData() {
    isLoading = true;
    try {
      const response = await fetch(buildAppUrl('/api/v2/analytics/species/summary?'));
      if (!response.ok) throw new Error(`Server responded with ${response.status}`);

      const rawSpecies: SpeciesData[] = await response.json();
      speciesData = rawSpecies.map(species =>
        species.thumbnail_url
          ? { ...species, thumbnail_url: buildAppUrl(species.thumbnail_url) }
          : species
      );
    } catch (error) {
      logger.error('Error fetching species data:', error);
      speciesData = [];
    } finally {
      isLoading = false;
    }
  }

  async function fetchManageData() {
    if (manageDataLoaded) {
      void loadRangeScores();
      return;
    }
    try {
      const [stats, included, confirmed, excluded] = await Promise.all([
        api.get<ReviewStat[]>('/api/v2/analytics/species/review-stats'),
        api.get<{ species: string[] }>('/api/v2/detections/included'),
        api.get<{ species: string[] }>('/api/v2/detections/confirmed'),
        api.get<{ species: string[] }>('/api/v2/detections/ignored'),
      ]);

      reviewStats.clear();
      for (const stat of stats) reviewStats.set(stat.scientificName, stat);
      includedSet.clear();
      for (const name of included.species ?? []) includedSet.add(name);
      confirmedSet.clear();
      for (const name of confirmed.species ?? []) confirmedSet.add(name);
      excludedSet.clear();
      for (const name of excluded.species ?? []) excludedSet.add(name);
      manageDataLoaded = true;
    } catch (error) {
      logger.error('Error fetching manage data:', error);
      membershipError = t('analytics.species.manage.loadFailed');
    }
    void loadRangeScores();
  }

  function rangeScoreCacheKey(): string {
    const settings = get(birdnetSettings);
    return `${settings.latitude}|${settings.longitude}|${settings.rangeFilter?.threshold}`;
  }

  async function loadRangeScores() {
    const key = rangeScoreCacheKey();
    if (cachedRangeScores && cachedRangeScoresKey === key) {
      if (rangeScores.size === 0) {
        for (const [name, score] of cachedRangeScores) rangeScores.set(name, score);
      }
      return;
    }

    rangeLoading = true;
    try {
      const result = await settingsActions.loadRangeFilterSpecies();
      const scores = new Map<string, number>();
      for (const species of result.species) {
        if (species.scientificName && typeof species.score === 'number') {
          scores.set(species.scientificName, species.score);
        }
      }
      cachedRangeScores = scores;
      cachedRangeScoresKey = key;
      rangeScores.clear();
      for (const [name, score] of scores) rangeScores.set(name, score);
    } catch (error) {
      logger.error('Error loading range scores:', error);
    } finally {
      rangeLoading = false;
    }
  }

  function handleManageSort(field: string) {
    const column = MANAGE_SORT_COLUMNS.find(candidate => candidate.field === field);
    if (!column) return;
    if (manageSortKey === column.field) {
      manageSortDirection = manageSortDirection === 'asc' ? 'desc' : 'asc';
    } else {
      manageSortKey = column.field;
      manageSortDirection = column.defaultAsc ? 'asc' : 'desc';
    }
  }

  function correctRate(species: SpeciesData): number {
    const stat = reviewStats.get(species.scientific_name);
    if (!stat) return -1;
    const reviewed = stat.verified + stat.rejected;
    if (reviewed === 0) return -1;
    return (stat.verified / reviewed) * 100;
  }

  function listEntryFor(set: SvelteSet<string>, species: SpeciesData): string | undefined {
    const commonName = species.common_name.toLowerCase();
    const scientificName = species.scientific_name.toLowerCase();
    for (const entry of set) {
      const normalizedEntry = entry.toLowerCase();
      if (normalizedEntry === commonName || normalizedEntry === scientificName) return entry;
    }
    return undefined;
  }

  function isMember(set: SvelteSet<string>, species: SpeciesData): boolean {
    return listEntryFor(set, species) !== undefined;
  }

  let manageSpecies = $derived.by<SpeciesData[]>(() => {
    const present = new Set(speciesData.map(species => species.scientific_name));
    const extras: SpeciesData[] = [];
    for (const [scientificName, stat] of reviewStats) {
      if (present.has(scientificName)) continue;
      extras.push({
        common_name: stat.commonName || scientificName,
        scientific_name: scientificName,
        count: stat.total,
        avg_confidence: 0,
        max_confidence: 0,
        first_heard: '',
        last_heard: '',
      });
    }
    return extras.length > 0 ? [...speciesData, ...extras] : speciesData;
  });

  let manageRows = $derived.by<SpeciesData[]>(() => {
    const locale = getLocale();
    const direction = manageSortDirection === 'asc' ? 1 : -1;
    return [...manageSpecies].sort((a, b) => {
      switch (manageSortKey) {
        case 'name':
          return (
            direction *
            localizeSpeciesName(a.scientific_name, a.common_name).localeCompare(
              localizeSpeciesName(b.scientific_name, b.common_name),
              locale
            )
          );
        case 'count':
          return direction * (a.count - b.count);
        case 'max_confidence':
          return direction * (a.max_confidence - b.max_confidence);
        case 'last_seen': {
          const dateA = parseLocalDateString(a.last_heard);
          const dateB = parseLocalDateString(b.last_heard);
          if (!dateA && !dateB) return 0;
          if (!dateA) return 1;
          if (!dateB) return -1;
          return direction * (dateA.getTime() - dateB.getTime());
        }
        case 'excluded':
          return direction * (Number(isMember(excludedSet, a)) - Number(isMember(excludedSet, b)));
        case 'included':
          return direction * (Number(isMember(includedSet, a)) - Number(isMember(includedSet, b)));
        case 'confirmed':
          return (
            direction * (Number(isMember(confirmedSet, a)) - Number(isMember(confirmedSet, b)))
          );
        case 'correct':
          return direction * (correctRate(a) - correctRate(b));
        case 'range':
          return (
            direction *
            ((rangeScores.get(a.scientific_name) ?? -1) -
              (rangeScores.get(b.scientific_name) ?? -1))
          );
        default: {
          const exhaustive: never = manageSortKey;
          void exhaustive;
          return 0;
        }
      }
    });
  });

  function formatPercentage(value: number): string {
    return `${(value * 100).toFixed(1)}%`;
  }

  function formatDateOnly(value: string): string {
    if (!value) return '—';
    const parsed = parseLocalDateString(value);
    return parsed ? getLocalDateString(parsed) : '—';
  }

  function correctRateText(species: SpeciesData): string {
    const rate = correctRate(species);
    return rate < 0 ? '—' : `${Math.round(rate)}%`;
  }

  async function toggleMembership(
    list: 'excluded' | 'included' | 'confirmed',
    species: SpeciesData
  ) {
    const inFlight =
      list === 'excluded'
        ? togglingExcluded
        : list === 'included'
          ? togglingIncluded
          : togglingConfirmed;
    const inFlightKey = species.scientific_name;
    if (inFlight.has(inFlightKey)) return;

    const membership =
      list === 'excluded' ? excludedSet : list === 'included' ? includedSet : confirmedSet;
    const path =
      list === 'excluded'
        ? '/api/v2/detections/ignore'
        : list === 'included'
          ? '/api/v2/detections/include'
          : '/api/v2/detections/confirm';
    const existingEntry = listEntryFor(membership, species);
    const payloadName = existingEntry ?? species.common_name;
    const wasPresent = existingEntry !== undefined;

    if (wasPresent) membership.delete(payloadName);
    else membership.add(payloadName);

    inFlight.add(inFlightKey);
    membershipError = null;
    try {
      await api.post<{ action: string }>(path, { common_name: payloadName });
    } catch (error) {
      logger.error(`Error toggling ${list} membership:`, error);
      membershipError = t('analytics.species.manage.membershipFailed');
      if (wasPresent) membership.add(payloadName);
      else membership.delete(payloadName);
    } finally {
      inFlight.delete(inFlightKey);
    }
  }

  function requestDelete(species: SpeciesData) {
    deleteTarget = species;
    deleteError = null;
    showDeleteModal = true;
  }

  let deleteAllTimeCount = $derived(
    deleteTarget ? (reviewStats.get(deleteTarget.scientific_name)?.total ?? deleteTarget.count) : 0
  );

  async function confirmDelete() {
    if (!deleteTarget) return;
    const target = deleteTarget;
    deleteInFlight = true;
    deleteError = null;
    deleteProgress = null;
    let totalDeleted = 0;
    let totalSkipped = 0;
    const excludeIds: string[] = [];

    try {
      let deletePasses = 0;
      for (;;) {
        deletePasses += 1;
        if (deletePasses > MAX_DELETE_PASSES) {
          throw new Error(`Species delete exceeded ${MAX_DELETE_PASSES} passes`);
        }
        const result = await api.post<{
          deleted: number;
          skipped: number;
          remaining: number;
          skipped_ids?: string[];
        }>('/api/v2/detections/species/delete', {
          scientific_name: target.scientific_name,
          exclude_ids: excludeIds,
        });
        totalDeleted += result?.deleted ?? 0;
        totalSkipped += result?.skipped ?? 0;
        excludeIds.push(...(result?.skipped_ids ?? []));
        const remaining = result?.remaining ?? 0;
        if (remaining === 0) break;
        deleteProgress = { deleted: totalDeleted, remaining };
      }

      showDeleteModal = false;
      deleteTarget = null;
      if (totalSkipped > 0) {
        manageDataLoaded = false;
        await Promise.all([fetchData(), fetchManageData()]);
        return;
      }

      speciesData = speciesData.filter(
        species => species.scientific_name !== target.scientific_name
      );
      reviewStats.delete(target.scientific_name);
      cachedRangeScores?.delete(target.scientific_name);
      rangeScores.delete(target.scientific_name);
    } catch (error) {
      logger.error('Error deleting species detections:', error);
      deleteError = t('analytics.species.manage.deleteFailed');
      if (totalDeleted > 0 || totalSkipped > 0) {
        manageDataLoaded = false;
        void Promise.all([fetchData(), fetchManageData()]);
      }
    } finally {
      deleteInFlight = false;
      deleteProgress = null;
    }
  }

  function cancelDelete() {
    showDeleteModal = false;
    deleteTarget = null;
    deleteError = null;
  }

  function navigateToSpeciesDetections(species: SpeciesData) {
    navigation.navigate(
      `/ui/detections?queryType=species&species=${encodeURIComponent(species.scientific_name)}&sortBy=confidence_desc`
    );
  }
</script>

<div class="col-span-12 space-y-4" role="region" aria-label={t('analytics.speciesList.title')}>
  <div class="card bg-[var(--color-base-100)] shadow-xs">
    <div class="card-body card-padding">
      <h1 class="card-title text-2xl">{t('analytics.speciesList.title')}</h1>
      <p class="text-sm text-base-content/70" role="note">
        {t('analytics.species.manage.allTimeNote')}
      </p>
    </div>
  </div>

  <div class="card bg-[var(--color-base-100)] shadow-xs">
    <div class="card-body card-padding">
      {#if isLoading}
        <div class="flex justify-center items-center p-8">
          <span class="loading loading-spinner loading-lg text-[var(--color-primary)]"></span>
        </div>
      {:else}
        {#if membershipError}
          <p class="text-sm text-[var(--color-error)] mb-2" role="alert">{membershipError}</p>
        {/if}

        {#if manageRows.length > 0}
          <div class="overflow-x-auto hidden sm:block">
            <table class="table w-full">
              <thead>
                <tr>
                  <SortableHeader
                    label={t('analytics.species.headers.species')}
                    field="name"
                    activeField={manageSortKey}
                    direction={manageSortDirection}
                    onSort={handleManageSort}
                  />
                  <SortableHeader
                    label={t('analytics.species.headers.detections')}
                    field="count"
                    activeField={manageSortKey}
                    direction={manageSortDirection}
                    onSort={handleManageSort}
                  />
                  <SortableHeader
                    label={t('analytics.species.headers.maxConfidence')}
                    field="max_confidence"
                    activeField={manageSortKey}
                    direction={manageSortDirection}
                    onSort={handleManageSort}
                  />
                  <SortableHeader
                    label={t('analytics.species.headers.lastDetected')}
                    field="last_seen"
                    activeField={manageSortKey}
                    direction={manageSortDirection}
                    onSort={handleManageSort}
                  />
                  <SortableHeader
                    label={t('analytics.species.manage.headers.excluded')}
                    field="excluded"
                    activeField={manageSortKey}
                    direction={manageSortDirection}
                    onSort={handleManageSort}
                  />
                  <SortableHeader
                    label={t('analytics.species.manage.headers.included')}
                    field="included"
                    activeField={manageSortKey}
                    direction={manageSortDirection}
                    onSort={handleManageSort}
                  />
                  <SortableHeader
                    label={t('analytics.species.manage.headers.reviewRatio')}
                    field="correct"
                    activeField={manageSortKey}
                    direction={manageSortDirection}
                    onSort={handleManageSort}
                  />
                  <SortableHeader
                    label={t('analytics.species.manage.headers.rangeProbability')}
                    field="range"
                    activeField={manageSortKey}
                    direction={manageSortDirection}
                    onSort={handleManageSort}
                  />
                  <SortableHeader
                    label={t('analytics.species.manage.headers.confirmed')}
                    field="confirmed"
                    activeField={manageSortKey}
                    direction={manageSortDirection}
                    onSort={handleManageSort}
                  />
                  <th>{t('analytics.species.manage.headers.actions')}</th>
                </tr>
              </thead>
              <tbody>
                {#each manageRows as species, index (`${species.scientific_name}_${index}`)}
                  {@const displayName = localizeSpeciesName(
                    species.scientific_name,
                    species.common_name
                  )}
                  <tr
                    class={index % 2 === 0
                      ? 'bg-[var(--color-base-100)]'
                      : 'bg-[var(--color-base-200)]'}
                  >
                    <td class="p-0">
                      <button
                        type="button"
                        class="w-full p-3 text-left cursor-pointer hover:bg-[var(--color-base-300)] transition-colors"
                        onclick={() => navigateToSpeciesDetections(species)}
                        aria-label={`View all recordings of ${displayName}`}
                      >
                        <div class="font-bold">{displayName}</div>
                        <div class="text-sm opacity-50 italic">{species.scientific_name}</div>
                      </button>
                    </td>
                    <td class="font-semibold">{species.count}</td>
                    <td
                      >{species.max_confidence > 0
                        ? formatPercentage(species.max_confidence)
                        : '—'}</td
                    >
                    <td class="text-sm">{formatDateOnly(species.last_heard)}</td>
                    <td
                      title={togglingExcluded.has(species.scientific_name)
                        ? t('common.ui.loading')
                        : undefined}
                    >
                      <Checkbox
                        checked={isMember(excludedSet, species)}
                        disabled={togglingExcluded.has(species.scientific_name)}
                        onchange={() => toggleMembership('excluded', species)}
                      />
                    </td>
                    <td
                      title={togglingIncluded.has(species.scientific_name)
                        ? t('common.ui.loading')
                        : undefined}
                    >
                      <Checkbox
                        checked={isMember(includedSet, species)}
                        disabled={togglingIncluded.has(species.scientific_name)}
                        onchange={() => toggleMembership('included', species)}
                      />
                    </td>
                    <td class="text-sm">{correctRateText(species)}</td>
                    <td class="text-sm">
                      {#if rangeLoading && !rangeScores.has(species.scientific_name)}
                        <span
                          class="loading loading-spinner loading-xs"
                          aria-label={t('common.ui.loading')}
                        ></span>
                      {:else if rangeScores.has(species.scientific_name)}
                        {formatPercentage(rangeScores.get(species.scientific_name) ?? 0)}
                      {:else}
                        —
                      {/if}
                    </td>
                    <td
                      title={togglingConfirmed.has(species.scientific_name)
                        ? t('common.ui.loading')
                        : undefined}
                    >
                      <Checkbox
                        checked={isMember(confirmedSet, species)}
                        disabled={togglingConfirmed.has(species.scientific_name)}
                        onchange={() => toggleMembership('confirmed', species)}
                      />
                    </td>
                    <td>
                      <button
                        type="button"
                        class="btn btn-ghost btn-xs text-[var(--color-error)]"
                        onclick={() => requestDelete(species)}
                        aria-label={t('analytics.species.manage.delete')}
                      >
                        <Trash2 class="h-4 w-4" />
                      </button>
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {:else}
          <div class="text-center py-8 text-[var(--color-base-content)] opacity-50">
            <p>{t('analytics.species.noSpeciesFound')}</p>
          </div>
        {/if}
      {/if}
    </div>
  </div>
</div>

<Modal
  isOpen={showDeleteModal}
  type="confirm"
  title={t('analytics.species.manage.deleteTitle')}
  confirmLabel={t('analytics.species.manage.deleteConfirm')}
  confirmVariant="error"
  loading={deleteInFlight}
  onClose={cancelDelete}
  onConfirm={confirmDelete}
>
  {#if deleteTarget}
    <p>
      {t('analytics.species.manage.deleteMessage', {
        species: localizeSpeciesName(deleteTarget.scientific_name, deleteTarget.common_name),
        count: formatNumber(deleteAllTimeCount),
      })}
    </p>
    <p class="text-sm opacity-70 mt-2">{t('analytics.species.manage.deleteWarning')}</p>
    {#if deleteProgress}
      <p class="text-sm opacity-70 mt-2" role="status" aria-live="polite">
        {t('analytics.species.manage.deleteProgress', {
          deleted: formatNumber(deleteProgress.deleted),
          remaining: formatNumber(deleteProgress.remaining),
        })}
      </p>
    {/if}
    {#if deleteError}
      <p class="text-sm text-[var(--color-error)] mt-2" role="alert">{deleteError}</p>
    {/if}
  {/if}
</Modal>

<style>
  .card-padding {
    padding: 1rem;
  }

  @media (min-width: 768px) {
    .card-padding {
      padding: 1.5rem;
    }
  }
</style>
