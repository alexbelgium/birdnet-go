<script lang="ts">
  import type { DailySpeciesSummary } from '$lib/types/detection.types';
  import { buildAppUrl } from '$lib/utils/urlHelpers';
  import { computeConfidenceColor, formatDetectionCount } from '../../utils/dailySummaryStats';
  import {
    X,
    ExternalLink,
    TrendingUp,
    TrendingDown,
    Sunrise,
    Sunset,
    BarChart2,
  } from '@lucide/svelte';
  import HourlyMiniChart from './HourlyMiniChart.svelte';
  import SpeciesHistoryModal from './SpeciesHistoryModal.svelte';
  import { buildEbirdUrl, isValidEbirdCode } from '../../utils/dailySummaryStats';
  import { getLocale } from '$lib/i18n';
  import { safeArrayAccess } from '$lib/utils/security';
  import { buildSpeciesDetectionUrl } from '$lib/utils/detectionUrls';

  interface Props {
    item: DailySpeciesSummary;
    sunriseHour: number | null;
    sunsetHour: number | null;
    displayName: string;
    speciesUrl: string;
    maxHour: number;
    onCollapse: () => void;
    selectedDate: string;
    /** Chart height in px; the desktop host passes a taller chart. */
    chartHeight?: number;
  }

  let {
    item,
    sunriseHour,
    sunsetHour,
    displayName,
    speciesUrl,
    maxHour,
    onCollapse,
    selectedDate,
    chartHeight = 64,
  }: Props = $props();

  const pct = $derived(Math.round(Math.max(0, Math.min(1, item.max_confidence ?? 0)) * 100));

  function formatLastSeen(days: number | undefined): string {
    if (days === undefined || days === null) return '';
    // "seen today" is superseded by the precise latest-detection time below.
    if (days === 0) return '';
    return `last seen ${days}d`;
  }

  const lastSeen = $derived(formatLastSeen(item.days_since_last_seen));

  // Time of the day's most recent detection, "HH:MM" (latest_heard is "HH:MM:SS").
  const latestTime = $derived(item.latest_heard ? item.latest_heard.slice(0, 5) : '');
  const detectionsUrl = $derived(buildSpeciesDetectionUrl(item.scientific_name, selectedDate));

  // Reduced-motion preference: use instant instead of smooth scrolling.
  const prefersReducedMotion =
    typeof window !== 'undefined'
      ? (window.matchMedia?.('(prefers-reduced-motion: reduce)')?.matches ?? false)
      : false;

  // Scroll the card fully into view when it expands near the bottom of the
  // screen; block:'nearest' is a no-op when it is already fully visible.
  // Also move focus onto the card so keyboard/screen-reader users land in the
  // detail they just opened (the host restores focus to the row on collapse).
  let cardEl = $state<HTMLDivElement>();

  $effect(() => {
    const el = cardEl;
    if (!el) return;
    const raf = window.requestAnimationFrame(() => {
      el.scrollIntoView({ block: 'nearest', behavior: prefersReducedMotion ? 'auto' : 'smooth' });
      el.focus({ preventScroll: true });
    });
    return () => window.cancelAnimationFrame(raf);
  });

  // History modal (multi-day detection trend) open state.
  let showHistory = $state(false);

  // Peak hour: hour with the highest detection count in 0..maxHour.
  const peakHour = $derived.by(() => {
    const counts = item.hourly_counts.slice(0, maxHour + 1);
    const maxVal = Math.max(...counts);
    if (maxVal === 0) return null;
    const idx = counts.indexOf(maxVal);
    return idx >= 0 ? idx : null;
  });

  // Novelty pill: strongest badge wins, returns {label, bg} or null.
  const novelty = $derived(
    item.is_new_species
      ? { label: 'New species', bg: '#8b5cf6' }
      : item.is_new_this_year
        ? { label: '1st this year', bg: '#f59e0b' }
        : item.is_new_this_season
          ? { label: `1st this ${item.current_season ?? 'season'}`, bg: '#06b6d4' }
          : null
  );

  // Adaptive axis ticks: fixed candidates filtered by maxHour, last hour always included.
  const axisTicks = $derived.by(() => {
    const base = [0, 6, 12, 18].filter(h => h <= maxHour);
    const last = base[base.length - 1];
    return last !== maxHour ? [...base, maxHour] : base;
  });

  // Tick position matches the SVG bar-centre: (h×4 + 1.5) / ((maxHour+1)×4) = (h+0.375)/(maxHour+1).
  function tickPct(hour: number): string {
    return `${(((hour + 0.375) / (maxHour + 1)) * 100).toFixed(1)}%`;
  }

  const thumbSrc = $derived(
    item.thumbnail_url
      ? buildAppUrl(item.thumbnail_url)
      : buildAppUrl(`/api/v2/media/species-image?name=${encodeURIComponent(item.scientific_name)}`)
  );

  // Peak bars: local maxima (count >= both neighbors) in 0..maxHour, top 4 by
  // count (more overlap at 9 px labels, so fewer than the old 6).
  const MAX_PEAK_LABELS = 4;
  const PEAK_LABEL_OFFSET_PX = 11; // label font (9px) + 2px gap above the bar

  const peakBars = $derived.by(() => {
    const maxBarH = chartHeight - 2; // headroom reserve matches HourlyMiniChart
    const counts = item.hourly_counts.slice(0, maxHour + 1);
    const maxC = Math.max(...counts, 1);
    const peaks: Array<{ hour: number; count: number; labelTopPx: number }> = [];
    for (let h = 0; h <= maxHour; h++) {
      const c = safeArrayAccess(counts, h, 0) ?? 0;
      if (c === 0) continue;
      const prev = safeArrayAccess(counts, h - 1, 0) ?? 0;
      const next = safeArrayAccess(counts, h + 1, 0) ?? 0;
      if (c >= prev && c >= next) {
        const barH = Math.max(2, Math.round((c / maxC) * maxBarH));
        const barTopPx = chartHeight - barH;
        peaks.push({ hour: h, count: c, labelTopPx: Math.max(0, barTopPx - PEAK_LABEL_OFFSET_PX) });
      }
    }
    return peaks.sort((a, b) => b.count - a.count).slice(0, MAX_PEAK_LABELS);
  });
