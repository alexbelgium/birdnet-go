<script module lang="ts">
  /** Column the mobile species table is sorted by. */
  export type MobileSortKey = 'count' | 'name' | 'conf' | 'latest';
  export type MobileSortDir = 'asc' | 'desc';
</script>

<script lang="ts">
  import type { DailySpeciesSummary } from '$lib/types/detection.types';
  import { buildAppUrl } from '$lib/utils/urlHelpers';
  import { localizeSpeciesName } from '$lib/utils/speciesDisplay';
  import { computeConfidenceColor, formatDetectionCount } from '../../utils/dailySummaryStats';
  import { getSpeciesBadgeColor, getSpeciesInitials } from '../../utils/speciesBadge';
  import { resolveNoveltyCategory, noveltyCategoryColorVar } from '../../utils/noveltyCategory';
  import { computeAxisTicks, tickPositionCss } from '../../utils/hourAxis';
  import { Star, ChevronUp, ChevronDown } from '@lucide/svelte';
  import HourlyMiniChart, { BAR_STRIDE } from './HourlyMiniChart.svelte';
  import SpeciesDetailCard from './SpeciesDetailCard.svelte';

  interface Props {
    data: DailySpeciesSummary[];
    sunriseHour: number | null;
    sunsetHour: number | null;
    getSpeciesUrl: (_item: DailySpeciesSummary) => string;
    showThumbnails: boolean;
    selectedDate: string;
    /** Last hour (inclusive) to render in charts — today's current hour, else 23.
        Owned by DailySummaryCard, which keeps it ticking across hour changes. */
    maxHour: number;
    /** Rows shown before the "Show all" expander; 0 disables the cap. */
    limit?: number;
    /** Current sort column + direction (owned/persisted by DailySummaryCard). */
    sortKey?: MobileSortKey;
    sortDir?: MobileSortDir;
    /** Invoked when a column header is tapped; parent flips direction / switches key. */
    onSortChange?: (_key: MobileSortKey) => void;
  }

  let {
    data,
    sunriseHour,
    sunsetHour,
    getSpeciesUrl,
    showThumbnails,
    selectedDate,
    maxHour,
    limit = 0,
    sortKey = 'count',
    sortDir = 'desc',
    onSortChange,
  }: Props = $props();

  // "Show all" expander: cap the list to `limit` rows on busy days (each row
  // carries an SVG chart) so the page doesn't scroll forever to reach the content
  // below. No data is hidden silently — the button states the full count.
  let showAll = $state(false);
  // Collapse back to the capped view whenever the day changes.
  $effect(() => {
    void selectedDate;
    showAll = false;
  });
  const isTruncated = $derived(limit > 0 && data.length > limit);
  const visibleRows = $derived(showAll || !isTruncated ? data : data.slice(0, limit));

  // Hour-scale ticks drawn in the pinned header so every row's chart is readable
  // at a glance (the per-row charts carry no axis of their own).
  const headerTicks = $derived(computeAxisTicks(maxHour));

  // Full accessible label for a sort header. aria-sort itself is only valid on
  // grid/columnheader roles (this is a CSS grid, not an ARIA grid), so the current
  // sort state is folded into the button's label instead.
  function sortLabel(key: MobileSortKey, what: string): string {
    if (sortKey !== key) return `Sort by ${what}`;
    return `Sorted by ${what}, ${sortDir === 'asc' ? 'ascending' : 'descending'}. Tap to reverse.`;
  }

  // Per-row expansion state — scientific name of the expanded row, or null.
  let expandedSpecies: string | null = $state(null);

  // Root element, used to restore focus to a row after its card collapses.
  let tableEl = $state<HTMLDivElement>();

  function collapseRow(scientificName: string) {
    expandedSpecies = null;
    // Restore focus to the compact row that replaces the card (next frame,
    // once the row exists again) so keyboard/SR users keep their place.
    window.requestAnimationFrame(() => {
      const escaped =
        typeof window.CSS?.escape === 'function'
          ? window.CSS.escape(scientificName)
          : scientificName;
      tableEl?.querySelector<HTMLElement>(`[data-species-row="${escaped}"]`)?.focus();
    });
  }

  // Chart column tracks the chart's actual rendered width (BAR_STRIDE px/bar,
  // from HourlyMiniChart) so leftover space goes to the species name instead of
  // sitting empty next to a shorter-than-24h chart.
  const chartColWidthPx = $derived((maxHour + 1) * BAR_STRIDE);

  // Gates the SSE row-flash animation for users who prefer reduced motion.
  const prefersReducedMotion =
    typeof window !== 'undefined'
      ? (window.matchMedia?.('(prefers-reduced-motion: reduce)')?.matches ?? false)
      : false;

  // ── Column-header pinning ─────────────────────────────────────────────────
  // Keeps the header visible while any part of the species list is on screen,
  // with the page (not this component) as the scroll surface. CSS
  // `position: sticky` is not an option: the app shell's .drawer-content has
  // overflow-y: auto, which makes it the header's scrollport, but it auto-grows
  // with its content and never scrolls itself (the window does) — so a sticky
  // header would never stick. Instead, translate the header down by however far
  // the table's top has scrolled past the viewport top, clamped so the header
  // leaves with the end of the list. The capture-phase listener catches the
  // scroll wherever it happens (window today, any scrolling ancestor tomorrow).
  let headerEl = $state<HTMLDivElement>();

  $effect(() => {
    const table = tableEl;
    const header = headerEl;
    if (!table || !header) return;

    let raf = 0;
    const update = () => {
      raf = 0;
      const tableTop = table.getBoundingClientRect().top;
      const maxShift = Math.max(0, table.offsetHeight - header.offsetHeight);
      const shift = Math.min(Math.max(0, -tableTop), maxShift);
      header.style.setProperty('--pin-shift', `${shift}px`);
      header.style.transform = shift > 0 ? `translate3d(0, ${shift}px, 0)` : '';
    };
    const schedule = () => {
      if (raf === 0) raf = window.requestAnimationFrame(update);
    };

    window.addEventListener('scroll', schedule, { capture: true, passive: true });
    window.addEventListener('resize', schedule, { passive: true });
    update();

    return () => {
      window.removeEventListener('scroll', schedule, { capture: true });
      window.removeEventListener('resize', schedule);
      if (raf !== 0) window.cancelAnimationFrame(raf);
    };
  });
