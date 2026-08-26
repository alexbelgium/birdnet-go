<script module lang="ts">
  /** Column the mobile species table is sorted by. */
  export type MobileSortKey = 'count' | 'name' | 'conf' | 'latest';
  export type MobileSortDir = 'asc' | 'desc';
</script>

<script lang="ts">
  import type { DailySpeciesSummary } from '$lib/types/detection.types';
  import { buildAppUrl } from '$lib/utils/urlHelpers';
  import { localizeSpeciesName } from '$lib/utils/speciesDisplay';
  import {
    computeConfidenceColor,
    computePeakHour,
    formatDetectionCount,
  } from '../../utils/dailySummaryStats';
  import { getSpeciesBadgeColor, getSpeciesInitials } from '../../utils/speciesBadge';
  import { resolveNoveltyCategory, noveltyCategoryColorVar } from '../../utils/noveltyCategory';
  import { computeAxisTicks, tickPositionCss } from '../../utils/hourAxis';
  import { Star, CalendarDays, Leaf, ChevronUp, ChevronDown } from '@lucide/svelte';
  import HourlyMiniChart from './HourlyMiniChart.svelte';
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
    /** Current sort column + direction (owned/persisted by DailySummaryCard). */
    sortKey?: MobileSortKey;
    sortDir?: MobileSortDir;
    /** Invoked when a column header is tapped; parent flips direction / switches key. */
    onSortChange?: (_key: MobileSortKey) => void;
    /** Per-hour weather accessor for the expanded detail card's peak-hour line. */
    getHourWeather?: (_hour: number) => { emoji: string; tempText: string } | undefined;
  }

  let {
    data,
    sunriseHour,
    sunsetHour,
    getSpeciesUrl,
    showThumbnails,
    selectedDate,
    maxHour,
    sortKey = 'count',
    sortDir = 'desc',
    onSortChange,
    getHourWeather,
  }: Props = $props();

  // Largest count of the day — the scale for the per-row abundance bar. Rows are
  // normalised against the day's busiest species, so the bars stay comparable
  // regardless of which column the table is sorted by.
  const maxCount = $derived(data.reduce((m, d) => Math.max(m, d.count), 0));

  /** "peak at 07:00" for a row, or '' when the day has no detections to peak at.
      The mini chart is aria-hidden, so this is the only way its headline fact —
      when the species was most active — reaches a screen reader. */
  function peakHourLabel(item: DailySpeciesSummary): string {
    const hour = computePeakHour(item.hourly_counts, maxHour);
    return hour === null ? '' : `, peak at ${String(hour).padStart(2, '0')}:00`;
  }

  /** Abundance-bar width for a row, as a CSS percentage of the name column. */
  function abundanceWidth(count: number): string {
    if (maxCount <= 0) return '0%';
    return `${Math.max(0, Math.min(100, (count / maxCount) * 100)).toFixed(1)}%`;
  }

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
  // Collapsed whenever the day changes: a card left open across a day swipe
  // would silently re-point at a different day's numbers.
  let expandedSpecies: string | null = $state(null);
  $effect(() => {
    void selectedDate;
    expandedSpecies = null;
  });

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
      Conf.{@render sortGlyph('conf')}
    </button>
    <button
      type="button"
      class="col-header-count col-header-btn"
      class:is-active={sortKey === 'count'}
      onclick={() => onSortChange?.('count')}
      aria-label={sortLabel('count', 'detection count')}
    >
      Cnt{@render sortGlyph('count')}
    </button>
    <!-- Chart column: the hour-scale axis IS the header; active-sort state tints
         the ticks (a chevron would overlap the right-edge tick). The underline
         drawn on .is-active is what actually marks the sorted column. -->
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

  {#each data as item (item.scientific_name)}
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
        {getHourWeather}
      />
    {:else}
      <!-- Compact row — tap → expand into detail card. A real <button>, so Enter/
           Space, touch, and assistive tech come from the platform rather than a
           hand-rolled keydown handler. -->
      <button
        type="button"
        class="mobile-summary-row"
        class:row-updated={((item.hourlyUpdated?.length ?? 0) > 0 ||
          item.countIncreased === true) &&
          !prefersReducedMotion}
        data-species-row={item.scientific_name}
        aria-label="{displayName}: {pct}% confidence, {formatDetectionCount(
          item.count
        )} detections{peakHourLabel(item)}. Tap to expand."
        onclick={() => {
          expandedSpecies = item.scientific_name;
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
                <CalendarDays class="size-3" />
              </span>
            {:else if novelty === 'season'}
              <span
                class="col-novelty-badge"
                style:color={noveltyCategoryColorVar('season')}
                title={`First time this ${item.current_season || 'season'} (${item.days_this_season ?? 0} day${(item.days_this_season ?? 0) === 1 ? '' : 's'} ago)`}
              >
                <Leaf class="size-3" />
              </span>
            {/if}
          </div>
          <span class="col-scientific-name">{item.scientific_name}</span>
          <!-- Abundance bar: this row's share of the day's busiest species. Keeps
               relative magnitude readable when a name/confidence sort scrambles
               the count order. Hidden in landscape, where the scientific name
               occupies this line. -->
          <span class="col-abundance" aria-hidden="true">
            <span class="col-abundance-fill" style:width={abundanceWidth(item.count)}></span>
          </span>
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
      </button>
    {/if}
  {/each}

  {#if data.length === 0}
    <div class="py-8 text-center text-sm text-[var(--color-base-content)]/60">
      No species detected
    </div>
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
    --col-conf-w: 2rem;
    --col-count-w: 1.75rem;

    /* Row height. Fixed rather than content-derived because it is also the
       intrinsic size the off-screen rows are measured at — see
       .mobile-summary-row. ≥ 28px also keeps every row clear of the WCAG 2.5.8
       target-size floor. */
    --row-h: 1.75rem;

    /* Name:chart share of the flexible width. The chart used to be a fixed px track
       sized from (maxHour + 1) × BAR_STRIDE, so every column reflowed once an hour
       on "today" and the chart was squeezed to ~44 px right through the morning.
       Splitting the free space by ratio instead keeps the layout stable all day and
       lets the SVG stretch into whatever it gets. Portrait weights the species name
       at 2:1 — there is only ~230 px to share, and a readable name beats a wider
       sparkline; landscape relaxes to the golden ratio, where both fit. */
    --col-name-fr: 2fr;

    /* Floor on the chart so it never collapses to an unreadable sliver on a very
       narrow phone; the name ellipsis-truncates instead. */
    --col-chart-fr: minmax(3.25rem, 1fr);
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
    grid-template-columns:
      var(--thumb-w) var(--col-name-fr) var(--col-conf-w) var(--col-count-w)
      var(--col-chart-fr);
    align-items: center;
    gap: 0.25rem;
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

  /* Active-sort column reads darker than the resting headers, and carries an
     underline. The chevron alone was ambiguous: the header cells sit a couple of
     pixels apart, so a glyph at a column edge reads as belonging to its neighbour.
     The underline spans the column, so the sorted column is never in doubt. */
  .col-header-btn.is-active {
    color: var(--color-base-content);
    box-shadow: inset 0 -2px 0 var(--color-primary);
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

  .mobile-summary-row {
    display: grid;
    grid-template-columns:
      var(--thumb-w) var(--col-name-fr) var(--col-conf-w) var(--col-count-w)
      var(--col-chart-fr);
    align-items: center;
    gap: 0.25rem;

    /* No vertical padding on purpose — see contain-intrinsic-size below. */
    padding: 0 0.125rem;
    min-height: var(--row-h);
    border-radius: 0.375rem;
    box-shadow: inset 0 -1px 0 color-mix(in srgb, var(--color-base-content) 7%, transparent);
    font: inherit;

    /* The full species list is rendered — no "show all" cap — so skip layout and
       paint for the rows that are off screen.
       contain-intrinsic-size sizes the CONTENT box, so it matches a skipped row's
       real height only while the row has no vertical padding and its content fits
       inside --row-h. Get that wrong and every skipped row is mis-measured,
       shifting .mobile-summary-table's offsetHeight — which is exactly what the
       header-pin $effect clamps against, so the pinned header stops in the wrong
       place. Both values read --row-h so they cannot drift apart. */
    content-visibility: auto;
    contain-intrinsic-size: auto var(--row-h);
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
     removes the class again. Best-effort by design: a row several screens down
     can run its whole pulse before the user reaches it. That is a property of
     the row being off screen, not of the content-visibility above — the
     animation is timeline-driven either way, so a row scrolled into view
     mid-pulse paints from wherever the timeline has got to. */
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

  /* Inset offset on purpose: content-visibility above implies paint containment,
     which clips an outline drawn outside the row's box. */
  .mobile-summary-row:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: -2px;
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

  .col-abundance {
    display: block;
    width: 100%;
    height: 2px;
    border-radius: 1px;
    background: color-mix(in srgb, var(--color-base-content) 8%, transparent);
    overflow: hidden;
  }

  .col-abundance-fill {
    display: block;
    height: 100%;
    min-width: 2px;
    border-radius: 1px;
    background: color-mix(in srgb, var(--color-primary) 40%, transparent);
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
    justify-self: stretch;
    border-radius: 0.25rem;
    padding: 0.125rem 0;
  }

  /* The SVG carries preserveAspectRatio="none", so stretching it to the track's
     width just widens the bars — the hour positions (and therefore the header's
     tick alignment, which is computed in percentages) are unchanged. */
  .col-chart :global(svg) {
    width: 100%;
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

      /* Landscape stacks the scientific name under the common name, so the row
         needs a second line of height — and the intrinsic size has to grow with
         it, or every off-screen row is measured a line short. */
      --row-h: 2.25rem;

      /* Landscape has room for both a full name and a wide chart, so relax the
         portrait 2:1 weighting back to the golden ratio. */
      --col-name-fr: 1.618fr;
    }

    /* The scientific name takes back the line the abundance bar uses in portrait. */
    .col-abundance {
      display: none;
    }

    .col-scientific-name {
      display: block;
    }
  }
</style>
