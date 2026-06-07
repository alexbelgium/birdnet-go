<script lang="ts">
  import { t, type TranslationKey } from '$lib/i18n';
  import { getLocalDateString, parseLocalDateString } from '$lib/utils/date';
  import { downloadBlob } from '$lib/utils/fileHelpers';
  import { formatNumber, formatDateTime } from '$lib/utils/formatters';
  import { loggers } from '$lib/utils/logger';
  import { getStoredValue, setStoredValue } from '$lib/utils/storage';
  import { buildAppUrl } from '$lib/utils/urlHelpers';
  import { onMount } from 'svelte';
  import SortableHeader from '$lib/desktop/components/ui/SortableHeader.svelte';
  import ConfirmModal from '$lib/desktop/components/modals/ConfirmModal.svelte';
  import SpeciesFilterForm from '../components/forms/SpeciesFilterForm.svelte';
  import SpeciesDetailModal from '../components/modals/SpeciesDetailModal.svelte';
  import SpeciesCard from '../components/ui/SpeciesCard.svelte';
  import SpeciesCardMobile from '../components/ui/SpeciesCardMobile.svelte';
  import StatCard from '../components/ui/StatCard.svelte';
  import { Lock, Trash2, CheckCircle2, XCircle, Circle } from '@lucide/svelte';
  import { toastActions } from '$lib/stores/toast';
  import { fetchWithCSRF } from '$lib/utils/api';
  import { isAuthenticated } from '$lib/utils/auth';

  const logger = loggers.analytics;

  // Type definitions
  interface SpeciesFilters {
    timePeriod: 'all' | 'today' | 'week' | 'month' | '90days' | 'year' | 'custom';
    startDate: string;
    endDate: string;
    sortOrder:
      | 'count_desc'
      | 'count_asc'
      | 'name_asc'
      | 'name_desc'
      | 'first_seen_desc'
      | 'first_seen_asc'
      | 'last_seen_desc'
      | 'last_seen_asc'
      | 'confidence_desc'
      | 'confidence_asc'
      | 'max_confidence_desc'
      | 'max_confidence_asc'
      | 'excluded_desc'
      | 'excluded_asc'
      | 'included_desc'
      | 'included_asc';
    searchTerm: string;
  }

  type SortOrder = SpeciesFilters['sortOrder'];

  interface SpeciesData {
    common_name: string;
    scientific_name: string;
    count: number;
    avg_confidence: number;
    max_confidence: number;
    first_heard: string;
    last_heard: string;
    thumbnail_url?: string;
    // Manage-view only: number of detections manually reviewed as correct/false-positive.
    verified_count?: number;
    rejected_count?: number;
    // Manage-view list membership flags (populated from fetched exclude/include lists).
    is_excluded?: boolean;
    is_included?: boolean;
  }

  // Per-species manual-review counts returned by /analytics/species/review-stats.
  interface SpeciesReviewStat {
    scientific_name: string;
    common_name: string;
    total: number;
    verified: number;
    rejected: number;
  }

  type ViewMode = 'grid' | 'list' | 'manage';

  // Species name defaults to ascending (A→Z); every other column defaults to
  // descending (most/highest/most recent first) on first click.
  const SORTABLE_COLUMNS: {
    field: string;
    labelKey: TranslationKey;
    asc: SortOrder;
    desc: SortOrder;
  }[] = [
    {
      field: 'species',
      labelKey: 'analytics.species.headers.species',
      asc: 'name_asc',
      desc: 'name_desc',
    },
    {
      field: 'count',
      labelKey: 'analytics.species.headers.detections',
      asc: 'count_asc',
      desc: 'count_desc',
    },
    {
      field: 'avg_confidence',
      labelKey: 'analytics.species.headers.avgConfidence',
      asc: 'confidence_asc',
      desc: 'confidence_desc',
    },
    {
      field: 'max_confidence',
      labelKey: 'analytics.species.headers.maxConfidence',
      asc: 'max_confidence_asc',
      desc: 'max_confidence_desc',
    },
    {
      field: 'first_seen',
      labelKey: 'analytics.species.headers.firstDetected',
      asc: 'first_seen_asc',
      desc: 'first_seen_desc',
    },
    {
      field: 'last_seen',
      labelKey: 'analytics.species.headers.lastDetected',
      asc: 'last_seen_asc',
      desc: 'last_seen_desc',
    },
  ];

  // Manage-view-only sortable columns (excluded/whitelisted membership toggles).
  const MANAGE_SORTABLE_COLUMNS: {
    field: string;
    labelKey: TranslationKey;
    asc: SortOrder;
    desc: SortOrder;
  }[] = [
    {
      field: 'excluded',
      labelKey: 'analytics.species.headers.excluded',
      asc: 'excluded_asc',
      desc: 'excluded_desc',
    },
    {
      field: 'included',
      labelKey: 'analytics.species.headers.whitelisted',
      asc: 'included_asc',
      desc: 'included_desc',
    },
  ];

  // Combined column list for sort handling (used by handleSort and activeColumn).
  const ALL_SORTABLE_COLUMNS = [...SORTABLE_COLUMNS, ...MANAGE_SORTABLE_COLUMNS];

  // Default sort and persistence (survives a page refresh).
  const DEFAULT_SORT_ORDER: SortOrder = 'count_desc';
  const SORT_STORAGE_KEY = 'analytics.species.sortOrder';
  // Only the species-name column defaults to ascending (A→Z) on first click.
  const SPECIES_COLUMN_FIELD = 'species';
  const VALID_SORT_ORDERS: Set<string> = new Set<string>(
    ALL_SORTABLE_COLUMNS.flatMap(column => [column.asc, column.desc])
  );

  function isSortOrder(value: unknown): value is SortOrder {
    return typeof value === 'string' && VALID_SORT_ORDERS.has(value);
  }

  let isLoading = $state<boolean>(true);
  let speciesData = $state<SpeciesData[]>([]);
  let filteredSpecies = $state<SpeciesData[]>([]);
  let viewMode = $state<ViewMode>('grid');
  let selectedSpecies = $state<SpeciesData | null>(null);
  let showDetailModal = $state(false);

  // Toggle response types for the exclude/include list endpoints.
  interface ExcludeToggleResponse {
    common_name: string;
    action: string;
    is_excluded: boolean;
  }
  interface IncludeToggleResponse {
    common_name: string;
    action: string;
    is_included: boolean;
  }

  // Species management (manage view): per-species review stats + delete confirmation.
  let reviewStats = $state<SpeciesReviewStat[]>([]);
  let isLoadingStats = $state(false);
  let showDeleteModal = $state(false);
  let deleteTarget = $state<{ scientific_name: string; common_name: string; count: number } | null>(
    null
  );

  // Manage-view exclude/include list state. Sets contain common names (matching the API).
  let excludedSpecies = $state<Set<string>>(new Set());
  let includedSpecies = $state<Set<string>>(new Set());
  // Tracks species whose toggle is in-flight (prevents double-click races).
  let togglingExclude = $state<Set<string>>(new Set());
  let togglingInclude = $state<Set<string>>(new Set());

  // True while the manage view is loading its review stats.
  let manageLoading = $derived(viewMode === 'manage' && isLoadingStats);

  // Manage rows: authoritative species set from review-stats (so fully-rejected mislabels
  // appear even though they are excluded from the false-positive-filtered summary),
  // enriched with summary display fields (thumbnail/confidence/dates) where available,
  // and with list-membership flags for the sortable Excluded/Whitelisted columns.
  let manageSpecies = $derived.by(() => {
    const summaryByName = new Map(speciesData.map(s => [s.scientific_name, s]));
    const rows: SpeciesData[] = reviewStats.map(stat => {
      const summary = summaryByName.get(stat.scientific_name);
      const commonName = stat.common_name || summary?.common_name || stat.scientific_name;
      return {
        common_name: commonName,
        scientific_name: stat.scientific_name,
        count: stat.total,
        avg_confidence: summary?.avg_confidence ?? 0,
        max_confidence: summary?.max_confidence ?? 0,
        first_heard: summary?.first_heard ?? '',
        last_heard: summary?.last_heard ?? '',
        thumbnail_url: summary?.thumbnail_url,
        verified_count: stat.verified,
        rejected_count: stat.rejected,
        is_excluded: excludedSpecies.has(commonName),
        is_included: includedSpecies.has(commonName),
      };
    });
    return sortSpeciesList(rows.filter(matchesSearch), appliedSortOrder);
  });

  // Rows rendered by the shared list/manage table.
  let displayedSpecies = $derived(viewMode === 'manage' ? manageSpecies : filteredSpecies);

  // Read once so both filters and the applied-sort indicator start at the same persisted value.
  const restoredSortOrder = getStoredValue<SortOrder>(
    SORT_STORAGE_KEY,
    DEFAULT_SORT_ORDER,
    isSortOrder
  );

  let filters = $state<SpeciesFilters>({
    timePeriod: 'all',
    startDate: '',
    endDate: '',
    sortOrder: restoredSortOrder,
    searchTerm: '',
  });

  // Tracks the sort order that is actually applied to the table. Only the
  // explicit commit points (header click in handleSort, Apply Filters/mount/reset
  // via fetchData) update it; applyFilters() renders from it without mutating it.
  // This keeps a pending dropdown change from being committed by an unrelated
  // applyFilters() call (e.g. a background thumbnail batch or a search rerender).
  let appliedSortOrder = $state<SortOrder>(restoredSortOrder);

  // Active column + direction for the header indicators, derived from the
  // applied sort (not the pending dropdown selection).
  let activeColumn = $derived(
    ALL_SORTABLE_COLUMNS.find(
      column => column.asc === appliedSortOrder || column.desc === appliedSortOrder
    )
  );
  let sortField = $derived(activeColumn?.field ?? '');
  let sortDirection: 'asc' | 'desc' = $derived(
    activeColumn?.desc === appliedSortOrder ? 'desc' : 'asc'
  );

  // Clicking a header: re-clicking the active column toggles direction; a new
  // column starts at its default (ascending for species name, descending else).
  function handleSort(field: string) {
    const column = ALL_SORTABLE_COLUMNS.find(c => c.field === field);
    if (!column) return;
    const next =
      sortField === field
        ? appliedSortOrder === column.asc
          ? column.desc
          : column.asc
        : field === SPECIES_COLUMN_FIELD
          ? column.asc
          : column.desc;
    filters.sortOrder = next;
    appliedSortOrder = next;
    setStoredValue<SortOrder>(SORT_STORAGE_KEY, next);
    applyFilters();
  }

  // Set default dates on mount
  onMount(() => {
    const today = new Date();
    const lastMonth = new Date();
    lastMonth.setDate(today.getDate() - 30);

    filters.endDate = formatDateForInput(today);
    filters.startDate = formatDateForInput(lastMonth);

    // Fetch initial data
    fetchData();
  });

  function formatDateForInput(date: Date): string {
    return getLocalDateString(date);
  }

  function formatPercentage(value: number): string {
    return (value * 100).toFixed(1) + '%';
  }

  async function fetchData() {
    isLoading = true;
    // Apply Filters (and mount/reset) commit the pending dropdown selection.
    appliedSortOrder = filters.sortOrder;
    setStoredValue<SortOrder>(SORT_STORAGE_KEY, filters.sortOrder);

    try {
      // Determine date range based on time period
      let startDate, endDate;
      const today = new Date();

      switch (filters.timePeriod) {
        case 'today':
          startDate = formatDateForInput(today);
          endDate = startDate;
          break;
        case 'week':
          endDate = formatDateForInput(today);
          startDate = formatDateForInput(new Date(today.getTime() - 6 * 24 * 60 * 60 * 1000));
          break;
        case 'month':
          endDate = formatDateForInput(today);
          startDate = formatDateForInput(new Date(today.getTime() - 29 * 24 * 60 * 60 * 1000));
          break;
        case '90days':
          endDate = formatDateForInput(today);
          startDate = formatDateForInput(new Date(today.getTime() - 89 * 24 * 60 * 60 * 1000));
          break;
        case 'year':
          endDate = formatDateForInput(today);
          startDate = formatDateForInput(new Date(today.getTime() - 364 * 24 * 60 * 60 * 1000));
          break;
        case 'custom':
          startDate = filters.startDate;
          endDate = filters.endDate;
          break;
        case 'all':
        default:
          startDate = null;
          endDate = null;
          break;
      }

      // Build query parameters
      const params = new URLSearchParams();
      if (startDate) params.set('start_date', startDate);
      if (endDate) params.set('end_date', endDate);

      // Fetch species summary data
      const response = await fetch(
        buildAppUrl(`/api/v2/analytics/species/summary?${params.toString()}`)
      );

      if (!response.ok) {
        throw new Error(`Server responded with ${response.status}`);
      }

      const rawSpecies: SpeciesData[] = await response.json();
      // Backend returns relative URLs (e.g. /api/v2/media/image/...). Run them
      // through buildAppUrl so they include the configured base path (e.g.
      // /birdnet, HA Ingress token) before they end up in <img src=...>.
      speciesData = rawSpecies.map(species =>
        species.thumbnail_url
          ? { ...species, thumbnail_url: buildAppUrl(species.thumbnail_url) }
          : species
      );
      applyFilters();

      // Load thumbnails asynchronously after main data is displayed
      loadThumbnailsAsync();
    } catch (error) {
      logger.error('Error fetching species data:', error);
      speciesData = [];
      filteredSpecies = [];
    } finally {
      isLoading = false;
    }
  }

  function makeDateComparator(field: 'first_heard' | 'last_heard', ascending: boolean) {
    return (a: SpeciesData, b: SpeciesData) => {
      // eslint-disable-next-line security/detect-object-injection
      const da = parseLocalDateString(a[field]);
      // eslint-disable-next-line security/detect-object-injection
      const db = parseLocalDateString(b[field]);
      // Sort invalid/missing dates consistently to the end so the comparator
      // stays transitive (returning 0 for any null pair would break sort order).
      if (!da && !db) return 0;
      if (!da) return 1;
      if (!db) return -1;
      return ascending ? da.getTime() - db.getTime() : db.getTime() - da.getTime();
    };
  }

  // Search predicate shared by the list and manage views.
  function matchesSearch(species: SpeciesData): boolean {
    if (!filters.searchTerm) return true;
    const searchLower = filters.searchTerm.toLowerCase();
    return (
      species.common_name.toLowerCase().includes(searchLower) ||
      species.scientific_name.toLowerCase().includes(searchLower)
    );
  }

  // Returns a sorted copy of the list for the given committed sort order.
  function sortSpeciesList(list: SpeciesData[], order: SortOrder): SpeciesData[] {
    const sorted = [...list];
    switch (order) {
      case 'count_desc':
        sorted.sort((a, b) => b.count - a.count);
        break;
      case 'count_asc':
        sorted.sort((a, b) => a.count - b.count);
        break;
      case 'name_asc':
        sorted.sort((a, b) => a.common_name.localeCompare(b.common_name));
        break;
      case 'name_desc':
        sorted.sort((a, b) => b.common_name.localeCompare(a.common_name));
        break;
      case 'first_seen_desc':
        sorted.sort(makeDateComparator('first_heard', false));
        break;
      case 'first_seen_asc':
        sorted.sort(makeDateComparator('first_heard', true));
        break;
      case 'last_seen_desc':
        sorted.sort(makeDateComparator('last_heard', false));
        break;
      case 'last_seen_asc':
        sorted.sort(makeDateComparator('last_heard', true));
        break;
      case 'confidence_desc':
        sorted.sort((a, b) => b.avg_confidence - a.avg_confidence);
        break;
      case 'confidence_asc':
        sorted.sort((a, b) => a.avg_confidence - b.avg_confidence);
        break;
      case 'max_confidence_desc':
        sorted.sort((a, b) => b.max_confidence - a.max_confidence);
        break;
      case 'max_confidence_asc':
        sorted.sort((a, b) => a.max_confidence - b.max_confidence);
        break;
      case 'excluded_desc':
        sorted.sort((a, b) => Number(b.is_excluded ?? false) - Number(a.is_excluded ?? false));
        break;
      case 'excluded_asc':
        sorted.sort((a, b) => Number(a.is_excluded ?? false) - Number(b.is_excluded ?? false));
        break;
      case 'included_desc':
        sorted.sort((a, b) => Number(b.is_included ?? false) - Number(a.is_included ?? false));
        break;
      case 'included_asc':
        sorted.sort((a, b) => Number(a.is_included ?? false) - Number(b.is_included ?? false));
        break;
      default: {
        // Exhaustiveness guard: adding a SortOrder value without a case is a compile error.
        const _exhaustive: never = order;
        void _exhaustive;
      }
    }
    return sorted;
  }

  function applyFilters() {
    filteredSpecies = sortSpeciesList(speciesData.filter(matchesSearch), appliedSortOrder);
  }

  // Fraction of manually reviewed detections marked correct, or null when none reviewed.
  function reviewedRatio(species: SpeciesData): number | null {
    const verified = species.verified_count ?? 0;
    const rejected = species.rejected_count ?? 0;
    const reviewed = verified + rejected;
    return reviewed === 0 ? null : verified / reviewed;
  }

  async function fetchReviewStats() {
    isLoadingStats = true;
    try {
      const response = await fetch(buildAppUrl('/api/v2/analytics/species/review-stats'));
      if (!response.ok) {
        throw new Error(`Server responded with ${response.status}`);
      }
      reviewStats = await response.json();
    } catch (error) {
      logger.error('Error fetching species review stats:', error);
      reviewStats = [];
      toastActions.error(t('analytics.species.manage.statsError'));
    } finally {
      isLoadingStats = false;
    }
  }

  function showManageView() {
    viewMode = 'manage';
    void fetchManageData();
  }

  async function fetchManageData() {
    await Promise.all([fetchReviewStats(), fetchExcludedSpecies(), fetchIncludedSpecies()]);
  }

  async function fetchExcludedSpecies() {
    try {
      const response = await fetch(buildAppUrl('/api/v2/detections/ignored'));
      if (!response.ok) throw new Error(`Server responded with ${response.status}`);
      const data = (await response.json()) as { species: string[] };
      excludedSpecies = new Set(data.species);
    } catch (error) {
      logger.error('Error fetching excluded species:', error);
    }
  }

  async function fetchIncludedSpecies() {
    try {
      const response = await fetch(buildAppUrl('/api/v2/detections/included'));
      if (!response.ok) throw new Error(`Server responded with ${response.status}`);
      const data = (await response.json()) as { species: string[] };
      includedSpecies = new Set(data.species);
    } catch (error) {
      logger.error('Error fetching included species:', error);
    }
  }

  async function toggleExcluded(species: SpeciesData) {
    const name = species.common_name;
    if (togglingExclude.has(name)) return;

    // Optimistic update.
    const newToggling = new Set(togglingExclude);
    newToggling.add(name);
    togglingExclude = newToggling;

    const wasExcluded = excludedSpecies.has(name);
    const optimistic = new Set(excludedSpecies);
    if (wasExcluded) {
      optimistic.delete(name);
    } else {
      optimistic.add(name);
    }
    excludedSpecies = optimistic;

    try {
      const resp = await fetchWithCSRF<ExcludeToggleResponse>('/api/v2/detections/ignore', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ common_name: name }),
      });
      // Reconcile with authoritative server state.
      const confirmed = new Set(excludedSpecies);
      if (resp.is_excluded) {
        confirmed.add(name);
      } else {
        confirmed.delete(name);
      }
      excludedSpecies = confirmed;
      toastActions.success(
        resp.action === 'added'
          ? t('analytics.species.manage.addedToExcluded', { species: name })
          : t('analytics.species.manage.removedFromExcluded', { species: name })
      );
    } catch (error) {
      // Revert optimistic update on failure.
      const reverted = new Set(excludedSpecies);
      if (wasExcluded) {
        reverted.add(name);
      } else {
        reverted.delete(name);
      }
      excludedSpecies = reverted;
      logger.error('Error toggling excluded species:', error);
      toastActions.error(t('analytics.species.manage.toggleError'));
    } finally {
      const done = new Set(togglingExclude);
      done.delete(name);
      togglingExclude = done;
    }
  }

  async function toggleIncluded(species: SpeciesData) {
    const name = species.common_name;
    if (togglingInclude.has(name)) return;

    // Optimistic update.
    const newToggling = new Set(togglingInclude);
    newToggling.add(name);
    togglingInclude = newToggling;

    const wasIncluded = includedSpecies.has(name);
    const optimistic = new Set(includedSpecies);
    if (wasIncluded) {
      optimistic.delete(name);
    } else {
      optimistic.add(name);
    }
    includedSpecies = optimistic;

    try {
      const resp = await fetchWithCSRF<IncludeToggleResponse>('/api/v2/detections/include', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ common_name: name }),
      });
      // Reconcile with authoritative server state.
      const confirmed = new Set(includedSpecies);
      if (resp.is_included) {
        confirmed.add(name);
      } else {
        confirmed.delete(name);
      }
      includedSpecies = confirmed;
      toastActions.success(
        resp.action === 'added'
          ? t('analytics.species.manage.addedToWhitelist', { species: name })
          : t('analytics.species.manage.removedFromWhitelist', { species: name })
      );
    } catch (error) {
      // Revert optimistic update on failure.
      const reverted = new Set(includedSpecies);
      if (wasIncluded) {
        reverted.add(name);
      } else {
        reverted.delete(name);
      }
      includedSpecies = reverted;
      logger.error('Error toggling included species:', error);
      toastActions.error(t('analytics.species.manage.toggleError'));
    } finally {
      const done = new Set(togglingInclude);
      done.delete(name);
      togglingInclude = done;
    }
  }

  function requestDeleteSpecies(species: SpeciesData) {
    deleteTarget = {
      scientific_name: species.scientific_name,
      common_name: species.common_name,
      count: species.count,
    };
    showDeleteModal = true;
  }

  function cancelDeleteSpecies() {
    showDeleteModal = false;
    deleteTarget = null;
  }

  async function confirmDeleteSpecies() {
    const target = deleteTarget;
    if (!target) return;
    try {
      const resp = await fetchWithCSRF<{ deleted: number; skipped: number }>(
        '/api/v2/detections/species/delete',
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ scientific_name: target.scientific_name }),
        }
      );
      showDeleteModal = false;
      deleteTarget = null;
      if (resp.skipped > 0) {
        toastActions.info(
          t('analytics.species.manage.deletePartial', {
            species: target.common_name,
            deleted: resp.deleted,
            skipped: resp.skipped,
          })
        );
      } else {
        toastActions.success(
          t('analytics.species.manage.deleteSuccess', {
            species: target.common_name,
            deleted: resp.deleted,
          })
        );
      }
      // Refresh both data sources so the deleted species disappears from the view.
      await fetchData();
      await fetchReviewStats();
    } catch (error) {
      logger.error('Error deleting species:', error);
      toastActions.error(
        t('analytics.species.manage.deleteError', { species: target.common_name })
      );
    }
  }

  function getFilteredCount(): number {
    return filteredSpecies.length;
  }

  function getTotalSpeciesCount(): number {
    return speciesData.length;
  }

  function getTotalDetections(): number {
    return speciesData.reduce((sum, species) => sum + species.count, 0);
  }

  function getTotalDetectionsText(): string {
    const total = getTotalDetections();
    return `${formatNumber(total)} ${t('analytics.stats.detections')}`;
  }

  function getAverageConfidence(): string {
    if (speciesData.length === 0) return '0%';
    const totalWeighted = speciesData.reduce(
      (sum, species) => sum + species.avg_confidence * species.count,
      0
    );
    const totalCount = getTotalDetections();
    if (totalCount === 0) return '0%';
    return ((totalWeighted / totalCount) * 100).toFixed(1) + '%';
  }

  function resetFilters() {
    filters.timePeriod = 'all';
    filters.sortOrder = DEFAULT_SORT_ORDER;
    // fetchData() below commits and persists the reset sort order (single commit point).
    filters.searchTerm = '';

    const today = new Date();
    const lastMonth = new Date();
    lastMonth.setDate(today.getDate() - 30);

    filters.endDate = formatDateForInput(today);
    filters.startDate = formatDateForInput(lastMonth);

    fetchData();
  }

  async function loadThumbnailsAsync() {
    // Skip if we don't have species data
    if (!speciesData || speciesData.length === 0) {
      return;
    }

    // Get scientific names that need thumbnails
    const scientificNames = speciesData
      .filter(species => !species.thumbnail_url)
      .map(species => species.scientific_name);

    if (scientificNames.length === 0) {
      return;
    }

    try {
      // Fetch thumbnails in batches to avoid overwhelming the server
      const batchSize = 20;
      for (let i = 0; i < scientificNames.length; i += batchSize) {
        const batch = scientificNames.slice(i, i + batchSize);

        // Create query parameters for this batch
        const params = new URLSearchParams();
        batch.forEach(name => params.append('species', name));

        // Fetch thumbnails for this batch
        const response = await fetch(
          buildAppUrl(`/api/v2/analytics/species/thumbnails?${params.toString()}`)
        );
        if (response.ok) {
          const thumbnails = await response.json();

          // Update species data with fetched thumbnails. Backend URLs are
          // relative; buildAppUrl prepends the configured base path so the
          // image request resolves correctly behind a reverse proxy.
          speciesData = speciesData.map(species => {
            const url = thumbnails[species.scientific_name];
            if (url) {
              return { ...species, thumbnail_url: buildAppUrl(url) };
            }
            return species;
          });

          // Re-apply filters to update the view
          applyFilters();
        }

        // Small delay between batches
        if (i + batchSize < scientificNames.length) {
          await new Promise(resolve => setTimeout(resolve, 100));
        }
      }
    } catch (error) {
      logger.error('Error loading thumbnails:', error);
      // Continue without thumbnails - don't break the UI
    }
  }

  function exportData() {
    // Generate CSV content
    const headers = [
      'Common Name',
      'Scientific Name',
      'Count',
      'Avg Confidence',
      'Max Confidence',
      'First Detected',
      'Last Detected',
    ];
    const rows = filteredSpecies.map(species => [
      species.common_name,
      species.scientific_name,
      species.count,
      (species.avg_confidence * 100).toFixed(1) + '%',
      (species.max_confidence * 100).toFixed(1) + '%',
      species.first_heard ? formatDateTime(species.first_heard) : '',
      species.last_heard ? formatDateTime(species.last_heard) : '',
    ]);

    // Create CSV string
    const csvContent = [
      headers.join(','),
      ...rows.map(row => row.map(cell => `"${cell}"`).join(',')),
    ].join('\n');

    // Create and download file
    const blob = new Blob([csvContent], { type: 'text/csv;charset=utf-8;' });
    downloadBlob(blob, `birdnet-species-${getLocalDateString()}.csv`);
  }

  let searchDebounce: ReturnType<typeof setTimeout> | undefined;

  function handleSearchInput(e: Event): void {
    const target = e.target as HTMLInputElement;
    filters.searchTerm = target.value;
    // Debounce the filter application
    clearTimeout(searchDebounce);
    searchDebounce = setTimeout(() => {
      applyFilters();
    }, 300);
  }

  function handleSpeciesClick(species: SpeciesData) {
    selectedSpecies = species;
    showDetailModal = true;
  }

  function handleCloseDetailModal() {
    showDetailModal = false;
    selectedSpecies = null;
  }
