<script lang="ts">
  import { t, getLocale, type TranslationKey } from '$lib/i18n';
  import { getLocalDateString, parseLocalDateString } from '$lib/utils/date';
  import { downloadBlob } from '$lib/utils/fileHelpers';
  import { formatNumber, formatDate, formatDateTime } from '$lib/utils/formatters';
  import { loggers } from '$lib/utils/logger';
  import { getStoredValue, setStoredValue } from '$lib/utils/storage';
  import { buildAppUrl } from '$lib/utils/urlHelpers';
  import { localizeSpeciesName } from '$lib/utils/speciesDisplay';
  import { onMount, onDestroy } from 'svelte';
  import SortableHeader from '$lib/desktop/components/ui/SortableHeader.svelte';
  import ConfirmModal from '$lib/desktop/components/modals/ConfirmModal.svelte';
  import SpeciesFilterForm from '../components/forms/SpeciesFilterForm.svelte';
  import SpeciesDetailModal from '../components/modals/SpeciesDetailModal.svelte';
  import SpeciesCard from '../components/ui/SpeciesCard.svelte';
  import SpeciesCardMobile from '../components/ui/SpeciesCardMobile.svelte';
  import StatCard from '../components/ui/StatCard.svelte';
  import { Lock, Trash2, CheckCircle2, XCircle, Circle, BadgeCheck } from '@lucide/svelte';
  import { toastActions } from '$lib/stores/toast';
  import { settingsActions, settingsStore } from '$lib/stores/settings';
  import { fetchWithCSRF } from '$lib/utils/api';
  import { isAuthenticated } from '$lib/utils/auth';
  import { get } from 'svelte/store';

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
      | 'included_asc'
      | 'review_ratio_desc'
      | 'review_ratio_asc'
      | 'range_score_desc'
      | 'range_score_asc'
      | 'confirmed_desc'
      | 'confirmed_asc';
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
    // Manage-view only: range-filter geomodel occurrence probability (0–1) for the active location/week.
    range_score?: number;
    // Manage-view only: species manually flagged as a genuine, confirmed occurrence.
    is_confirmed?: boolean;
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

  // One table-column descriptor drives both the sortable headers and the sort
  // handling. Species name defaults to ascending (A→Z); every other column defaults
  // to descending (most/highest/most recent first) on first click. Per-column flags:
  //   manageOnly  – rendered/sortable only in the manage view (and never persisted,
  //                 so a manage-only sort like review ratio doesn't leak into the
  //                 grid/list views, which can't show that column).
  //   hideInManage – hidden in the manage view to free horizontal space for the
  //                 management columns (avg confidence, first detection date).
  type SortableColumn = {
    field: string;
    labelKey: TranslationKey;
    asc: SortOrder;
    desc: SortOrder;
    manageOnly?: boolean;
    hideInManage?: boolean;
  };
  const SORTABLE_COLUMNS: SortableColumn[] = [
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
      hideInManage: true,
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
      hideInManage: true,
    },
    {
      field: 'last_seen',
      labelKey: 'analytics.species.headers.lastDetected',
      asc: 'last_seen_asc',
      desc: 'last_seen_desc',
    },
    {
      field: 'excluded',
      labelKey: 'analytics.species.headers.excluded',
      asc: 'excluded_asc',
      desc: 'excluded_desc',
      manageOnly: true,
    },
    {
      field: 'included',
      labelKey: 'analytics.species.headers.whitelisted',
      asc: 'included_asc',
      desc: 'included_desc',
      manageOnly: true,
    },
    {
      field: 'review_ratio',
      labelKey: 'analytics.species.headers.reviewRatio',
      asc: 'review_ratio_asc',
      desc: 'review_ratio_desc',
      manageOnly: true,
    },
    {
      field: 'range_score',
      labelKey: 'analytics.species.headers.probability',
      asc: 'range_score_asc',
      desc: 'range_score_desc',
      manageOnly: true,
    },
    {
      field: 'confirmed',
      labelKey: 'analytics.species.headers.confirmed',
      asc: 'confirmed_asc',
      desc: 'confirmed_desc',
      manageOnly: true,
    },
  ];

  // Manage view only: pin the header row while the (bounded-height) table body scrolls.
  // An opaque background keeps scrolled rows from showing through the sticky cells.
  const STICKY_HEADER_CLASS = 'sticky top-0 z-10 bg-[var(--color-base-100)]';

  // Default sort and persistence (survives a page refresh).
  const DEFAULT_SORT_ORDER: SortOrder = 'count_desc';
  const SORT_STORAGE_KEY = 'analytics.species.sortOrder';
  // Only the species-name column defaults to ascending (A→Z) on first click.
  const SPECIES_COLUMN_FIELD = 'species';

  const VALID_SORT_ORDERS: Set<string> = new Set<string>(
    SORTABLE_COLUMNS.flatMap(column => [column.asc, column.desc])
  );

  function isSortOrder(value: unknown): value is SortOrder {
    return typeof value === 'string' && VALID_SORT_ORDERS.has(value);
  }

  // Look up the column backing a sort order (used to resolve its field/direction
  // and whether it is manage-only, so manage sorts stay out of persistent storage).
  function columnForSortOrder(order: SortOrder): SortableColumn | undefined {
    return SORTABLE_COLUMNS.find(c => c.asc === order || c.desc === order);
  }

  let isLoading = $state<boolean>(true);
  let speciesData = $state<SpeciesData[]>([]);
  // Debounced copy of filters.searchTerm that actually drives filtering; the raw
  // filters.searchTerm stays live for the input box and the "filtered" badge.
  let debouncedSearchTerm = $state<string>('');
  let viewMode = $state<ViewMode>('grid');
  let selectedSpecies = $state<SpeciesData | null>(null);
  let showDetailModal = $state(false);

  // Shared response shape for the exclude/include/confirm list-toggle endpoints.
  // The server returns action and the membership flag in lockstep, so 'added'/'removed'
  // is the single source of truth for both the toast and the reconcile step below.
  interface SpeciesToggleResponse {
    common_name: string;
    action: string;
  }

  // Describes one manage-view membership column (excluded / whitelisted / confirmed):
  // the API list it toggles, how to read and write its local state, the response flag,
  // and the toast/tooltip copy plus button appearance. One descriptor per list lets
  // toggleSpeciesList and the toggle-cell snippet stay generic instead of triplicated.
  interface ManageListDescriptor {
    endpoint: string; // POST toggle endpoint
    listEndpoint: string; // GET endpoint returning current members
    activeClass: string;
    activeIcon: typeof Circle;
    getMembers: () => Set<string>;
    setMembers: (_next: Set<string>) => void;
    getToggling: () => Set<string>;
    setToggling: (_next: Set<string>) => void;
    // i18n keys passed straight to t(); typed as string (not TranslationKey) so these
    // manage-view keys need not be carried in the generated TranslationKey union.
    addedKey: string;
    removedKey: string;
    activeTooltipKey: string;
    inactiveTooltipKey: string;
  }

  // Species management (manage view): per-species review stats + delete confirmation.
  let reviewStats = $state<SpeciesReviewStat[]>([]);
  let isLoadingStats = $state(false);
  let showDeleteModal = $state(false);
  let deleteTarget = $state<{ scientific_name: string; common_name: string; count: number } | null>(
    null
  );

  // Manage-view exclude/include/confirmed list state. Sets contain common names (matching the API).
  let excludedSpecies = $state<Set<string>>(new Set());
  let includedSpecies = $state<Set<string>>(new Set());
  let confirmedSpecies = $state<Set<string>>(new Set());
  // Range-filter geomodel probabilities keyed by scientific name (manage view only).
  let rangeScores = $state<Map<string, number>>(new Map());
  let isLoadingScores = $state(false);
  // Tracks species whose toggle is in-flight (prevents double-click races).
  let togglingExclude = $state<Set<string>>(new Set());
  let togglingInclude = $state<Set<string>>(new Set());
  let togglingConfirmed = $state<Set<string>>(new Set());

  // Manage-view membership columns, in display order (Excluded, Whitelisted, Confirmed).
  // Each descriptor wires its API endpoint to the matching local state + UI affordances.
  const excludedList: ManageListDescriptor = {
    endpoint: '/api/v2/detections/ignore',
    listEndpoint: '/api/v2/detections/ignored',
    activeClass: 'text-[var(--color-error)]',
    activeIcon: XCircle,
    getMembers: () => excludedSpecies,
    setMembers: next => {
      excludedSpecies = next;
    },
    getToggling: () => togglingExclude,
    setToggling: next => {
      togglingExclude = next;
    },
    addedKey: 'analytics.species.manage.addedToExcluded',
    removedKey: 'analytics.species.manage.removedFromExcluded',
    activeTooltipKey: 'analytics.species.manage.removeFromExcludedTooltip',
    inactiveTooltipKey: 'analytics.species.manage.addToExcludedTooltip',
  };
  const includedList: ManageListDescriptor = {
    endpoint: '/api/v2/detections/include',
    listEndpoint: '/api/v2/detections/included',
    activeClass: 'text-[var(--color-success)]',
    activeIcon: CheckCircle2,
    getMembers: () => includedSpecies,
    setMembers: next => {
      includedSpecies = next;
    },
    getToggling: () => togglingInclude,
    setToggling: next => {
      togglingInclude = next;
    },
    addedKey: 'analytics.species.manage.addedToWhitelist',
    removedKey: 'analytics.species.manage.removedFromWhitelist',
    activeTooltipKey: 'analytics.species.manage.removeFromWhitelistTooltip',
    inactiveTooltipKey: 'analytics.species.manage.addToWhitelistTooltip',
  };
  const confirmedList: ManageListDescriptor = {
    endpoint: '/api/v2/detections/confirm',
    listEndpoint: '/api/v2/detections/confirmed',
    activeClass: 'text-[var(--color-primary)]',
    activeIcon: BadgeCheck,
    getMembers: () => confirmedSpecies,
    setMembers: next => {
      confirmedSpecies = next;
    },
    getToggling: () => togglingConfirmed,
    setToggling: next => {
      togglingConfirmed = next;
    },
    addedKey: 'analytics.species.manage.addedToConfirmed',
    removedKey: 'analytics.species.manage.removedFromConfirmed',
    activeTooltipKey: 'analytics.species.manage.unconfirmSpeciesTooltip',
    inactiveTooltipKey: 'analytics.species.manage.confirmSpeciesTooltip',
  };

  // True while the manage view is loading its review stats.
  let manageLoading = $derived(viewMode === 'manage' && isLoadingStats);

  // Columns shown for the current view: the manage view drops the hideInManage
  // columns (avg confidence, first detection) to make room and adds the manageOnly
  // management columns; grid/list views show neither set of manage extras.
  let visibleColumns = $derived(
    SORTABLE_COLUMNS.filter(column =>
      viewMode === 'manage' ? !column.hideInManage : !column.manageOnly
    )
  );

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
        range_score: rangeScores.get(stat.scientific_name),
        is_confirmed: confirmedSpecies.has(commonName),
      };
    });
    return sortSpeciesList(rows.filter(matchesSearch), appliedSortOrder);
  });

  // Rows rendered by the shared list/manage table. Wrapped in $derived.by so the
  // filteredSpecies reference is evaluated lazily (it is declared further below).
  let displayedSpecies = $derived.by(() =>
    viewMode === 'manage' ? manageSpecies : filteredSpecies
  );

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
  let activeColumn = $derived(columnForSortOrder(appliedSortOrder));
  let sortField = $derived(activeColumn?.field ?? '');
  let sortDirection: 'asc' | 'desc' = $derived(
    activeColumn?.desc === appliedSortOrder ? 'desc' : 'asc'
  );

  // Clicking a header: re-clicking the active column toggles direction; a new
  // column starts at its default (ascending for species name, descending else).
  function handleSort(field: string) {
    const column = SORTABLE_COLUMNS.find(c => c.field === field);
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
    // Persist only list/grid orders; manage-only sorts are session-scoped.
    if (!column.manageOnly) {
      setStoredValue<SortOrder>(SORT_STORAGE_KEY, next);
    }
    // filteredSpecies/manageSpecies are $derived and re-sort automatically on appliedSortOrder.
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

  // Manage view packs more columns in, so dates drop the clock time to save
  // horizontal space; the list view keeps the full date+time.
  function formatColumnDate(value: string): string {
    return viewMode === 'manage' ? formatDate(value) : formatDateTime(value);
  }

  // Increments on every fetchData so an in-flight thumbnail loop from a previous
  // fetch can detect it has been superseded and stop mutating speciesData.
  let thumbnailFetchSeq = 0;

  async function fetchData() {
    isLoading = true;
    const fetchSeq = ++thumbnailFetchSeq;
    // Apply Filters (and mount/reset) commit the pending dropdown selection.
    appliedSortOrder = filters.sortOrder;
    if (!columnForSortOrder(filters.sortOrder)?.manageOnly) {
      setStoredValue<SortOrder>(SORT_STORAGE_KEY, filters.sortOrder);
    }
    // Commit the search term immediately too, cancelling any pending debounce so
    // a just-typed term is not re-applied a moment later.
    clearTimeout(searchDebounce);
    debouncedSearchTerm = filters.searchTerm;

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

      // Load thumbnails asynchronously after main data is displayed
      loadThumbnailsAsync(fetchSeq);
    } catch (error) {
      logger.error('Error fetching species data:', error);
      speciesData = [];
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

  // Search predicate shared by the list and manage views. Matches the visitor-
  // localized common name (what the user sees) as well as the server common name
  // and scientific name. Reads debouncedSearchTerm so typing stays responsive.
  function matchesSearch(species: SpeciesData): boolean {
    const searchLower = debouncedSearchTerm.trim().toLowerCase();
    if (!searchLower) return true;
    const displayName = localizeSpeciesName(species.scientific_name, species.common_name);
    return (
      displayName.toLowerCase().includes(searchLower) ||
      species.common_name.toLowerCase().includes(searchLower) ||
      species.scientific_name.toLowerCase().includes(searchLower)
    );
  }

  // Returns a sorted copy of the list for the given committed sort order. Name
  // sorts use the visitor-localized common name so order matches what is shown.
  function sortSpeciesList(list: SpeciesData[], order: SortOrder): SpeciesData[] {
    const sorted = [...list];
    // Read the locale once per sort (not per comparison); localizeSpeciesName reads
    // the species-dictionary state, so the calling $derived re-runs on locale change.
    const locale = getLocale();
    const displayName = (s: SpeciesData) => localizeSpeciesName(s.scientific_name, s.common_name);
    switch (order) {
      case 'count_desc':
        sorted.sort((a, b) => b.count - a.count);
        break;
      case 'count_asc':
        sorted.sort((a, b) => a.count - b.count);
        break;
      case 'name_asc':
        sorted.sort((a, b) => displayName(a).localeCompare(displayName(b), locale));
        break;
      case 'name_desc':
        sorted.sort((a, b) => displayName(b).localeCompare(displayName(a), locale));
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
      case 'review_ratio_desc':
        // Unreviewed species (null ratio) sort last in both directions via the -1 sentinel.
        sorted.sort((a, b) => (reviewedRatio(b) ?? -1) - (reviewedRatio(a) ?? -1));
        break;
      case 'review_ratio_asc':
        sorted.sort((a, b) => (reviewedRatio(a) ?? -1) - (reviewedRatio(b) ?? -1));
        break;
      case 'range_score_desc':
        // Species with no geomodel score (undefined) sort last via the -1 sentinel.
        sorted.sort((a, b) => (b.range_score ?? -1) - (a.range_score ?? -1));
        break;
      case 'range_score_asc':
        sorted.sort((a, b) => (a.range_score ?? -1) - (b.range_score ?? -1));
        break;
      case 'confirmed_desc':
        sorted.sort((a, b) => Number(b.is_confirmed ?? false) - Number(a.is_confirmed ?? false));
        break;
      case 'confirmed_asc':
        sorted.sort((a, b) => Number(a.is_confirmed ?? false) - Number(b.is_confirmed ?? false));
        break;
      default: {
        // Exhaustiveness guard: adding a SortOrder value without a case is a compile error.
        const _exhaustive: never = order;
        void _exhaustive;
      }
    }
    return sorted;
  }

  // Filtered + sorted list rows. Search/sort run on the localized name via the
  // shared helpers, so this re-runs when speciesData, the search term, the sort,
  // the locale, or the species dictionary changes - no imperative applyFilters().
  let filteredSpecies = $derived(
    sortSpeciesList(speciesData.filter(matchesSearch), appliedSortOrder)
  );

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
      reviewStats = await fetchWithCSRF<SpeciesReviewStat[]>(
        '/api/v2/analytics/species/review-stats'
      );
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

  // Returning to grid/list from manage: if the active sort is a manage-only order
  // (excluded/included), fall back to the persisted list sort so the table isn't
  // ordered by a now-hidden column and the sort dropdown isn't left blank.
  function setListView(mode: 'grid' | 'list') {
    if (columnForSortOrder(appliedSortOrder)?.manageOnly) {
      const restored = getStoredValue<SortOrder>(SORT_STORAGE_KEY, DEFAULT_SORT_ORDER, isSortOrder);
      appliedSortOrder = restored;
      filters.sortOrder = restored;
      // filteredSpecies is $derived and re-sorts automatically on appliedSortOrder.
    }
    viewMode = mode;
  }

  async function fetchManageData() {
    await Promise.all([
      fetchReviewStats(),
      fetchListMembers(excludedList),
      fetchListMembers(includedList),
      fetchListMembers(confirmedList),
      fetchRangeScores(),
    ]);
  }

  // Loads the current members of one manage-view list (excluded / whitelisted / confirmed)
  // into its descriptor's state. On failure the existing membership is left untouched.
  async function fetchListMembers(list: ManageListDescriptor) {
    try {
      const data = await fetchWithCSRF<{ species: string[] }>(list.listEndpoint);
      list.setMembers(new Set(data.species));
    } catch (error) {
      logger.error('Error fetching species list members:', error);
    }
  }

  // Loads range-filter geomodel probabilities for the configured location/threshold,
  // reusing the same POST /api/v2/range/species/test path the Active Species settings
  // page uses (via settingsActions.loadRangeFilterSpecies) — no new backend endpoint.
  // Species below the configured threshold aren't returned and show "—". Failures are
  // non-fatal: the Probability column simply shows "—".
  async function fetchRangeScores() {
    // The geomodel output only changes with location/threshold, so reuse the scores for
    // the rest of the session.
    if (rangeScores.size > 0) return;
    isLoadingScores = true;
    try {
      // loadRangeFilterSpecies reads coordinates + threshold from the settings store, so
      // make sure settings are loaded (the analytics page may not have fetched them yet).
      if (!get(settingsStore).dataLoaded) {
        await settingsActions.loadSettings();
      }
      const result = await settingsActions.loadRangeFilterSpecies();
      const next = new Map<string, number>();
      for (const entry of result.species) {
        if (entry.scientificName && typeof entry.score === 'number') {
          next.set(entry.scientificName, entry.score);
        }
      }
      rangeScores = next;
    } catch (error) {
      logger.error('Error fetching range filter scores:', error);
    } finally {
      isLoadingScores = false;
    }
  }

  // Toggle a species in one of the manage-view membership lists (excluded / whitelisted /
  // confirmed). Optimistically flips local state, POSTs to the list endpoint, reconciles
  // with the authoritative response, and reverts on failure. The descriptor supplies the
  // endpoint, state accessors, response flag, and toast copy.
  async function toggleSpeciesList(species: SpeciesData, list: ManageListDescriptor) {
    const name = species.common_name;
    if (list.getToggling().has(name)) return;

    // Mark in-flight (prevents double-click races).
    const inFlight = new Set(list.getToggling());
    inFlight.add(name);
    list.setToggling(inFlight);

    // Optimistic update.
    const wasMember = list.getMembers().has(name);
    const optimistic = new Set(list.getMembers());
    if (wasMember) {
      optimistic.delete(name);
    } else {
      optimistic.add(name);
    }
    list.setMembers(optimistic);

    try {
      const resp = await fetchWithCSRF<SpeciesToggleResponse>(list.endpoint, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ common_name: name }),
      });
      // Reconcile with authoritative server state ('added' or 'removed' — the server sets
      // action and membership in lockstep, so action is the single source of truth here).
      const reconciled = new Set(list.getMembers());
      if (resp.action === 'added') {
        reconciled.add(name);
      } else {
        reconciled.delete(name);
      }
      list.setMembers(reconciled);
      toastActions.success(
        resp.action === 'added'
          ? t(list.addedKey, { species: name })
          : t(list.removedKey, { species: name })
      );
    } catch (error) {
      // Revert optimistic update on failure.
      const reverted = new Set(list.getMembers());
      if (wasMember) {
        reverted.add(name);
      } else {
        reverted.delete(name);
      }
      list.setMembers(reverted);
      logger.error('Error toggling species list membership:', error);
      toastActions.error(t('analytics.species.manage.toggleError'));
    } finally {
      const done = new Set(list.getToggling());
      done.delete(name);
      list.setToggling(done);
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
        // Some detections couldn't be deleted — refresh to show accurate remaining count.
        // The summary and review-stats endpoints are independent, so fetch them together.
        await Promise.all([fetchData(), fetchReviewStats()]);
      } else {
        toastActions.success(
          t('analytics.species.manage.deleteSuccess', {
            species: target.common_name,
            deleted: resp.deleted,
          })
        );
        // All detections deleted — remove the row from local state without a round-trip.
        speciesData = speciesData.filter(s => s.scientific_name !== target.scientific_name);
        reviewStats = reviewStats.filter(s => s.scientific_name !== target.scientific_name);
      }
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

  async function loadThumbnailsAsync(fetchSeq: number) {
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
        // A newer fetchData superseded this run; stop fetching and mutating stale state.
        if (fetchSeq !== thumbnailFetchSeq) return;
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
          // Re-check after the await: a newer fetch may have replaced speciesData
          // while this batch was in flight, so don't apply stale thumbnails to it.
          if (fetchSeq !== thumbnailFetchSeq) return;

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
          // filteredSpecies is $derived; reassigning speciesData re-renders it.
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

  // Cancel a pending debounce timer when the page unmounts so it can't fire
  // (and write state) after the component is gone.
  onDestroy(() => clearTimeout(searchDebounce));

  function handleSearchInput(e: Event): void {
    const target = e.target as HTMLInputElement;
    // Keep filters.searchTerm live (input box + "filtered" badge), but debounce
    // committing it to debouncedSearchTerm, which the filteredSpecies $derived reads.
    filters.searchTerm = target.value;
    clearTimeout(searchDebounce);
    searchDebounce = setTimeout(() => {
      debouncedSearchTerm = filters.searchTerm;
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

<!-- Manage-view membership toggle cell (excluded / whitelisted / confirmed). Driven by a
     ManageListDescriptor so the three columns share one button definition. -->
{#snippet toggleCell(species: SpeciesData, active: boolean, list: ManageListDescriptor)}
  {@const tooltip = t(active ? list.activeTooltipKey : list.inactiveTooltipKey, {
    species: species.common_name,
  })}
  <td class="text-center">
    <button
      type="button"
      class="btn btn-ghost btn-xs {active ? list.activeClass : 'opacity-30'}"
      disabled={list.getToggling().has(species.common_name)}
      onclick={() => void toggleSpeciesList(species, list)}
      title={tooltip}
      aria-label={tooltip}
      aria-pressed={active}
    >
      {#if active}
        {@const ActiveIcon = list.activeIcon}
        <ActiveIcon class="h-5 w-5" />
      {:else}
        <Circle class="h-5 w-5" />
      {/if}
    </button>
  </td>
{/snippet}

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
            onclick={() => setListView('grid')}
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
            onclick={() => setListView('list')}
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
        <!-- Manage view widens the table; horizontal scroll keeps every column readable.
             Manage also bounds the height so the sticky header can pin while rows scroll. -->
        <div class={viewMode === 'manage' ? 'overflow-auto max-h-[70vh]' : 'overflow-x-auto'}>
          <table class="table w-full hidden sm:table">
            <thead>
              <tr>
                <!-- One loop drives every header. In the manage view the manageOnly
                     columns (Excluded, Whitelisted, Review Ratio, Probability, Confirmed)
                     are centered and the header row is pinned. -->
                {#each visibleColumns as { field, labelKey, manageOnly } (field)}
                  <SortableHeader
                    label={t(labelKey)}
                    {field}
                    activeField={sortField}
                    direction={sortDirection}
                    onSort={handleSort}
                    className={`${manageOnly ? 'text-center ' : ''}${viewMode === 'manage' ? STICKY_HEADER_CLASS : ''}`}
                  />
                {/each}
                {#if viewMode === 'manage'}
                  <th class={STICKY_HEADER_CLASS}>{t('analytics.species.headers.actions')}</th>
                {/if}
              </tr>
            </thead>
            <tbody>
              {#each displayedSpecies as species, index (`${species.scientific_name}_${index}`)}
                {@const ratio = reviewedRatio(species)}
                {@const displayName = localizeSpeciesName(
                  species.scientific_name,
                  species.common_name
                )}
                <tr
                  class={index % 2 === 0
                    ? 'bg-[var(--color-base-100)]'
                    : 'bg-[var(--color-base-200)]'}
                >
                  <td>
                    {#if viewMode === 'manage'}
                      <!-- Manage view is text-only (no thumbnail) to keep the wider table compact. -->
                      <div>
                        <div class="font-bold">{displayName}</div>
                        <div class="text-sm opacity-50 italic">{species.scientific_name}</div>
                      </div>
                    {:else}
                      <div class="flex items-center gap-3">
                        <div class="avatar">
                          <div
                            class="mask mask-squircle w-12 h-12"
                            class:bg-[var(--color-base-300)]={!species.thumbnail_url}
                          >
                            {#if species.thumbnail_url}
                              <img
                                src={species.thumbnail_url}
                                alt={displayName}
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
                          <div class="font-bold">{displayName}</div>
                          <div class="text-sm opacity-50 italic">{species.scientific_name}</div>
                        </div>
                      </div>
                    {/if}
                  </td>
                  <td class="font-semibold">{species.count}</td>
                  {#if viewMode !== 'manage'}
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
                  {/if}
                  <td>{formatPercentage(species.max_confidence)}</td>
                  {#if viewMode !== 'manage'}
                    <td class="text-sm whitespace-nowrap">
                      {formatColumnDate(species.first_heard)}
                    </td>
                  {/if}
                  <td class="text-sm whitespace-nowrap">{formatColumnDate(species.last_heard)}</td>
                  {#if viewMode === 'manage'}
                    {@const isExcluded = species.is_excluded ?? false}
                    {@const isIncluded = species.is_included ?? false}
                    {@const isConfirmed = species.is_confirmed ?? false}
                    <!-- Excluded / Whitelisted membership toggles -->
                    {@render toggleCell(species, isExcluded, excludedList)}
                    {@render toggleCell(species, isIncluded, includedList)}
                    <!-- Confirmed / rejected review counts as plain numbers (no bar). -->
                    <td class="tabular-nums whitespace-nowrap">
                      {#if ratio === null}
                        <span class="opacity-40" title={t('analytics.species.manage.noReviews')}
                          >—</span
                        >
                      {:else}
                        <span
                          class="text-sm"
                          title={t('analytics.species.manage.reviewCounts', {
                            verified: species.verified_count ?? 0,
                            rejected: species.rejected_count ?? 0,
                          })}
                        >
                          {species.verified_count ?? 0} / {species.rejected_count ?? 0}
                        </span>
                      {/if}
                    </td>
                    <!-- Probability (range-filter geomodel score, 3 decimals) -->
                    <td class="text-right tabular-nums whitespace-nowrap">
                      {#if species.range_score !== undefined}
                        <span title={t('analytics.species.manage.probabilityTooltip')}>
                          {species.range_score.toFixed(3)}
                        </span>
                      {:else if isLoadingScores}
                        <span
                          class="loading loading-dots loading-xs"
                          title={t('common.loading')}
                          aria-label={t('common.loading')}
                        ></span>
                      {:else}
                        <span
                          class="opacity-40"
                          title={t('analytics.species.manage.probabilityNone')}>—</span
                        >
                      {/if}
                    </td>
                    <!-- Confirmed (manually verified as a genuine occurrence) -->
                    {@render toggleCell(species, isConfirmed, confirmedList)}
                    <!-- Actions -->
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
