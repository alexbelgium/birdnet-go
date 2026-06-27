<!--
AllSpecies.svelte - Self-contained "All Species" page.

Single, isolated file so it can ride along an always-open PR with a minimal
footprint on the rest of the tree. It has two views:

  1. Table view  - every detected species. For authorized users it doubles as a
     curation/manage table: correct-rate %, range probability, and per-species
     include / confirm / exclude toggles plus delete. Click a species image or
     name to drill into its recordings.
  2. Recordings  - all recordings of the chosen species across ALL dates.
     Sort control: Max confidence (default) / Most recent / Locked only.

Backend touch-points are deliberately tiny: a Confirmed list on SpeciesSettings
plus a small set of curation endpoints (internal/api/v2/species_curation.go).
Everything else reuses existing endpoints:
  - summary           GET  /api/v2/analytics/species/summary
  - recordings        GET  /api/v2/detections (queryType=search + species ->
                           advanced routing, all dates, honours sortBy/locked)
  - correct-rate      GET  /api/v2/analytics/species/review-stats   (new)
  - range probability POST /api/v2/range/species/test (via settingsActions.
                           loadRangeFilterSpecies: honours the configured
                           location + threshold, matching the range filter)
  - exclude toggle    POST /api/v2/detections/ignore
  - include toggle    POST /api/v2/detections/include              (new)
  - confirm toggle    POST /api/v2/detections/confirm              (new)
  - delete species    POST /api/v2/detections/batch/delete (skips locked)

