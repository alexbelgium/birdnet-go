<script lang="ts">
  import type { DailySpeciesSummary } from '$lib/types/detection.types';
  import { buildAppUrl } from '$lib/utils/urlHelpers';
  import { computeConfidenceColor, formatDetectionCount } from '../../utils/dailySummaryStats';
  import { X } from '@lucide/svelte';
  import HourlyMiniChart from './HourlyMiniChart.svelte';
  import SpeciesEbirdLink from './SpeciesEbirdLink.svelte';

  interface Props {
    item: DailySpeciesSummary;
    sunriseHour: number | null;
    sunsetHour: number | null;
    displayName: string;
    speciesUrl: string;
    maxHour: number;
    onCollapse: () => void;
    dailyUrl: string;
  }

  let {
    item,
    sunriseHour,
    sunsetHour,
    displayName,
    speciesUrl,
    maxHour,
    onCollapse,
    dailyUrl,
  }: Props = $props();

  const pct = $derived(Math.round(Math.max(0, Math.min(1, item.max_confidence ?? 0)) * 100));

  function formatDetectionTime(timeStr: string): string {
    if (!timeStr) return '';
    // Handle "HH:MM" or "HH:MM:SS" bare time strings
    const timeOnly = timeStr.match(/^(\d{1,2}):(\d{2})/);
    if (timeOnly) {
      return `${(timeOnly[1] ?? '0').padStart(2, '0')}:${timeOnly[2] ?? '00'}`;
    }
    // Handle ISO 8601 / RFC3339 datetime strings
    const d = new Date(timeStr);
    if (!isNaN(d.getTime())) {
      return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
    }
    return '';
  }

  const firstTime = $derived(formatDetectionTime(item.first_heard));
  const lastTime = $derived(formatDetectionTime(item.latest_heard));
  const timeRange = $derived(
    firstTime && lastTime && firstTime !== lastTime
      ? `${firstTime}–${lastTime}`
      : firstTime || lastTime
  );

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

  // Tick position as a percentage of the chart width (bar centres).
  function tickPct(hour: number): string {
    if (maxHour === 0) return '0%';
    return `${((hour / maxHour) * 100).toFixed(1)}%`;
  }

  const thumbSrc = $derived(
    item.thumbnail_url
      ? buildAppUrl(item.thumbnail_url)
      : buildAppUrl(`/api/v2/media/species-image?name=${encodeURIComponent(item.scientific_name)}`)
  );
</script>

<!--
  Expanded species card — replaces the compact row when a species chart is tapped.
  Close button (×) or tapping the chart section collapses back to compact row.
  Tapping the species name navigates to species detections.
  Double-tapping anywhere (outside links/buttons) navigates to daily detections.
-->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  class="species-card"
  aria-label="{displayName} species detail"
  onclick={(e: MouseEvent) => e.stopPropagation()}
  onkeydown={(e: KeyboardEvent) => e.stopPropagation()}
  ondblclick={(e: MouseEvent) => {
    const target = e.target as HTMLElement | null;
    if (
      !target?.closest('a') &&
      !target?.closest('button') &&
      !target?.closest('[role="button"]')
    ) {
      window.location.href = dailyUrl;
    }
  }}
>
  <!-- ╔═══════════════════ HEADER ═══════════════════╗ -->
  <div class="card-header">
    <!-- Thumbnail: 4rem × 3rem -->
    <div class="card-thumb">
      <img src={thumbSrc} alt="" class="card-thumb-img" loading="lazy" />
    </div>

    <!-- Info column -->
    <div class="card-info">
      <!-- Name + eBird (× button is absolute) -->
      <div class="card-name-row">
        <a href={speciesUrl} class="card-name">{displayName}</a>
        <span class="card-ebird-wrap">
          <SpeciesEbirdLink speciesCode={item.species_code} {displayName} />
        </span>
      </div>

      <!-- Scientific name -->
      <span class="card-sci">{item.scientific_name}</span>

      <!-- Meta line: count · conf% · time range -->
      <div class="card-meta">
        <span class="card-count-badge">{formatDetectionCount(item.count)}</span>
        <span class="card-meta-sep">·</span>
        <span class="card-conf-val" style:color={computeConfidenceColor(pct)}>{pct}%</span>
        {#if timeRange}
          <span class="card-meta-sep">·</span>
          <span class="card-time">{timeRange}</span>
        {/if}
      </div>

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
      <HourlyMiniChart {item} {sunriseHour} {sunsetHour} {maxHour} chartHeight={64} />
    </div>

    <!-- Hour axis labels -->
    <div class="card-axis" aria-hidden="true">
      {#each axisTicks as tick (tick)}
        <span class="card-axis-tick" style:left={tickPct(tick)}>
          {String(tick).padStart(2, '0')}
        </span>
      {/each}
    </div>
  </div>
</div>

<style>
  .species-card {
    position: relative;
    background: var(--color-base-200);
    border-radius: 0.5rem;
    overflow: hidden;
    margin: 0.1rem 0;
    border: 1px solid color-mix(in srgb, var(--color-base-content) 10%, transparent);
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

  .card-ebird-wrap {
    flex-shrink: 0;
    display: flex;
    align-items: center;
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

  .card-count-badge {
    display: inline-flex;
    align-items: center;
    background: #22c55e;
    color: white;
    font-size: 0.6rem;
    font-weight: 700;
    padding: 0.1rem 0.35rem;
    border-radius: 9999px;
    line-height: 1.4;
    flex-shrink: 0;
  }

  .card-meta-sep {
    font-size: 0.6rem;
    color: color-mix(in srgb, var(--color-base-content) 35%, transparent);
    line-height: 1;
  }

  .card-conf-val {
    font-size: 0.7rem;
    font-weight: 700;
    line-height: 1;
    flex-shrink: 0;
  }

  .card-time {
    font-size: 0.6rem;
    font-weight: 500;
    color: color-mix(in srgb, var(--color-base-content) 60%, transparent);
    font-variant-numeric: tabular-nums;
    flex-shrink: 0;
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
    padding: 0.375rem 0.5rem 1.375rem;
    border-top: 1px solid color-mix(in srgb, var(--color-base-content) 8%, transparent);
  }

  .card-chart-section:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: -2px;
  }

  /* SVG fills full chart-section width */
  .card-chart-wrap :global(svg) {
    width: 100%;
    height: 64px;
    display: block;
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
</style>
