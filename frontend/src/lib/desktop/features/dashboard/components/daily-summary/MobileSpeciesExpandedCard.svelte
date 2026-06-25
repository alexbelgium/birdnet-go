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

  // Trim ISO datetime or HH:MM:SS to HH:MM for compact display.
  function trimHHMM(t: string): string {
    if (!t) return '';
    const i = t.indexOf('T');
    const hhmm = i >= 0 ? t.substring(i + 1) : t;
    return hhmm.substring(0, 5);
  }

  const firstHHMM = $derived(trimHHMM(item.first_heard));
  const latestHHMM = $derived(trimHHMM(item.latest_heard));

  // Hour axis tick labels: always show 00, 06, 12, 18, and the last bar hour.
  const axisTicks = $derived(() => {
    const fixed = [0, 6, 12, 18];
    const ticks = fixed.filter(h => h <= maxHour);
    if (!ticks.includes(maxHour)) ticks.push(maxHour);
    return ticks;
  });

  const totalBars = $derived(maxHour + 1);
  function tickPct(hour: number): string {
    return `${((hour / Math.max(totalBars - 1, 1)) * 100).toFixed(1)}%`;
  }

  const thumbSrc = $derived(
    item.thumbnail_url
      ? buildAppUrl(item.thumbnail_url)
      : buildAppUrl(`/api/v2/media/species-image?name=${encodeURIComponent(item.scientific_name)}`)
  );
</script>

<!--
  Expanded species card — vertical layout: header (thumb + info) + full-width chart.
  × button or click-outside (handled in MobileSummaryTable) collapses back to compact row.
  Chart section tap also collapses.
  Double-tap on card body (outside links/buttons) navigates to daily detections.
-->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<!-- svelte-ignore a11y_click_events_have_key_events -->
<div
  class="species-card"
  aria-label="{displayName} species detail"
  onclick={(e: MouseEvent) => e.stopPropagation()}
  ondblclick={(e: MouseEvent) => {
    const target = e.target as HTMLElement | null;
    if (
      !target?.closest('a') &&
      !target?.closest('[role="button"]') &&
      !target?.closest('button')
    ) {
      window.location.href = dailyUrl;
    }
  }}