All user-facing text is intentionally hardcoded English (no i18n).
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { fetchWithCSRF } from '$lib/utils/api';
  import { buildAppUrl } from '$lib/utils/urlHelpers';
  import { navigation } from '$lib/stores/navigation.svelte';
  import { getLogger } from '$lib/utils/logger';
  import { toastActions } from '$lib/stores/toast';
  import { settingsActions } from '$lib/stores/settings';
  import { appState } from '$lib/stores/appState.svelte';
  import SpectrogramPlayer from '$lib/desktop/components/media/SpectrogramPlayer.svelte';
  import { ChevronLeft, Lock, Search, Plus, Check, Ban, Trash2 } from '@lucide/svelte';

  const logger = getLogger('app');

  // Shape returned by GET /api/v2/analytics/species/summary
  interface SpeciesSummary {
    scientific_name: string;
    common_name: string;
    species_code?: string;
    count: number;
    first_heard?: string;
    last_heard?: string;
    avg_confidence?: number;
    max_confidence?: number;
    thumbnail_url?: string;
  }

  // Subset of a detection from GET /api/v2/detections we actually render
  interface Detection {
    id: number;
    date: string;
    time: string;
    scientificName: string;
    commonName: string;
    confidence: number;
    verified: string;
    locked: boolean;
  }

  interface DetectionsResponse {
    data: Detection[];
    total: number;
  }

  // Per-species review tally from GET /api/v2/analytics/species/review-stats
  interface ReviewStat {
    scientific_name: string;
    correct: number;
    false_positive: number;
    total: number;
    correct_rate: number;
  }

  // Wrapper for GET /api/v2/analytics/species/review-stats. `truncated` is true
  // when the backend sweep stopped at its page cap, so the correct-% figures are
  // based on a partial sample and we surface that to the user.
  interface ReviewStatsResponse {
    stats: Record<string, ReviewStat>;
    truncated: boolean;
  }

  type SortMode = 'confidence' | 'date' | 'locked';
  type TableColumn =
    | 'count'
    | 'name'
    | 'max'
    | 'first'
    | 'last'
    | 'correct'
    | 'range'
    | 'include'
    | 'confirm'
    | 'exclude';
  type Membership = 'include' | 'confirm' | 'exclude';

  const RESULTS_OPTIONS = [10, 25, 50, 100] as const;
  const DELETE_PAGE_SIZE = 200; // pull detection IDs in pages of this size
  const DELETE_CHUNK_SIZE = 500; // batch-delete server cap (maxBatchSize)
  const DELETE_MAX_PAGES = 200; // safety cap on ID collection
  // Most detections a single delete pass can collect. Beyond this the pass
  // deletes what it gathered and tells the user to repeat, rather than silently
  // dropping the rest.
  const DELETE_MAX_COLLECT = DELETE_PAGE_SIZE * DELETE_MAX_PAGES;

  // Only authorized users get the curation columns + actions (desktop manage
  // view). The endpoints behind them are auth-protected anyway.
  let canEdit = $derived(!appState.security.enabled || appState.security.accessAllowed);

  // ---- which view is active ----
  let view = $state<'table' | 'recordings'>('table');

  // ---- species table state ----
  let species = $state<SpeciesSummary[]>([]);
  let loadingTable = $state(true);
  let tableError = $state<string | null>(null);
  let searchTerm = $state('');
  let tableColumn = $state<TableColumn>('count');
  let tableAscending = $state(false);

  // ---- curation state (keyed by name; Maps/Sets avoid object-injection lint) ----
  let reviewStats = $state<Map<string, ReviewStat>>(new Map());
  let reviewStatsTruncated = $state(false); // backend hit its sweep cap; figures are partial
  let rangeScores = $state<Map<string, number>>(new Map());
  let rangeLoading = $state(false); // range scores stream in after the table renders
  // Range-filter scores need a full geomodel evaluation server-side, so cache
  // them for the lifetime of the page (location/threshold don't change mid-visit).
  let cachedRangeScores: Map<string, number> | null = null;
  let includedSet = $state<Set<string>>(new Set());
  let confirmedSet = $state<Set<string>>(new Set());
  let excludedSet = $state<Set<string>>(new Set());
  let busySpecies = $state<Set<string>>(new Set()); // common names with an in-flight toggle

  // ---- delete confirmation ----
  let deleteTarget = $state<SpeciesSummary | null>(null);
  let deleting = $state(false);

  // ---- recordings state ----
  let selectedScientific = $state('');
  let selectedCommon = $state('');
  let recordings = $state<Detection[]>([]);
  let loadingRecordings = $state(false);
  let recordingsError = $state<string | null>(null);
  let sortMode = $state<SortMode>('confidence');
  let numResults = $state(25);
  let offset = $state(0);
  let total = $state(0);

  // -------------------------------------------------------------------------
  // Data loading
  // -------------------------------------------------------------------------

  async function fetchSpecies(): Promise<void> {
    loadingTable = true;
    tableError = null;
    try {
      // No date params => overall, all-time summary of every species.
      const data = await fetchWithCSRF<SpeciesSummary[]>('/api/v2/analytics/species/summary');
      species = Array.isArray(data) ? data : [];
    } catch (err) {
      tableError = err instanceof Error ? err.message : 'Failed to load species';
      logger.error('AllSpecies: failed to load species summary', err);
    } finally {
      loadingTable = false;
    }
  }

  async function loadCuration(): Promise<void> {
    if (!canEdit) return;

    const results = await Promise.allSettled([
      fetchWithCSRF<ReviewStatsResponse>('/api/v2/analytics/species/review-stats'),
      fetchWithCSRF<{ species: string[] }>('/api/v2/detections/included'),
      fetchWithCSRF<{ species: string[] }>('/api/v2/detections/confirmed'),
      fetchWithCSRF<{ species: string[] }>('/api/v2/detections/ignored'),
    ]);

    const [stats, included, confirmed, excluded] = results;

    if (stats.status === 'fulfilled' && stats.value) {
      reviewStats = new Map(Object.entries(stats.value.stats ?? {}));
      reviewStatsTruncated = Boolean(stats.value.truncated);
    }
    if (included.status === 'fulfilled') includedSet = new Set(included.value?.species ?? []);
    if (confirmed.status === 'fulfilled') confirmedSet = new Set(confirmed.value?.species ?? []);
    if (excluded.status === 'fulfilled') excludedSet = new Set(excluded.value?.species ?? []);

    if (results.some(r => r.status === 'rejected')) {
      logger.error('AllSpecies: some curation data failed to load');
    }
  }

  // Range-filter probability per species, loaded separately from the rest of the
  // curation data so the table renders immediately while the (expensive) geomodel
  // evaluation streams in. Uses settingsActions.loadRangeFilterSpecies → POST
  // /api/v2/range/species/test, the same proven path the settings range-filter
  // test uses, so the figures honour the configured location + threshold. Species
  // below the threshold are absent from the result and render as an em dash.
  async function loadRangeScores(): Promise<void> {
    if (!canEdit) return;
    if (cachedRangeScores) {
      rangeScores = new Map(cachedRangeScores);
      return;
    }
    rangeLoading = true;
    try {
      const result = await settingsActions.loadRangeFilterSpecies();
      const map = new Map<string, number>();
      for (const sp of result.species) {
        if (sp.scientificName && typeof sp.score === 'number') {
          map.set(sp.scientificName, sp.score);
        }
      }
      cachedRangeScores = map;
      rangeScores = map;
    } catch (err) {
      logger.error('AllSpecies: failed to load range-filter scores', err);
    } finally {
      rangeLoading = false;
    }
  }

  // -------------------------------------------------------------------------
  // Species table
  // -------------------------------------------------------------------------

  let visibleSpecies = $derived.by<SpeciesSummary[]>(() => {
    const term = searchTerm.trim().toLowerCase();
    const rows = term
      ? species.filter(
          s =>
            s.common_name.toLowerCase().includes(term) ||
            s.scientific_name.toLowerCase().includes(term)
        )
      : species.slice();

    const dir = tableAscending ? 1 : -1;
    rows.sort((a, b) => {
      switch (tableColumn) {
        case 'name':
          return dir * a.common_name.localeCompare(b.common_name);
        case 'max':
          return dir * ((a.max_confidence ?? 0) - (b.max_confidence ?? 0));
        case 'first':
          return dir * (a.first_heard ?? '').localeCompare(b.first_heard ?? '');
        case 'last':
          return dir * (a.last_heard ?? '').localeCompare(b.last_heard ?? '');
        case 'correct':
          return (
            dir *
            ((reviewStats.get(a.scientific_name)?.correct_rate ?? -1) -
              (reviewStats.get(b.scientific_name)?.correct_rate ?? -1))
          );
        case 'range':
          return (
            dir *
            ((rangeScores.get(a.scientific_name) ?? -1) -
              (rangeScores.get(b.scientific_name) ?? -1))
          );
        case 'include': {
          const ai = includedSet.has(a.common_name) ? 1 : 0;
          const bi = includedSet.has(b.common_name) ? 1 : 0;
          return dir * (ai - bi) || a.common_name.localeCompare(b.common_name);
        }
        case 'confirm': {
          const ac = confirmedSet.has(a.common_name) ? 1 : 0;
          const bc = confirmedSet.has(b.common_name) ? 1 : 0;
          return dir * (ac - bc) || a.common_name.localeCompare(b.common_name);
        }
        case 'exclude': {
          const ae = excludedSet.has(a.common_name) ? 1 : 0;
          const be = excludedSet.has(b.common_name) ? 1 : 0;
          return dir * (ae - be) || a.common_name.localeCompare(b.common_name);
        }
        case 'count':
        default:
          return dir * (a.count - b.count);
      }
    });
    return rows;
  });

  function setTableColumn(column: TableColumn): void {
    if (tableColumn === column) {
      tableAscending = !tableAscending;
    } else {
      tableColumn = column;
      // Names read best A->Z, everything else high->low first.
      tableAscending = column === 'name';
    }
  }

  function sortIndicator(column: TableColumn): string {
    if (tableColumn !== column) return '';
    return tableAscending ? ' ▲' : ' ▼';
  }

  // aria-sort for the active sortable column header (accessibility: lets screen
  // readers announce the current sort state).
  function ariaSort(column: TableColumn): 'ascending' | 'descending' | 'none' {
    if (tableColumn !== column) return 'none';
    return tableAscending ? 'ascending' : 'descending';
  }

  // -------------------------------------------------------------------------
  // Curation actions
  // -------------------------------------------------------------------------

  function membershipUrl(kind: Membership): string {
    if (kind === 'exclude') return '/api/v2/detections/ignore';
    return `/api/v2/detections/${kind}`;
  }

  function withBusy(name: string, busy: boolean): void {
    const next = new Set(busySpecies);
    if (busy) next.add(name);
    else next.delete(name);
    busySpecies = next;
  }

  function applyMembership(kind: Membership, name: string, active: boolean): void {
    const source =
      kind === 'include' ? includedSet : kind === 'confirm' ? confirmedSet : excludedSet;
    const next = new Set(source);
    if (active) next.add(name);
    else next.delete(name);
    if (kind === 'include') includedSet = next;
    else if (kind === 'confirm') confirmedSet = next;
    else excludedSet = next;
  }

  async function toggleMembership(kind: Membership, s: SpeciesSummary): Promise<void> {
    const name = s.common_name;
    if (busySpecies.has(name)) return;
    withBusy(name, true);
    try {
      const res = await fetchWithCSRF<{ is_member?: boolean; is_excluded?: boolean }>(
        membershipUrl(kind),
        { method: 'POST', body: { common_name: name } }
      );
      const active = kind === 'exclude' ? Boolean(res?.is_excluded) : Boolean(res?.is_member);
      applyMembership(kind, name, active);
      const verb = active ? 'added to' : 'removed from';
      const listName = kind === 'exclude' ? 'exclude' : kind;
      toastActions.show(`${name} ${verb} ${listName} list`, 'success');
    } catch (err) {
      toastActions.show(`Failed to update ${name}`, 'error');
      logger.error('AllSpecies: membership toggle failed', err);
    } finally {
      withBusy(name, false);
    }
  }

  function askDelete(s: SpeciesSummary): void {
    deleteTarget = s;
  }

  function cancelDelete(): void {
    if (deleting) return;
    deleteTarget = null;
  }

  // Collect every detection ID for a species across all dates (paged), then
  // batch-delete in server-sized chunks. The server skips locked detections.
  async function confirmDelete(): Promise<void> {
    if (!deleteTarget) return;
    const target = deleteTarget;
    deleting = true;
    try {
      const ids: string[] = [];
      let capped = false;
      for (let page = 0; page < DELETE_MAX_PAGES; page++) {
        const params = new URLSearchParams();
        params.set('queryType', 'search');
        params.set('species', target.scientific_name);
        params.set('numResults', String(DELETE_PAGE_SIZE));
        params.set('offset', String(page * DELETE_PAGE_SIZE));
        params.set('sortBy', 'date_desc');
        const data = await fetchWithCSRF<DetectionsResponse>(
          `/api/v2/detections?${params.toString()}`
        );
        const rows = data?.data ?? [];
        for (const d of rows) ids.push(String(d.id));
        if (rows.length < DELETE_PAGE_SIZE) break;
        // A full final page means the cap was reached before exhausting the
        // species, so recordings remain that this pass will not collect.
        if (page === DELETE_MAX_PAGES - 1) capped = true;
      }

      let processed = 0;
      let skipped = 0;
      for (let i = 0; i < ids.length; i += DELETE_CHUNK_SIZE) {
        const chunk = ids.slice(i, i + DELETE_CHUNK_SIZE);
        const res = await fetchWithCSRF<{ processed?: number; skipped?: number }>(
          '/api/v2/detections/batch/delete',
          { method: 'POST', body: { ids: chunk } }
        );
        processed += res?.processed ?? 0;
        skipped += res?.skipped ?? 0;
      }

      const summary =
        `Deleted ${processed} recording${processed === 1 ? '' : 's'} of ${target.common_name}` +
        (skipped > 0 ? ` (${skipped} locked, skipped)` : '');
      if (capped) {
        // Don't claim "all" were removed when the collection pass was capped.
        toastActions.show(
          `${summary}. More than ${DELETE_MAX_COLLECT.toLocaleString()} recordings exist — run delete again to remove the rest.`,
          'warning'
        );
      } else {
        toastActions.show(summary, 'success');
      }
      deleteTarget = null;
      await fetchSpecies();
      await loadCuration();
    } catch (err) {
      toastActions.show(`Failed to delete recordings of ${target.common_name}`, 'error');
      logger.error('AllSpecies: delete failed', err);
    } finally {
      deleting = false;
    }
  }

  // -------------------------------------------------------------------------
  // Recordings for a single species
  // -------------------------------------------------------------------------

  function openRecordings(s: SpeciesSummary): void {
    selectedScientific = s.scientific_name;
    selectedCommon = s.common_name;
    sortMode = 'confidence';
    offset = 0;
    total = 0;
    recordings = [];
    view = 'recordings';
    void fetchRecordings();
  }

  function backToTable(): void {
    view = 'table';
    recordings = [];
    recordingsError = null;
  }

  async function fetchRecordings(): Promise<void> {
    loadingRecordings = true;
    recordingsError = null;
    try {
      const params = new URLSearchParams();
      // queryType=search + a species filter routes through the advanced search
      // path, which returns matches across ALL dates (the dedicated species
      // handler would otherwise restrict to a single day) and honours sortBy /
      // locked. This needs no backend change.
      params.set('queryType', 'search');
      params.set('species', selectedScientific);
      params.set('numResults', String(numResults));
      params.set('offset', String(offset));
      params.set('includeWeather', 'false');

      if (sortMode === 'date') {
        params.set('sortBy', 'date_desc');
      } else if (sortMode === 'locked') {
        params.set('locked', 'true');
        params.set('sortBy', 'confidence_desc');
      } else {
        params.set('sortBy', 'confidence_desc');
      }

      const data = await fetchWithCSRF<DetectionsResponse>(
        `/api/v2/detections?${params.toString()}`
      );
      recordings = data?.data ?? [];
      total = data?.total ?? 0;
    } catch (err) {
      recordingsError = err instanceof Error ? err.message : 'Failed to load recordings';
      logger.error('AllSpecies: failed to load recordings', err);
    } finally {
      loadingRecordings = false;
    }
  }

  function setSortMode(mode: SortMode): void {
    if (sortMode === mode) return;
    sortMode = mode;
    offset = 0;
    void fetchRecordings();
  }

  function changeNumResults(value: number): void {
    if (!RESULTS_OPTIONS.includes(value as (typeof RESULTS_OPTIONS)[number])) return;
    numResults = value;
    offset = 0;
    void fetchRecordings();
  }

  function nextPage(): void {
    if (offset + numResults < total) {
      offset += numResults;
      void fetchRecordings();
    }
  }

  function prevPage(): void {
    if (offset > 0) {
      offset = Math.max(0, offset - numResults);
      void fetchRecordings();
    }
  }

  // -------------------------------------------------------------------------
  // Helpers
  // -------------------------------------------------------------------------

  function thumbnailUrl(scientificName: string): string {
    return buildAppUrl(`/api/v2/media/species-image?name=${encodeURIComponent(scientificName)}`);
  }

  function audioUrl(id: number): string {
    return buildAppUrl(`/api/v2/audio/${id}`);
  }

  function openDetail(id: number): void {
    navigation.navigate(`/ui/detections/${id}`);
  }

  function percent(value: number): string {
    return `${Math.round(value * 100)}%`;
  }

  // Colour the correct-rate so a glance shows accuracy (green good, red poor).
  function correctRateClass(rate: number): string {
    if (rate >= 0.8) return 'text-[var(--color-success)]';
    if (rate >= 0.5) return 'text-[var(--color-warning)]';
    return 'text-[var(--color-error)]';
  }

  onMount(() => {
    void fetchSpecies();
    void loadCuration();
    void loadRangeScores();
  });