</script>

<!--
  Expanded species detail card — replaces the compact row (mobile) or renders
  under the heatmap row (desktop) when a species is selected.
  Close button (×) or tapping the chart section collapses back.
  Tapping the species name navigates to species detections.
-->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  bind:this={cardEl}
  class="species-card"
  tabindex="-1"
  aria-label="{displayName} species detail"
  style:--detail-chart-h="{chartHeight}px"
  onclick={(e: MouseEvent) => e.stopPropagation()}
  onkeydown={(e: KeyboardEvent) => e.stopPropagation()}
>
  <!-- ╔═══════════════════ HEADER ═══════════════════╗ -->
  <div class="card-header">
    <!-- Thumbnail: 4rem × 3rem -->
    <div class="card-thumb">
      <img src={thumbSrc} alt="" class="card-thumb-img" loading="lazy" />
    </div>

    <!-- Info column -->
    <div class="card-info">
      <!-- Name + action pills -->
      <div class="card-name-row">
        <a href={speciesUrl} class="card-name">{displayName}</a>
        {#if isValidEbirdCode(item.species_code)}
          <a
            href={buildEbirdUrl(item.species_code, getLocale())}
            target="_blank"
            rel="noopener noreferrer"
            class="card-ebird-btn"
            aria-label="View {displayName} on eBird"
            onclick={(e: MouseEvent) => e.stopPropagation()}
          >
            <ExternalLink class="size-3" />eBird
          </a>
        {/if}
        <a
          href={detectionsUrl}
          class="card-detections-btn"
          aria-label="View {displayName} detections for this day"
          onclick={(e: MouseEvent) => e.stopPropagation()}
        >
          <ExternalLink class="size-3" />Detections
        </a>
        <button
          class="card-history-btn"
          aria-label="Detection history for {displayName}"
          onclick={(e: MouseEvent) => {
            e.stopPropagation();
            showHistory = true;
          }}
        >
          <BarChart2 class="size-3" />
        </button>
      </div>

      <!-- Scientific name -->
      <span class="card-sci">{item.scientific_name}</span>

      <!-- Meta line: 762 x ↑ - 99% max conf. - last seen 7d -->
      <div class="card-meta">
        <span class="card-meta-plain">
          {formatDetectionCount(item.count)} x
        </span>
        {#if item.countIncreased === true}
          <TrendingUp class="card-trend trend-up size-3" />
        {:else if item.countIncreased === false}
          <TrendingDown class="card-trend trend-down size-3" />
        {/if}
        <span class="card-meta-sep">-</span>
        <span class="card-conf-val" style:color={computeConfidenceColor(pct)}>{pct}% max conf.</span
        >
        {#if lastSeen}
          <span class="card-meta-sep">-</span>
          <span class="card-meta-plain">{lastSeen}</span>
        {/if}
        {#if latestTime}
          <span class="card-meta-sep">-</span>
          <span class="card-meta-plain">latest {latestTime}</span>
        {/if}
      </div>

      <!-- Secondary stats: peak hour -->
      {#if peakHour !== null}
        <div class="card-stats">
          <span class="card-stat">peak {String(peakHour).padStart(2, '0')}h</span>
        </div>
      {/if}

      <!-- Novelty pill (shown only when applicable) -->
      {#if novelty}
        <span class="card-novelty" style:background={novelty.bg}>
          {novelty.label}
        </span>
      {/if}
    </div>

    <!-- Close button — absolute top-right -->
    <button
      class="card-close"
      aria-label="Close {displayName} detail"
      onclick={(e: MouseEvent) => {
        e.stopPropagation();
        onCollapse();
      }}
    >
      <X class="size-3.5" />
    </button>
  </div>

  <!-- ╔═══════════════════ CHART ════════════════════╗ -->
  <!-- Tapping the chart section also collapses the card -->
  <div
    class="card-chart-section"
    role="button"
    tabindex="0"
    aria-label="Collapse hourly chart for {displayName}"
    onclick={(e: MouseEvent) => {
      e.stopPropagation();
      onCollapse();
    }}
    onkeydown={(e: KeyboardEvent) => {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        onCollapse();
      }
    }}
  >
    <div class="card-chart-wrap">
      <HourlyMiniChart {item} {sunriseHour} {sunsetHour} {maxHour} {chartHeight} />
    </div>

    <!-- Peak count labels overlay -->
    {#if peakBars.length > 0}
      <div class="card-peak-labels" aria-hidden="true">
        {#each peakBars as { hour, count, labelTopPx } (hour)}
          <span class="card-peak-label" style:left={tickPct(hour)} style:top="{labelTopPx}px">
            {formatDetectionCount(count)}
          </span>
        {/each}
      </div>
    {/if}

    <!-- Sunrise/sunset markers just above the hour axis -->
    {#if (sunriseHour !== null && sunriseHour <= maxHour) || (sunsetHour !== null && sunsetHour <= maxHour)}
      <div class="card-sun-marks" aria-hidden="true">
        {#if sunriseHour !== null && sunriseHour <= maxHour}
          <span class="card-sun-mark sun-rise" style:left={tickPct(sunriseHour)}>
            <Sunrise class="size-3" />
          </span>
        {/if}
        {#if sunsetHour !== null && sunsetHour <= maxHour}
          <span class="card-sun-mark sun-set" style:left={tickPct(sunsetHour)}>
            <Sunset class="size-3" />
          </span>
        {/if}
      </div>
    {/if}

    <!-- Hour axis labels -->
    <div class="card-axis" aria-hidden="true">
      {#each axisTicks as tick, i (tick)}
        <span
          class="card-axis-tick"
          class:tick-first={i === 0}
          class:tick-last={i === axisTicks.length - 1}
          style:left={i === axisTicks.length - 1 ? undefined : tickPct(tick)}
          style:right={i === axisTicks.length - 1 ? '0' : undefined}
        >
          {String(tick).padStart(2, '0')}
        </span>
      {/each}
    </div>
  </div>
</div>

{#if showHistory}
  <SpeciesHistoryModal
    scientificName={item.scientific_name}
    {displayName}
    {selectedDate}
    onClose={() => (showHistory = false)}
  />
{/if}

<style>
  .species-card {
    position: relative;
    background: var(--color-base-200);
    border-radius: 0.5rem;
    overflow: hidden;
    margin: 0.1rem 0;
    border: 1px solid color-mix(in srgb, var(--color-base-content) 10%, transparent);
  }

  /* Card receives programmatic focus on expand (tabindex="-1"); the visible
     focus ring stays on the interactive children. */
  .species-card:focus {
    outline: none;
  }

  /* ── Header ── */

  .card-header {
    display: flex;
    align-items: flex-start;
    gap: 0.5rem;
    padding: 0.5rem 2.25rem 0.375rem 0.5rem; /* right pad keeps info clear of the × button */
  }

  .card-thumb {
    flex-shrink: 0;
    width: 4rem;
    height: 3rem;
  }

  .card-thumb-img {
    width: 4rem;
    height: 3rem;
    border-radius: 0.375rem;
    object-fit: cover;
    display: block;
  }

  .card-info {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
  }

  .card-name-row {
    display: flex;
    align-items: center;
    gap: 0.3rem;
    min-width: 0;
  }

  .card-name {
    font-size: 0.85rem;
    font-weight: 700;
    color: var(--color-base-content);
    text-decoration: none;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    min-width: 0;
    flex: 1;
    line-height: 1.25;
  }

  .card-name:hover {
    text-decoration: underline;
  }

  .card-ebird-btn {
    display: inline-flex;
    align-items: center;
    gap: 0.2rem;
    flex-shrink: 0;
    font-size: 0.6rem;
    font-weight: 600;
    color: var(--color-primary);
    text-decoration: none;
    border: 1px solid color-mix(in srgb, var(--color-primary) 50%, transparent);
    border-radius: 9999px;
    padding: 0.1rem 0.375rem;
    line-height: 1.4;
  }

  .card-ebird-btn:hover {
    background: color-mix(in srgb, var(--color-primary) 10%, transparent);
  }

  .card-detections-btn {
    display: inline-flex;
    align-items: center;
    gap: 0.2rem;
    flex-shrink: 0;
    font-size: 0.6rem;
    font-weight: 600;
    color: var(--color-base-content);
    text-decoration: none;
    border: 1px solid color-mix(in srgb, var(--color-base-content) 30%, transparent);
    border-radius: 9999px;
    padding: 0.1rem 0.375rem;
    line-height: 1.4;
  }

  .card-detections-btn:hover {
    background: color-mix(in srgb, var(--color-base-content) 8%, transparent);
  }

  /* Icon-only history button — same pill treatment as Detections */
  .card-history-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    color: var(--color-base-content);
    background: none;
    border: 1px solid color-mix(in srgb, var(--color-base-content) 30%, transparent);
    border-radius: 9999px;
    padding: 0.15rem;
    line-height: 1;
    cursor: pointer;
  }

  .card-history-btn:hover {
    background: color-mix(in srgb, var(--color-base-content) 8%, transparent);
  }

  .card-history-btn:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 2px;
  }

  .card-sci {
    font-size: 0.575rem;
    font-style: italic;
    color: color-mix(in srgb, var(--color-base-content) 50%, transparent);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    line-height: 1.2;
  }

  .card-meta {
    display: flex;
    align-items: center;
    gap: 0.3rem;
    flex-wrap: wrap;
    margin-top: 0.1rem;
  }

  .card-meta-plain {
    font-size: 0.65rem;
    font-weight: 700;
    color: var(--color-base-content);
    line-height: 1;
    flex-shrink: 0;
  }

  .card-meta-sep {
    font-size: 0.6rem;
    color: color-mix(in srgb, var(--color-base-content) 35%, transparent);
    line-height: 1;
  }

  .card-conf-val {
    font-size: 0.65rem;
    font-weight: 700;
    line-height: 1;
    flex-shrink: 0;
  }

  :global(.card-trend) {
    flex-shrink: 0;
  }

  :global(.card-trend.trend-up) {
    color: #22c55e;
  }

  :global(.card-trend.trend-down) {
    color: #f87171;
  }

  .card-stats {
    display: flex;
    align-items: center;
    gap: 0.3rem;
    margin-top: 0.05rem;
  }

  .card-stat {
    font-size: 0.575rem;
    font-weight: 600;
    color: color-mix(in srgb, var(--color-base-content) 55%, transparent);
    line-height: 1;
  }

  .card-novelty {
    display: inline-flex;
    align-items: center;
    font-size: 0.55rem;
    font-weight: 700;
    letter-spacing: 0.05em;
    text-transform: uppercase;
    color: white;
    padding: 0.1rem 0.45rem;
    border-radius: 9999px;
    margin-top: 0.15rem;
    align-self: flex-start;
    text-shadow: 0 1px 2px rgb(0 0 0 / 0.25);
  }

  /* Close (×) button — absolute top-right corner */
  .card-close {
    position: absolute;
    top: 0.375rem;
    right: 0.375rem;
    display: flex;
    align-items: center;
    justify-content: center;
    width: 1.5rem;
    height: 1.5rem;
    border-radius: 9999px;
    background: color-mix(in srgb, var(--color-base-content) 12%, transparent);
    color: var(--color-base-content);
    border: none;
    cursor: pointer;
    padding: 0;
  }

  .card-close:hover {
    background: color-mix(in srgb, var(--color-base-content) 22%, transparent);
  }

  .card-close:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 2px;
  }

  /* ── Chart section ── */

  .card-chart-section {
    position: relative;
    cursor: pointer;

    /* Bottom padding hosts the sun-marks strip + hour axis */
    padding: 0.375rem 0.5rem 2.125rem;
    border-top: 1px solid color-mix(in srgb, var(--color-base-content) 8%, transparent);
  }

  .card-chart-section:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: -2px;
  }

  /* SVG fills full chart-section width; height follows the chartHeight prop */
  .card-chart-wrap :global(svg) {
    width: 100%;
    height: var(--detail-chart-h, 64px);
    display: block;
  }

  /* Peak count labels floating above bars */
  .card-peak-labels {
    position: absolute;
    top: 0.375rem; /* matches card-chart-section padding-top */
    left: 0.5rem;
    right: 0.5rem;
    height: var(--detail-chart-h, 64px);
    pointer-events: none;
  }

  .card-peak-label {
    position: absolute;
    transform: translateX(-50%);
    font-size: 0.5625rem; /* 9px — smallest still-legible size */
    font-weight: 700;
    line-height: 1;
    font-variant-numeric: tabular-nums;
    color: color-mix(in srgb, var(--color-base-content) 70%, transparent);
    white-space: nowrap;
  }

  /* Sunrise/sunset icon strip between the chart and the hour axis */
  .card-sun-marks {
    position: absolute;
    bottom: 1.1875rem;
    left: 0.5rem;
    right: 0.5rem;
    height: 0.75rem;
    pointer-events: none;
  }

  .card-sun-mark {
    position: absolute;
    transform: translateX(-50%);
    line-height: 0;
  }

  .card-sun-mark.sun-rise {
    color: var(--color-warning);
  }

  .card-sun-mark.sun-set {
    color: var(--color-info);
  }

  .card-axis {
    position: absolute;
    bottom: 0.25rem;
    left: 0.5rem;
    right: 0.5rem;
    height: 0.875rem;
  }

  .card-axis-tick {
    position: absolute;
    transform: translateX(-50%);
    font-size: 0.5rem;
    line-height: 1;
    color: color-mix(in srgb, var(--color-base-content) 42%, transparent);
    font-variant-numeric: tabular-nums;
  }

  .card-axis-tick.tick-first {
    left: 0 !important;
    transform: none;
  }

  .card-axis-tick.tick-last {
    right: 0;
    left: auto !important;
    transform: none;
  }

  /* ─── Desktop (≥768px): the card renders under a heatmap row — roomier
     header, larger type, bigger thumbnail ─── */
  @media (min-width: 768px) {
    .card-header {
      gap: 0.75rem;
      padding: 0.75rem 2.75rem 0.625rem 0.75rem;
    }

    .card-thumb,
    .card-thumb-img {
      width: 5rem;
      height: 3.75rem;
    }

    .card-name {
      font-size: 1rem;
    }

    .card-sci {
      font-size: 0.7rem;
    }

    .card-meta-plain,
    .card-conf-val {
      font-size: 0.75rem;
    }

    .card-stat {
      font-size: 0.65rem;
    }

    .card-ebird-btn,
    .card-detections-btn {
      font-size: 0.7rem;
      padding: 0.15rem 0.5rem;
    }

    .card-peak-label {
      font-size: 0.625rem;
    }

    .card-axis-tick {
      font-size: 0.625rem;
    }
  }

  /* ─── Wide desktop (≥1600px): two-column layout — meta/actions left,
     large chart right ─── */
  @media (min-width: 1600px) {
    .species-card {
      display: grid;
      grid-template-columns: minmax(0, 2fr) minmax(0, 3fr);
      align-items: stretch;
    }

    /* Overlays (.card-peak-labels/.card-sun-marks/.card-axis) are positioned
       against this section's padding box, so keep normal block flow here. */
    .card-chart-section {
      border-top: none;
      border-left: 1px solid color-mix(in srgb, var(--color-base-content) 8%, transparent);
    }
  }
</style>