>
  <!-- × close button — absolute top-right corner -->
  <button class="close-btn" onclick={onCollapse} aria-label="Close species card">
    <X class="size-3.5" />
  </button>

  <!-- Section 1: header (thumbnail + name/stats) -->
  <div class="card-header">
    <img src={thumbSrc} alt="" class="card-thumb" loading="lazy" />

    <div class="card-info">
      <div class="card-name-row">
        <a href={speciesUrl} class="card-name">{displayName}</a>
        <SpeciesEbirdLink speciesCode={item.species_code} {displayName} />
      </div>

      <span class="card-sci-name">{item.scientific_name}</span>

      <!-- Stats strip: count · confidence · time range -->
      <div class="card-stats">
        <span class="card-count-badge">{formatDetectionCount(item.count)}</span>
        <span class="stat-sep" aria-hidden="true">·</span>
        <span class="card-conf" style:color={computeConfidenceColor(pct)}>{pct}%</span>
        {#if firstHHMM && latestHHMM}
          <span class="stat-sep" aria-hidden="true">·</span>
          <span class="card-time">{firstHHMM}–{latestHHMM}</span>
        {/if}
      </div>

      <!-- Novelty badge — show only the most significant one -->
      {#if item.is_new_species}
        <span class="novelty-badge novelty-lifetime">New species</span>
      {:else if item.is_new_this_year}
        <span class="novelty-badge novelty-year">1st this year</span>
      {:else if item.is_new_this_season}
        <span class="novelty-badge novelty-season">1st this season</span>
      {/if}
    </div>
  </div>

  <!-- Section 2: full-width hourly chart (tap to collapse) -->
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
    <HourlyMiniChart {item} {sunriseHour} {sunsetHour} {maxHour} chartHeight={64} />

    <!-- Hour axis labels positioned as percentages of chart width -->
    <div class="card-hour-labels" aria-hidden="true">
      {#each axisTicks() as tick (tick)}
        <span class="card-hour-tick" style:left={tickPct(tick)}>
          {String(tick).padStart(2, '0')}
        </span>
      {/each}
    </div>
  </div>
</div>

<style>
  .species-card {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    background: var(--color-base-200);
    border-radius: 0.5rem;
    padding: 0.5rem 0.375rem 0.375rem;
    margin: 0.1rem 0;
  }

  /* ── Close button ── */
  .close-btn {
    position: absolute;
    top: 0.25rem;
    right: 0.25rem;
    display: flex;
    align-items: center;
    justify-content: center;

    /* 1.75rem visual + transparent padding = ~44px touch target */
    width: 1.75rem;
    height: 1.75rem;
    padding: 0.5rem;
    margin: -0.5rem;
    border-radius: 9999px;
    color: var(--color-base-content);
    opacity: 0.45;
    background: transparent;
    border: none;
    cursor: pointer;
    transition: opacity 0.15s;
  }

  .close-btn:hover,
  .close-btn:focus-visible {
    opacity: 1;
  }

  .close-btn:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 2px;
  }

  /* ── Header section ── */
  .card-header {
    display: flex;
    align-items: flex-start;
    gap: 0.5rem;
    padding-right: 1.5rem; /* clear close button */
  }

  .card-thumb {
    width: 4rem;
    height: 3rem;
    border-radius: 0.375rem;
    object-fit: cover;
    flex-shrink: 0;
  }

  .card-info {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 0.1rem;
  }

  .card-name-row {
    display: flex;
    align-items: center;
    gap: 0.3rem;
    min-width: 0;
  }

  .card-name {
    font-size: 0.8rem;
    font-weight: 700;
    color: var(--color-base-content);
    text-decoration: none;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    flex: 1;
    min-width: 0;
  }

  .card-name:hover {
    text-decoration: underline;
  }

  .card-sci-name {
    font-size: 0.575rem;
    font-style: italic;
    color: color-mix(in srgb, var(--color-base-content) 50%, transparent);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* ── Stats strip ── */
  .card-stats {
    display: flex;
    align-items: center;
    flex-wrap: nowrap;
    gap: 0.25rem;
    font-size: 0.6875rem;
    overflow: hidden;
  }

  .stat-sep {
    color: color-mix(in srgb, var(--color-base-content) 30%, transparent);
    flex-shrink: 0;
  }

  .card-count-badge {
    flex-shrink: 0;
    background: #22c55e;
    color: white;
    font-size: 0.6rem;
    font-weight: 700;
    padding: 0.1rem 0.35rem;
    border-radius: 9999px;
    line-height: 1.4;
  }

  .card-conf {
    font-size: 0.6875rem;
    font-weight: 600;
    flex-shrink: 0;
  }

  .card-time {
    font-size: 0.6rem;
    color: color-mix(in srgb, var(--color-base-content) 60%, transparent);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* ── Novelty badges ── */
  .novelty-badge {
    display: inline-block;
    font-size: 0.55rem;
    font-weight: 600;
    padding: 0.1rem 0.35rem;
    border-radius: 9999px;
    width: fit-content;
    margin-top: 0.1rem;
  }

  .novelty-lifetime {
    background: #dbeafe;
    color: #1d4ed8;
  }

  .novelty-year {
    background: #fef3c7;
    color: #92400e;
  }

  .novelty-season {
    background: #d1fae5;
    color: #065f46;
  }

  /* ── Chart section ── */
  .card-chart-section {
    position: relative;
    cursor: pointer;
    border-radius: 0.25rem;
    padding-bottom: 0.875rem; /* room for axis labels */
  }

  .card-chart-section:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 2px;
  }

  /* SVG fills the full card width */
  .card-chart-section :global(svg) {
    width: 100%;
    height: 64px;
    display: block;
  }

  .card-hour-labels {
    position: absolute;
    bottom: 0;
    left: 0;
    right: 0;
    height: 0.75rem;
  }

  .card-hour-tick {
    position: absolute;
    transform: translateX(-50%);
    font-size: 0.5rem;
    line-height: 1;
    color: color-mix(in srgb, var(--color-base-content) 40%, transparent);
  }
</style>