</script>

<!--
  Compact mobile species table — replaces the heatmap grid on screens <768 px.
  Interaction model: tap anywhere on a row → expand into the species detail
  card; the card's pills link out (eBird, detections, history).
-->
<div
  bind:this={tableEl}
  class="mobile-summary-table w-full"
  aria-label="Species detected on {selectedDate}"
  style:--col-chart-w="{chartColWidthPx}px"
>
  <!-- Active-sort indicator for a header button -->
  {#snippet sortGlyph(key: MobileSortKey)}
    {#if sortKey === key}
      {#if sortDir === 'asc'}
        <ChevronUp class="size-2.5" aria-hidden="true" />
      {:else}
        <ChevronDown class="size-2.5" aria-hidden="true" />
      {/if}
    {/if}
  {/snippet}

  <!-- Header (JS-pinned while the list is in view — see pin $effect). Each column
       is a tap-to-sort button; the chart column doubles as the hour-scale axis. -->
  <div bind:this={headerEl} class="mobile-summary-header">
    <button
      type="button"
      class="col-header-species col-header-btn"
      class:is-active={sortKey === 'name'}
      onclick={() => onSortChange?.('name')}
      aria-label={sortLabel('name', 'species name')}
    >
      Species{@render sortGlyph('name')}
    </button>
    <button
      type="button"
      class="col-header-conf col-header-btn"
      class:is-active={sortKey === 'conf'}
      onclick={() => onSortChange?.('conf')}
      aria-label={sortLabel('conf', 'confidence')}
    >
      {@render sortGlyph('conf')}Conf.
    </button>
    <button
      type="button"
      class="col-header-count col-header-btn"
      class:is-active={sortKey === 'count'}
      onclick={() => onSortChange?.('count')}
      aria-label={sortLabel('count', 'detection count')}
    >
      {@render sortGlyph('count')}Cnt
    </button>
    <!-- Chart column: the hour-scale axis IS the header; active-sort state tints
         the ticks (a chevron would overlap the right-edge tick in this narrow column). -->
    <button
      type="button"
      class="col-header-chart col-header-btn"
      class:is-active={sortKey === 'latest'}
      onclick={() => onSortChange?.('latest')}
      aria-label={sortLabel('latest', 'most recent detection')}
    >
      <span class="col-header-axis" aria-hidden="true">
        {#each headerTicks as tick, i (tick)}
          <span
            class="col-header-tick"
            class:tick-first={i === 0}
            class:tick-last={i === headerTicks.length - 1}
            style:left={i === headerTicks.length - 1 ? undefined : tickPositionCss(tick, maxHour)}
            style:right={i === headerTicks.length - 1 ? '0' : undefined}
          >
            {tick}
          </span>
        {/each}
      </span>
    </button>
  </div>

  {#each visibleRows as item (item.scientific_name)}
    {@const displayName = localizeSpeciesName(item.scientific_name, item.common_name)}
    {@const pct = Math.round(Math.max(0, Math.min(1, item.max_confidence ?? 0)) * 100)}
    {@const isExpanded = expandedSpecies === item.scientific_name}
    {@const novelty = resolveNoveltyCategory(item)}

    {#if isExpanded}
      <!-- Expanded: full species card replaces the compact row -->
      <SpeciesDetailCard
        {item}
        {sunriseHour}
        {sunsetHour}
        {displayName}
        speciesUrl={getSpeciesUrl(item)}
        {maxHour}
        onCollapse={() => collapseRow(item.scientific_name)}
        {selectedDate}
      />
    {:else}
      <!-- Compact row — tap → expand into detail card -->
      <div
        class="mobile-summary-row"
        class:row-updated={((item.hourlyUpdated?.length ?? 0) > 0 ||
          item.countIncreased === true) &&
          !prefersReducedMotion}
        role="button"
        tabindex="0"
        data-species-row={item.scientific_name}
        aria-label="{displayName}: {pct}% confidence, {formatDetectionCount(
          item.count
        )} detections. Tap to expand."
        onclick={() => {
          expandedSpecies = item.scientific_name;
        }}
        onkeydown={(e: KeyboardEvent) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            expandedSpecies = item.scientific_name;
          }
        }}
      >
        <!-- Thumbnail or initials badge — portrait hides at default, shown in landscape -->
        <div class="mobile-thumb-col">
          {#if showThumbnails}
            <img
              src={item.thumbnail_url
                ? buildAppUrl(item.thumbnail_url)
                : buildAppUrl(
                    `/api/v2/media/species-image?name=${encodeURIComponent(item.scientific_name)}`
                  )}
              alt=""
              class="mobile-thumb"
              loading="lazy"
            />
          {:else}
            <span
              class="mobile-badge"
              style:background-color={getSpeciesBadgeColor(item.scientific_name)}
              aria-hidden="true"
            >
              {getSpeciesInitials(displayName)}
            </span>
          {/if}
        </div>

        <!-- Species name + scientific name -->
        <div class="col-name-group">
          <div class="col-name-row">
            <span class="col-name text-sm font-medium truncate leading-tight">
              {displayName}
            </span>
            {#if novelty === 'lifetime'}
              <span
                class="col-novelty-badge"
                style:color={noveltyCategoryColorVar('lifetime')}
                title={`New species (first seen ${item.days_since_first_seen ?? 0} day${(item.days_since_first_seen ?? 0) === 1 ? '' : 's'} ago)`}
              >
                <Star class="size-3 fill-current" />
              </span>
            {:else if novelty === 'year'}
              <span
                class="col-novelty-badge"
                style:color={noveltyCategoryColorVar('year')}
                title={`First time this year (${item.days_this_year ?? 0} day${(item.days_this_year ?? 0) === 1 ? '' : 's'} ago)`}
              >
                📅
              </span>
            {:else if novelty === 'season'}
              <span
                class="col-novelty-badge"
                style:color={noveltyCategoryColorVar('season')}
                title={`First time this ${item.current_season || 'season'} (${item.days_this_season ?? 0} day${(item.days_this_season ?? 0) === 1 ? '' : 's'} ago)`}
              >
                🌿
              </span>
            {/if}
          </div>
          <span class="col-scientific-name">{item.scientific_name}</span>
        </div>

        <!-- Max confidence, color-coded -->
        <span
          class="col-conf text-xs tabular-nums font-semibold"
          style:color={computeConfidenceColor(pct)}
        >
          {pct}%
        </span>

        <!-- Detection count, abbreviated -->
        <span class="col-count text-xs tabular-nums">
          {formatDetectionCount(item.count)}
        </span>

        <!-- Hourly chart (visual only — whole row handles tap) -->
        <div class="col-chart" aria-hidden="true">
          <HourlyMiniChart {item} {sunriseHour} {sunsetHour} {maxHour} />
        </div>
      </div>
    {/if}
  {/each}

  {#if data.length === 0}
    <div class="py-8 text-center text-sm text-[var(--color-base-content)]/60">
      No species detected
    </div>
  {/if}

  {#if isTruncated}
    <!-- Visible expander (not a hint): the list is capped, so state the full count -->
    <button type="button" class="show-all-btn" onclick={() => (showAll = !showAll)}>
      {showAll ? 'Show fewer' : `Show all ${data.length} species`}
    </button>
  {/if}

  {#if data.length > 0}
    <!-- Gesture hints only; both actions have accessible equivalents (row buttons, DatePicker) -->
    <div class="zoom-hint" aria-hidden="true">tap a row for details · swipe ⟷ to change day</div>
  {/if}
</div>

<style>
  .mobile-summary-table {
    --thumb-w: 1.5rem;
    --thumb-h: 1.25rem;
    --col-conf-w: 2.5rem;
    --col-count-w: 2.25rem;

    /* --col-chart-w is bound inline to the chart's actual rendered width (px), so the
       column never reserves more space than the chart needs — the name column gets
       whatever is left over. */
  }

  /* Curtain above the column names while the header is pinned */
  .mobile-summary-header::before {
    content: '';
    position: absolute;
    left: -0.25rem;
    right: -0.25rem;
    bottom: 100%;
    height: var(--pin-shift, 0px);
    background: color-mix(in srgb, var(--color-base-100) 75%, black 25%);
    backdrop-filter: blur(6px);
    -webkit-backdrop-filter: blur(6px);
    pointer-events: none;
    z-index: -1;
  }

  /* ─── 5-column grid; header and rows share the same tracks ─── */

  .mobile-summary-header {
    display: grid;
    grid-template-columns: var(--thumb-w) 1fr var(--col-conf-w) var(--col-count-w) var(
        --col-chart-w
      );
    align-items: center;
    gap: 0.125rem;
    padding: 0 0.125rem 0.375rem;
    margin-bottom: 0.125rem;
    border-bottom: 1px solid var(--color-base-200);
    font-size: 0.625rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: color-mix(in srgb, var(--color-base-content) 50%, transparent);

    /* Pinned via translateY while the page scrolls (see the pin $effect in the
       script block). position: sticky cannot work here: the app shell's
       .drawer-content ancestor has overflow-y: auto, making it this header's
       scrollport — but it auto-grows with its content and never scrolls (the
       window does), so a sticky header would simply never stick. */
    will-change: transform;
    position: relative;
    z-index: 50;
    background: color-mix(in srgb, var(--color-base-100) 92%, transparent);
    backdrop-filter: blur(6px);
    -webkit-backdrop-filter: blur(6px);
    isolation: isolate;
  }

  /* Header cells are tap-to-sort buttons: strip the native chrome, inherit the
     header's tiny uppercase type, keep them from becoming zoom surfaces. */
  .col-header-btn {
    appearance: none;
    background: none;
    border: none;
    margin: 0;
    padding: 0.25rem 0;
    font: inherit;
    letter-spacing: inherit;
    text-transform: inherit;
    color: inherit;
    cursor: pointer;
    display: flex;
    align-items: center;
    gap: 0.0625rem;
    min-width: 0;
    touch-action: manipulation;
  }

  .col-header-btn:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 1px;
    border-radius: 0.25rem;
  }

  /* Active-sort column reads darker than the resting headers. */
  .col-header-btn.is-active {
    color: var(--color-base-content);
  }

  .col-header-species {
    grid-column: 1 / 3;
    justify-content: flex-start;
  }

  .col-header-conf,
  .col-header-count {
    justify-content: flex-end;
    text-align: right;
  }

  /* Chart column doubles as the hour axis; give the ticks a positioning context. */
  .col-header-chart {
    position: relative;
    justify-content: flex-end;
    min-height: 0.75rem;
  }

  .col-header-axis {
    position: relative;
    display: block;
    width: 100%;
    height: 0.6rem;
  }

  .col-header-tick {
    position: absolute;
    top: 0;
    transform: translateX(-50%);
    font-size: 0.5rem;
    line-height: 1;
    font-variant-numeric: tabular-nums;
    color: color-mix(in srgb, var(--color-base-content) 42%, transparent);
  }

  .col-header-tick.tick-first {
    left: 0 !important;
    transform: none;
  }

  .col-header-tick.tick-last {
    left: auto !important;
    transform: none;
  }

  /* When 'latest' is the active sort, brighten the axis to mark the column. */
  .col-header-chart.is-active .col-header-tick {
    color: var(--color-base-content);
  }

  /* Full-width expander shown when the list is capped */
  .show-all-btn {
    appearance: none;
    display: block;
    width: 100%;
    margin-top: 0.25rem;
    padding: 0.5rem;
    background: none;
    border: none;
    border-top: 1px solid var(--color-base-200);
    font-size: 0.75rem;
    font-weight: 600;
    color: var(--color-primary);
    cursor: pointer;
    touch-action: manipulation;
  }

  .show-all-btn:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: -2px;
    border-radius: 0.375rem;
  }

  .mobile-summary-row {
    display: grid;
    grid-template-columns: var(--thumb-w) 1fr var(--col-conf-w) var(--col-count-w) var(
        --col-chart-w
      );
    align-items: center;
    gap: 0.125rem;
    padding: 0 0.125rem;
    min-height: 1.25rem;
    border-radius: 0.375rem;
    color: var(--color-base-content);
    cursor: pointer;
    border: none;
    background: none;
    text-align: left;
    width: 100%;

    /* Rows are tap targets, never zoom/pan surfaces — avoids double-tap zoom */
    touch-action: manipulation;
  }

  /* Brief pulse when SSE delivers a new detection for this species.
     DashboardPage clears hourlyUpdated/countIncreased after ~2.2 s, which
     removes the class again. */
  .mobile-summary-row.row-updated {
    animation: row-flash 2s ease-out;
  }

  @keyframes row-flash {
    0% {
      background-color: color-mix(in srgb, var(--color-primary) 15%, transparent);
    }

    100% {
      background-color: transparent;
    }
  }

  .mobile-summary-row:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 2px;
  }

  .mobile-thumb-col {
    display: flex;
    align-items: center;
  }

  .mobile-thumb {
    width: var(--thumb-w);
    height: var(--thumb-h);
    border-radius: 0.25rem;
    object-fit: cover;
    display: block;
    flex-shrink: 0;
    margin-right: 0.5rem;
  }

  .mobile-badge {
    display: flex;
    align-items: center;
    justify-content: center;
    width: var(--thumb-w);
    height: var(--thumb-h);
    border-radius: 0.25rem;
    font-size: 0.5rem;
    font-weight: 700;
    color: white;
    text-shadow: 0 1px 2px rgb(0 0 0 / 0.3);
    flex-shrink: 0;
  }

  .col-name-group {
    display: flex;
    flex-direction: column;
    justify-content: center;
    min-width: 0;
    gap: 0.125rem;
  }

  .col-name-row {
    display: flex;
    align-items: center;
    gap: 0.25rem;
    min-width: 0;
  }

  .col-name {
    display: block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;

    /* Hug the name so the novelty badge sits right after it (not pushed to the
       column's right edge); still shrinks + ellipsis-truncates when too long. */
    flex: 0 1 auto;
    color: var(--color-base-content);
  }

  .col-novelty-badge {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    font-size: 0.625rem;
    line-height: 1;
  }

  .col-scientific-name {
    display: none;
    font-size: 0.625rem;
    font-style: italic;
    color: color-mix(in srgb, var(--color-base-content) 55%, transparent);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .col-conf,
  .col-count,
  .col-chart {
    text-align: right;
    justify-self: end;
  }

  .col-chart {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    border-radius: 0.25rem;
    padding: 0.125rem 0;
  }

  /* Subtle hint below the species list */
  .zoom-hint {
    text-align: center;
    font-size: 0.625rem;
    letter-spacing: 0.05em;
    text-transform: uppercase;
    color: color-mix(in srgb, var(--color-base-content) 25%, transparent);
    padding: 0.625rem 0 0.125rem;
  }

  /* ─── Landscape: larger thumbnail + scientific name, name capped at φ ─── */
  @media (orientation: landscape) {
    .mobile-summary-table {
      --thumb-w: 2rem;
      --thumb-h: 1.5rem;
    }

    /* Name capped at the golden ratio; graph fills the remaining flex space.
       name:chart = 1.618:1 → name ≈ 61.8% of the flexible width (conf/count are
       fixed tracks, so name is strictly < 61.8% of the *total* width → "at most"). */
    .mobile-summary-header,
    .mobile-summary-row {
      grid-template-columns: var(--thumb-w) 1.618fr var(--col-conf-w) var(--col-count-w) 1fr;
    }

    /* The chart track is now flexible; let the cell fill it and the SVG stretch.
       `.col-chart` otherwise has justify-self:end (shrinks to content). */
    .col-chart {
      justify-self: stretch;
    }

    .col-chart :global(svg) {
      width: 100%;
    }

    .col-scientific-name {
      display: block;
    }
  }
</style>