</script>

<div class="col-span-12 space-y-4" role="region" aria-label={t('analytics.species.title')}>
  <!-- Page Header -->
  <div class="card bg-[var(--color-base-100)] shadow-xs">
    <div class="card-body card-padding">
      <div class="flex justify-between items-start">
        <div>
          <h1 class="card-title text-2xl">{t('analytics.species.title')}</h1>
          <p class="text-[var(--color-base-content)] opacity-60">
            {t('analytics.species.subtitle')}
          </p>
        </div>
        <div class="flex gap-4">
          <StatCard
            title={t('analytics.stats.totalSpecies')}
            value={getTotalSpeciesCount()}
            subtitle={getTotalDetectionsText()}
            iconClassName="bg-[var(--color-primary)]/20"
          >
            {#snippet icon()}
              <svg
                xmlns="http://www.w3.org/2000/svg"
                class="h-6 w-6 text-[var(--color-primary)]"
                viewBox="0 0 20 20"
                fill="currentColor"
              >
                <path
                  d="M5 3a2 2 0 00-2 2v2a2 2 0 002 2h2a2 2 0 002-2V5a2 2 0 00-2-2H5zM5 11a2 2 0 00-2 2v2a2 2 0 002 2h2a2 2 0 002-2v-2a2 2 0 00-2-2H5zM11 5a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2V5zM13 11a2 2 0 00-2 2v2a2 2 0 002 2h2a2 2 0 002-2v-2a2 2 0 00-2-2h-2z"
                />
              </svg>
            {/snippet}
          </StatCard>

          <StatCard
            title={t('analytics.stats.avgConfidence')}
            value={getAverageConfidence()}
            subtitle={t('analytics.stats.overallAverage')}
            iconClassName="bg-[var(--color-secondary)]/20"
          >
            {#snippet icon()}
              <svg
                xmlns="http://www.w3.org/2000/svg"
                class="h-6 w-6 text-[var(--color-secondary)]"
                viewBox="0 0 20 20"
                fill="currentColor"
              >
                <path
                  fill-rule="evenodd"
                  d="M18 10a8 8 0 11-16 0 8 8 0 0116 0zm-7-4a1 1 0 11-2 0 1 1 0 012 0zM9 9a.75.75 0 000 1.5h.253a.25.25 0 01.244.304l-.459 2.066A1.75 1.75 0 0010.747 15H11a.75.75 0 000-1.5h-.253a.25.25 0 01-.244-.304l.459-2.066A1.75 1.75 0 009.253 9H9z"
                  clip-rule="evenodd"
                />
              </svg>
            {/snippet}
          </StatCard>
        </div>
      </div>
    </div>
  </div>

  <!-- Filter Controls -->
  <SpeciesFilterForm
    bind:filters
    {isLoading}
    filteredCount={getFilteredCount()}
    onSubmit={fetchData}
    onReset={resetFilters}
    onExport={exportData}
    onSearchInput={handleSearchInput}
  />

  <!-- Species Grid/List -->
  <div class="card bg-[var(--color-base-100)] shadow-xs">
    <div class="card-body card-padding">
      <!-- View Toggle -->
      <div class="flex justify-between items-center mb-4">
        <h2 class="card-title">{t('analytics.species.speciesList')}</h2>
        <div class="join hidden sm:flex">
          <button
            class="btn btn-sm join-item"
            class:btn-active={viewMode === 'grid'}
            onclick={() => (viewMode = 'grid')}
            aria-label={t('analytics.species.switchToGrid')}
          >
            <svg
              xmlns="http://www.w3.org/2000/svg"
              class="h-4 w-4"
              viewBox="0 0 20 20"
              fill="currentColor"
            >
              <path
                d="M5 3a2 2 0 00-2 2v2a2 2 0 002 2h2a2 2 0 002-2V5a2 2 0 00-2-2H5zM5 11a2 2 0 00-2 2v2a2 2 0 002 2h2a2 2 0 002-2v-2a2 2 0 00-2-2H5zM11 5a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2V5zM13 11a2 2 0 00-2 2v2a2 2 0 002 2h2a2 2 0 002-2v-2a2 2 0 00-2-2h-2z"
              />
            </svg>
          </button>
          <button
            class="btn btn-sm join-item"
            class:btn-active={viewMode === 'list'}
            onclick={() => (viewMode = 'list')}
            aria-label={t('analytics.species.switchToList')}
          >
            <svg
              xmlns="http://www.w3.org/2000/svg"
              class="h-4 w-4"
              viewBox="0 0 20 20"
              fill="currentColor"
            >
              <path
                fill-rule="evenodd"
                d="M3 4a1 1 0 011-1h12a1 1 0 110 2H4a1 1 0 01-1-1zm0 4a1 1 0 011-1h12a1 1 0 110 2H4a1 1 0 01-1-1zm0 4a1 1 0 011-1h12a1 1 0 110 2H4a1 1 0 01-1-1zm0 4a1 1 0 011-1h12a1 1 0 110 2H4a1 1 0 01-1-1z"
                clip-rule="evenodd"
              />
            </svg>
          </button>
          {#if $isAuthenticated}
            <button
              class="btn btn-sm join-item"
              class:btn-active={viewMode === 'manage'}
              onclick={showManageView}
              aria-label={t('analytics.species.switchToManage')}
              title={t('analytics.species.switchToManage')}
            >
              <Lock class="h-4 w-4" />
            </button>
          {/if}
        </div>
      </div>

      <!-- Loading State -->
      {#if isLoading || manageLoading}
        <div class="flex justify-center items-center p-8">
          <span class="loading loading-spinner loading-lg text-[var(--color-primary)]"></span>
        </div>
      {/if}

      <!-- Mobile View - Compact List -->
      {#if !isLoading && viewMode === 'grid' && filteredSpecies.length > 0}
        <div class="sm:hidden space-y-2">
          {#each filteredSpecies as species, index (`${species.scientific_name}_${index}`)}
            <SpeciesCardMobile {species} variant="compact" onClick={handleSpeciesClick} />
          {/each}
        </div>
      {/if}

      <!-- Desktop Grid View -->
      {#if !isLoading && viewMode === 'grid' && filteredSpecies.length > 0}
        <div class="species-grid hidden sm:grid">
          {#each filteredSpecies as species, index (`${species.scientific_name}_${index}`)}
            <SpeciesCard {species} />
          {/each}
        </div>
      {/if}

      <!-- List / Manage View (shared table; manage adds review + delete columns) -->
      {#if !isLoading && !manageLoading && (viewMode === 'list' || viewMode === 'manage')}
        <div class="overflow-x-auto">
          <table class="table w-full hidden sm:table">
            <thead>
              <tr>
                {#each SORTABLE_COLUMNS as { field, labelKey } (field)}
                  <SortableHeader
                    label={t(labelKey)}
                    {field}
                    activeField={sortField}
                    direction={sortDirection}
                    onSort={handleSort}
                  />
                {/each}
                {#if viewMode === 'manage'}
                  {#each MANAGE_SORTABLE_COLUMNS as { field, labelKey } (field)}
                    <SortableHeader
                      label={t(labelKey)}
                      {field}
                      activeField={sortField}
                      direction={sortDirection}
                      onSort={handleSort}
                    />
                  {/each}
                  <th>{t('analytics.species.headers.reviewRatio')}</th>
                  <th>{t('analytics.species.headers.actions')}</th>
                {/if}
              </tr>
            </thead>
            <tbody>
              {#each displayedSpecies as species, index (`${species.scientific_name}_${index}`)}
                {@const ratio = reviewedRatio(species)}
                <tr
                  class={index % 2 === 0
                    ? 'bg-[var(--color-base-100)]'
                    : 'bg-[var(--color-base-200)]'}
                >
                  <td>
                    <div class="flex items-center gap-3">
                      <div class="avatar">
                        <div
                          class="mask mask-squircle w-12 h-12"
                          class:bg-[var(--color-base-300)]={!species.thumbnail_url}
                        >
                          {#if species.thumbnail_url}
                            <img
                              src={species.thumbnail_url}
                              alt={species.common_name}
                              onerror={e => {
                                const img = e.target as HTMLImageElement;
                                if (img) {
                                  img.style.display = 'none';
                                  img.parentElement?.classList.add('bg-[var(--color-base-300)]');
                                }
                              }}
                            />
                          {/if}
                        </div>
                      </div>
                      <div>
                        <div class="font-bold">{species.common_name}</div>
                        <div class="text-sm opacity-50 italic">{species.scientific_name}</div>
                      </div>
                    </div>
                  </td>
                  <td class="font-semibold">{species.count}</td>
                  <td>
                    <div class="flex items-center gap-2">
                      <progress
                        class="progress w-20 {species.avg_confidence >= 0.8
                          ? 'progress-success'
                          : species.avg_confidence >= 0.4
                            ? 'progress-warning'
                            : 'progress-error'}"
                        value={species.avg_confidence}
                        max="1"
                      ></progress>
                      <span class="text-sm">{formatPercentage(species.avg_confidence)}</span>
                    </div>
                  </td>
                  <td>{formatPercentage(species.max_confidence)}</td>
                  <td class="text-sm">{formatDateTime(species.first_heard)}</td>
                  <td class="text-sm">{formatDateTime(species.last_heard)}</td>
                  {#if viewMode === 'manage'}
                    {@const isExcluded = species.is_excluded ?? false}
                    {@const isIncluded = species.is_included ?? false}
                    <td>
                      <button
                        type="button"
                        class="btn btn-ghost btn-xs"
                        class:text-[var(--color-error)]={isExcluded}
                        class:opacity-30={!isExcluded}
                        disabled={togglingExclude.has(species.common_name)}
                        onclick={() => void toggleExcluded(species)}
                        title={isExcluded
                          ? t('analytics.species.manage.removeFromExcludedTooltip', {
                              species: species.common_name,
                            })
                          : t('analytics.species.manage.addToExcludedTooltip', {
                              species: species.common_name,
                            })}
                        aria-label={isExcluded
                          ? t('analytics.species.manage.removeFromExcludedTooltip', {
                              species: species.common_name,
                            })
                          : t('analytics.species.manage.addToExcludedTooltip', {
                              species: species.common_name,
                            })}
                        aria-pressed={isExcluded}
                      >
                        {#if isExcluded}
                          <XCircle class="h-5 w-5" />
                        {:else}
                          <Circle class="h-5 w-5" />
                        {/if}
                      </button>
                    </td>
                    <td>
                      <button
                        type="button"
                        class="btn btn-ghost btn-xs"
                        class:text-[var(--color-success)]={isIncluded}
                        class:opacity-30={!isIncluded}
                        disabled={togglingInclude.has(species.common_name)}
                        onclick={() => void toggleIncluded(species)}
                        title={isIncluded
                          ? t('analytics.species.manage.removeFromWhitelistTooltip', {
                              species: species.common_name,
                            })
                          : t('analytics.species.manage.addToWhitelistTooltip', {
                              species: species.common_name,
                            })}
                        aria-label={isIncluded
                          ? t('analytics.species.manage.removeFromWhitelistTooltip', {
                              species: species.common_name,
                            })
                          : t('analytics.species.manage.addToWhitelistTooltip', {
                              species: species.common_name,
                            })}
                        aria-pressed={isIncluded}
                      >
                        {#if isIncluded}
                          <CheckCircle2 class="h-5 w-5" />
                        {:else}
                          <Circle class="h-5 w-5" />
                        {/if}
                      </button>
                    </td>
                    <td>
                      {#if ratio === null}
                        <span
                          class="text-sm opacity-50"
                          title={t('analytics.species.manage.noReviews')}
                        >
                          {t('analytics.species.manage.noReviewsShort')}
                        </span>
                      {:else}
                        <div
                          class="flex items-center gap-2"
                          title={t('analytics.species.manage.reviewCounts', {
                            verified: species.verified_count ?? 0,
                            rejected: species.rejected_count ?? 0,
                          })}
                        >
                          <progress
                            class="progress w-16 {ratio >= 0.5
                              ? 'progress-success'
                              : 'progress-error'}"
                            value={ratio}
                            max="1"
                          ></progress>
                          <span class="text-sm whitespace-nowrap">{formatPercentage(ratio)}</span>
                        </div>
                      {/if}
                    </td>
                    <td>
                      <button
                        type="button"
                        class="btn btn-ghost btn-sm text-[var(--color-error)]"
                        onclick={() => requestDeleteSpecies(species)}
                        aria-label={t('analytics.species.manage.deleteSpecies', {
                          species: species.common_name,
                        })}
                        title={t('analytics.species.manage.deleteSpecies', {
                          species: species.common_name,
                        })}
                      >
                        <Trash2 class="h-4 w-4" />
                      </button>
                    </td>
                  {/if}
                </tr>
              {/each}
            </tbody>
          </table>
          <!-- Mobile list view -->
          <div class="sm:hidden space-y-2">
            {#each displayedSpecies as species, index (`${species.scientific_name}_${index}`)}
              <SpeciesCardMobile {species} variant="list" onClick={handleSpeciesClick} />
            {/each}
          </div>
        </div>
      {/if}

      <!-- Empty State -->
      {#if !isLoading && !manageLoading && displayedSpecies.length === 0}
        <div class="text-center py-8 text-[var(--color-base-content)] opacity-50">
          <svg
            xmlns="http://www.w3.org/2000/svg"
            class="h-16 w-16 mx-auto mb-4 opacity-20"
            viewBox="0 0 20 20"
            fill="currentColor"
          >
            <path
              fill-rule="evenodd"
              d="M5 3a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2V5a2 2 0 00-2-2H5zm9 4a1 1 0 10-2 0v6a1 1 0 102 0V7zm-3 2a1 1 0 10-2 0v4a1 1 0 102 0V9zm-3 3a1 1 0 10-2 0v1a1 1 0 102 0v-1z"
              clip-rule="evenodd"
            />
          </svg>
          <p>{t('analytics.species.noSpeciesFound')}</p>
        </div>
      {/if}
    </div>
  </div>
</div>

<!-- Mobile Species Detail Modal -->
<SpeciesDetailModal
  species={selectedSpecies}
  isOpen={showDetailModal}
  onClose={handleCloseDetailModal}
/>

<!-- Delete-species confirmation (manage view) -->
<ConfirmModal
  isOpen={showDeleteModal}
  title={deleteTarget
    ? t('analytics.species.manage.confirmDeleteTitle', { species: deleteTarget.common_name })
    : ''}
  message={deleteTarget
    ? t('analytics.species.manage.confirmDeleteMessage', {
        species: deleteTarget.common_name,
        count: deleteTarget.count,
      })
    : ''}
  confirmLabel={t('common.delete')}
  confirmVariant="error"
  onClose={cancelDeleteSpecies}
  onConfirm={confirmDeleteSpecies}
/>

<!-- Mobile Audio Player -->

<style>
  .card-padding {
    padding: 1rem;
  }

  @media (min-width: 768px) {
    .card-padding {
      padding: 1.5rem;
    }
  }

  .species-grid {
    display: grid;
    grid-template-columns: 1fr;
    gap: 1rem;
  }

  @media (min-width: 768px) {
    .species-grid {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }

  @media (min-width: 1024px) {
    .species-grid {
      grid-template-columns: repeat(3, minmax(0, 1fr));
    }
  }

  @media (min-width: 1280px) {
    .species-grid {
      grid-template-columns: repeat(4, minmax(0, 1fr));
    }
  }
</style>