</script>

<div class="col-span-12 space-y-4">
  {#if view === 'table'}
    <!-- ============================ Table view ============================ -->
    <section class="card col-span-12 bg-[var(--color-base-100)] shadow-xs">
      <div class="card-body grow-0 p-2 sm:p-4 sm:pt-3">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <span class="card-title text-base sm:text-xl">All Species</span>
          <label class="relative">
            <Search
              class="pointer-events-none absolute left-2 top-1/2 size-4 -translate-y-1/2 opacity-50"
            />
            <input
              type="text"
              placeholder="Search species"
              bind:value={searchTerm}
              aria-label="Search species"
              class="h-9 w-48 rounded-lg border border-[var(--color-base-300)] bg-[var(--color-base-100)] pl-8 pr-2 text-sm"
            />
          </label>
        </div>

        {#if canEdit && reviewStatsTruncated}
          <p role="status" class="mt-1 text-xs text-[var(--color-warning)]">
            Correct % reflects a partial sample — there are too many reviewed detections to tally
            them all.
          </p>
        {/if}

        {#if loadingTable}
          <div role="status" aria-live="polite" class="py-12 text-center opacity-70">
            Loading species…
          </div>
        {:else if tableError}
          <div
            role="alert"
            class="flex items-center gap-3 rounded-lg border border-[var(--color-error)]/20 bg-[var(--color-error)]/10 p-4 text-[var(--color-error)]"
          >
            {tableError}
          </div>
        {:else if visibleSpecies.length === 0}
          <div class="py-12 text-center opacity-70">
            {species.length === 0 ? 'No species detected yet.' : 'No species match your search.'}
          </div>
        {:else}
          <div class="mt-2 overflow-x-auto">
            <table class="table table-zebra w-full">
              <thead>
                <tr>
                  <th class="w-16">Image</th>
                  <th aria-sort={ariaSort('name')}>
                    <button
                      type="button"
                      class="font-semibold"
                      onclick={() => setTableColumn('name')}
                    >
                      Species{sortIndicator('name')}
                    </button>
                  </th>
                  <th class="text-right" aria-sort={ariaSort('count')}>
                    <button
                      type="button"
                      class="font-semibold"
                      onclick={() => setTableColumn('count')}
                    >
                      Detections{sortIndicator('count')}
                    </button>
                  </th>
                  {#if canEdit}
                    <th class="text-right" aria-sort={ariaSort('correct')}>
                      <button
                        type="button"
                        class="font-semibold"
                        onclick={() => setTableColumn('correct')}
                        title="Share of human-reviewed detections marked correct"
                      >
                        Correct %{sortIndicator('correct')}
                      </button>
                    </th>
                    <th class="text-right" aria-sort={ariaSort('range')}>
                      <button
                        type="button"
                        class="font-semibold"
                        onclick={() => setTableColumn('range')}
                        title="Range-filter probability this species occurs here now"
                      >
                        Range %{sortIndicator('range')}
                      </button>
                    </th>
                    <th class="text-center" aria-sort={ariaSort('include')}>
                      <button
                        type="button"
                        class="font-semibold"
                        onclick={() => setTableColumn('include')}
                        title="Sort by: species on the always-include list first"
                      >
                        Incl{sortIndicator('include')}
                      </button>
                    </th>
                    <th class="text-center" aria-sort={ariaSort('confirm')}>
                      <button
                        type="button"
                        class="font-semibold"
                        onclick={() => setTableColumn('confirm')}
                        title="Sort by: confirmed species first"
                      >
                        Conf{sortIndicator('confirm')}
                      </button>
                    </th>
                    <th class="text-center" aria-sort={ariaSort('exclude')}>
                      <button
                        type="button"
                        class="font-semibold"
                        onclick={() => setTableColumn('exclude')}
                        title="Sort by: excluded species first"
                      >
                        Excl{sortIndicator('exclude')}
                      </button>
                    </th>
                    <th class="text-center">Delete</th>
                  {:else}
                    <th class="text-right" aria-sort={ariaSort('max')}>
                      <button
                        type="button"
                        class="font-semibold"
                        onclick={() => setTableColumn('max')}
                      >
                        Max conf.{sortIndicator('max')}
                      </button>
                    </th>
                    <th class="hidden sm:table-cell" aria-sort={ariaSort('first')}>
                      <button
                        type="button"
                        class="font-semibold"
                        onclick={() => setTableColumn('first')}
                      >
                        First seen{sortIndicator('first')}
                      </button>
                    </th>
                    <th class="hidden sm:table-cell" aria-sort={ariaSort('last')}>
                      <button
                        type="button"
                        class="font-semibold"
                        onclick={() => setTableColumn('last')}
                      >
                        Last seen{sortIndicator('last')}
                      </button>
                    </th>
                  {/if}
                </tr>
              </thead>
              <tbody>
                {#each visibleSpecies as s (s.scientific_name)}
                  {@const stat = reviewStats.get(s.scientific_name)}
                  {@const range = rangeScores.get(s.scientific_name)}
                  {@const busy = busySpecies.has(s.common_name)}
                  <tr class="hover">
                    <td>
                      <button
                        type="button"
                        onclick={() => openRecordings(s)}
                        aria-label={`View recordings of ${s.common_name}`}
                      >
                        <img
                          src={thumbnailUrl(s.scientific_name)}
                          alt={s.common_name}
                          loading="lazy"
                          class="size-12 rounded object-cover bg-[var(--color-base-200)]"
                        />
                      </button>
                    </td>
                    <td>
                      <button
                        type="button"
                        onclick={() => openRecordings(s)}
                        class="text-left transition-colors hover:text-[var(--color-primary)]"
                      >
                        <div class="font-medium">{s.common_name}</div>
                        <div class="text-xs italic opacity-60">{s.scientific_name}</div>
                      </button>
                    </td>
                    <td class="text-right tabular-nums">{s.count}</td>
                    {#if canEdit}
                      <td class="text-right tabular-nums">
                        {#if stat && stat.total > 0}
                          <span
                            class={correctRateClass(stat.correct_rate)}
                            title={`${stat.correct} correct / ${stat.false_positive} false of ${stat.total} reviewed`}
                          >
                            {percent(stat.correct_rate)}
                          </span>
                        {:else}
                          <span class="opacity-40">—</span>
                        {/if}
                      </td>
                      <td class="text-right tabular-nums">
                        {#if range != null}
                          {percent(range)}
                        {:else if rangeLoading}
                          <span
                            class="opacity-40"
                            title="Loading range-filter probability…"
                            aria-label="Loading range probability">…</span
                          >
                        {:else}
                          <span class="opacity-40" title="Below the range-filter threshold here"
                            >—</span
                          >
                        {/if}
                      </td>
                      <td class="text-center">
                        <button
                          type="button"
                          disabled={busy}
                          aria-pressed={includedSet.has(s.common_name)}
                          title="Always include this species"
                          class={`btn btn-xs ${includedSet.has(s.common_name) ? 'btn-success' : 'btn-outline'}`}
                          onclick={() => toggleMembership('include', s)}
                        >
                          <Plus class="size-3" />
                        </button>
                      </td>
                      <td class="text-center">
                        <button
                          type="button"
                          disabled={busy}
                          aria-pressed={confirmedSet.has(s.common_name)}
                          title="Mark this species as confirmed"
                          class={`btn btn-xs ${confirmedSet.has(s.common_name) ? 'btn-primary' : 'btn-outline'}`}
                          onclick={() => toggleMembership('confirm', s)}
                        >
                          <Check class="size-3" />
                        </button>
                      </td>
                      <td class="text-center">
                        <button
                          type="button"
                          disabled={busy}
                          aria-pressed={excludedSet.has(s.common_name)}
                          title="Exclude (ignore) this species"
                          class={`btn btn-xs ${excludedSet.has(s.common_name) ? 'btn-error' : 'btn-outline'}`}
                          onclick={() => toggleMembership('exclude', s)}
                        >
                          <Ban class="size-3" />
                        </button>
                      </td>
                      <td class="text-center">
                        <button
                          type="button"
                          class="btn btn-xs btn-ghost text-[var(--color-error)]"
                          title={`Delete all recordings of ${s.common_name}`}
                          aria-label={`Delete all recordings of ${s.common_name}`}
                          onclick={() => askDelete(s)}
                        >
                          <Trash2 class="size-4" />
                        </button>
                      </td>
                    {:else}
                      <td class="text-right tabular-nums">
                        {s.max_confidence != null ? percent(s.max_confidence) : '—'}
                      </td>
                      <td class="hidden text-sm sm:table-cell">{s.first_heard || '—'}</td>
                      <td class="hidden text-sm sm:table-cell">{s.last_heard || '—'}</td>
                    {/if}
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {/if}
      </div>
    </section>
  {:else}
    <!-- ========================= Recordings view ========================= -->
    <section class="card col-span-12 bg-[var(--color-base-100)] shadow-xs">
      <div class="card-body grow-0 p-2 sm:p-4 sm:pt-3">
        <div class="flex flex-wrap items-center gap-3">
          <button type="button" class="btn btn-ghost btn-sm gap-1" onclick={backToTable}>
            <ChevronLeft class="size-4" />
            All species
          </button>
          <div class="min-w-0 flex-1">
            <span class="card-title truncate text-base sm:text-xl">{selectedCommon}</span>
            <div class="text-xs italic opacity-60">{selectedScientific}</div>
          </div>
        </div>

        <!-- Controls: sort segment (the requested option) + results selector -->
        <div class="mt-2 flex flex-wrap items-center justify-between gap-3">
          <div class="join" role="group" aria-label="Sort recordings">
            <button
              type="button"
              class={`btn btn-sm join-item ${sortMode === 'confidence' ? 'btn-primary' : 'btn-outline'}`}
              aria-pressed={sortMode === 'confidence'}
              onclick={() => setSortMode('confidence')}
            >
              Max confidence
            </button>
            <button
              type="button"
              class={`btn btn-sm join-item ${sortMode === 'date' ? 'btn-primary' : 'btn-outline'}`}
              aria-pressed={sortMode === 'date'}
              onclick={() => setSortMode('date')}
            >
              Most recent
            </button>
            <button
              type="button"
              class={`btn btn-sm join-item ${sortMode === 'locked' ? 'btn-primary' : 'btn-outline'}`}
              aria-pressed={sortMode === 'locked'}
              onclick={() => setSortMode('locked')}
            >
              Locked only
            </button>
          </div>

          <label class="flex items-center gap-2 text-sm">
            <span class="opacity-70">Show</span>
            <select
              value={numResults}
              onchange={e => changeNumResults(Number((e.currentTarget as HTMLSelectElement).value))}
              class="h-8 rounded-lg border border-[var(--color-base-300)] bg-[var(--color-base-100)] px-2 text-sm"
            >
              {#each RESULTS_OPTIONS as option (option)}
                <option value={option}>{option}</option>
              {/each}
            </select>
          </label>
        </div>

        {#if loadingRecordings}
          <div role="status" aria-live="polite" class="py-12 text-center opacity-70">
            Loading recordings…
          </div>
        {:else if recordingsError}
          <div
            role="alert"
            class="flex items-center gap-3 rounded-lg border border-[var(--color-error)]/20 bg-[var(--color-error)]/10 p-4 text-[var(--color-error)]"
          >
            {recordingsError}
          </div>
        {:else if recordings.length === 0}
          <div class="py-12 text-center opacity-70">
            {sortMode === 'locked'
              ? 'No locked recordings for this species.'
              : 'No recordings found for this species.'}
          </div>
        {:else}
          <div class="mt-2 overflow-x-auto">
            <table class="table w-full">
              <thead>
                <tr>
                  <th>Species</th>
                  <th>Confidence</th>
                  <th>Date</th>
                  <th>Status</th>
                  <th class="hidden md:table-cell">Recording</th>
                </tr>
              </thead>
              <tbody>
                {#each recordings as d (d.id)}
                  <tr class="hover">
                    <td>
                      <button
                        type="button"
                        onclick={() => openDetail(d.id)}
                        class="flex items-center gap-3 text-left"
                      >
                        <img
                          src={thumbnailUrl(d.scientificName)}
                          alt={d.commonName}
                          loading="lazy"
                          class="size-10 rounded object-cover bg-[var(--color-base-200)]"
                        />
                        <span>
                          <span
                            class="block font-medium transition-colors hover:text-[var(--color-primary)]"
                          >
                            {d.commonName}
                          </span>
                          <span class="block text-xs italic opacity-60">{d.scientificName}</span>
                        </span>
                      </button>
                    </td>
                    <td class="tabular-nums">{percent(d.confidence)}</td>
                    <td class="whitespace-nowrap text-sm">{d.date} {d.time}</td>
                    <td>
                      {#if d.locked}
                        <span class="badge gap-1">
                          <Lock class="size-3" /> Locked
                        </span>
                      {:else if d.verified === 'correct'}
                        <span class="badge badge-success">Verified</span>
                      {:else if d.verified === 'false_positive'}
                        <span class="badge badge-error">False positive</span>
                      {:else}
                        <span class="opacity-40">—</span>
                      {/if}
                    </td>
                    <td class="hidden md:table-cell">
                      <SpectrogramPlayer
                        audioUrl={audioUrl(d.id)}
                        detectionId={d.id.toString()}
                        spectrogramSize="md"
                      />
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>

          <div class="mt-4 flex items-center justify-between text-sm">
            <span class="opacity-70">
              Showing {total === 0 ? 0 : offset + 1}–{Math.min(offset + numResults, total)} of {total}
            </span>
            <div class="join">
              <button
                type="button"
                class="btn btn-sm join-item"
                onclick={prevPage}
                disabled={offset === 0}
              >
                Prev
              </button>
              <button
                type="button"
                class="btn btn-sm join-item"
                onclick={nextPage}
                disabled={offset + numResults >= total}
              >
                Next
              </button>
            </div>
          </div>
        {/if}
      </div>
    </section>
  {/if}
</div>

<!-- ===================== Delete confirmation modal ===================== -->
{#if deleteTarget}
  {@const targetCommon = deleteTarget.common_name}
  {@const targetCount = deleteTarget.count}
  <div
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
    role="dialog"
    aria-modal="true"
    aria-label="Confirm delete"
  >
    <div class="w-full max-w-md rounded-lg bg-[var(--color-base-100)] p-6 shadow-xl">
      <h3 class="text-lg font-semibold">Delete all recordings?</h3>
      <p class="mt-2 text-sm text-[var(--color-base-content)]/80">
        This permanently deletes all <span class="font-medium">{targetCount}</span>
        recording{targetCount === 1 ? '' : 's'}
        of <span class="font-medium">{targetCommon}</span> across every date. Locked recordings are skipped.
        This cannot be undone.
      </p>
      {#if targetCount > DELETE_MAX_COLLECT}
        <p class="mt-2 text-sm text-[var(--color-warning)]">
          Only up to {DELETE_MAX_COLLECT.toLocaleString()} recordings are removed per pass, so you may
          need to run delete again to clear the rest.
        </p>
      {/if}
      <div class="mt-6 flex justify-end gap-2">
        <button
          type="button"
          class="btn btn-sm btn-ghost"
          onclick={cancelDelete}
          disabled={deleting}
        >
          Cancel
        </button>
        <button
          type="button"
          class="btn btn-sm btn-error"
          onclick={confirmDelete}
          disabled={deleting}
        >
          {deleting ? 'Deleting…' : 'Delete'}
        </button>
      </div>
    </div>
  </div>
{/if}
