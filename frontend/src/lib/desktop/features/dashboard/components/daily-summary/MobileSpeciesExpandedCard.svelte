<script lang="ts">
  import type { DailySpeciesSummary } from '$lib/types/detection.types';
  import { buildAppUrl } from '$lib/utils/urlHelpers';
  import { computeConfidenceColor, formatDetectionCount } from '../../utils/dailySummaryStats';
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

  // Hour axis tick labels: always show 00, 06, 12, 18, and the last bar hour.
  const axisTicks = $derived(() => {
    const fixed = [0, 6, 12, 18];
    const ticks = fixed.filter(h => h <= maxHour);
    if (!ticks.includes(maxHour)) ticks.push(maxHour);
    return ticks;
  });

  // Position of a tick as a percentage of the chart width.
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
  Expanded species card — replaces the compact row when a species chart is tapped.
  Tapping the chart area collapses back to the compact row.
  Tapping the species name navigates to species detections.
  Double-tapping the card body (outside links/buttons) navigates to daily detections.
-->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  class="species-card"
  aria-label="{displayName} species detail"
  ondblclick={(e: MouseEvent) => {
    const target = e.target as HTMLElement | null;
    if (!target?.closest('a') && !target?.closest('[role="button"]')) {
      window.location.href = dailyUrl;
    }
  }}
>
  <!-- Thumbnail -->
  <div class="card-thumb">
    <img src={thumbSrc} alt="" class="card-thumb-img" loading="lazy" />
  </div>

  <!-- Body: name + count + chart -->
  <div class="card-body">
    <div class="card-title-row">
      <a href={speciesUrl} class="card-name">{displayName}</a>
      <span class="card-count-badge">{formatDetectionCount(item.count)}</span>
      <SpeciesEbirdLink speciesCode={item.species_code} {displayName} />
    </div>
    <span class="card-sci-name">{item.scientific_name}</span>

    <!-- Chart: tap to collapse -->
    <div
      class="card-chart-area"
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
      <HourlyMiniChart {item} {sunriseHour} {sunsetHour} {maxHour} chartHeight={48} />

      <!-- Hour axis labels positioned as percentages of the chart width -->
      <div class="card-hour-labels" aria-hidden="true">
        {#each axisTicks() as tick (tick)}
          <span class="card-hour-tick" style:left={tickPct(tick)}>
            {String(tick).padStart(2, '0')}
          </span>
        {/each}
      </div>
    </div>
  </div>

  <!-- Right: confidence -->
  <div class="card-conf">
    <span class="card-conf-value" style:color={computeConfidenceColor(pct)}>{pct}%</span>
    <span class="card-conf-label">Max conf.</span>
  </div>

  <!-- Collapse indicator -->
  <div class="card-chevron" aria-hidden="true">&#8250;</div>
</div>

<style>
  .species-card {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    background: var(--color-base-200);
    border-radius: 0.5rem;
    padding: 0.5rem 0.5rem 0.5rem 0.375rem;
    margin: 0.1rem 0;
  }

  .card-thumb {
    flex-shrink: 0;
    width: 3rem;
    height: 3rem;
  }

  .card-thumb-img {
    width: 3rem;
    height: 3rem;
    border-radius: 0.375rem;
    object-fit: cover;
    display: block;
  }

  .card-body {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
  }

  .card-title-row {
    display: flex;
    align-items: center;
    gap: 0.35rem;
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
    min-width: 0;
    flex: 1;
  }

  .card-name:hover {
    text-decoration: underline;
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

  .card-sci-name {
    font-size: 0.575rem;
    font-style: italic;
    color: color-mix(in srgb, var(--color-base-content) 50%, transparent);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .card-chart-area {
    position: relative;
    cursor: pointer;
    border-radius: 0.25rem;
    padding-bottom: 0.875rem; /* room for axis labels */
  }

  .card-chart-area:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 2px;
  }

  /* SVG inside fills available width */
  .card-chart-area :global(svg) {
    width: 100%;
    height: 48px;
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

  .card-conf {
    flex-shrink: 0;
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 0.1rem;
  }

  .card-conf-value {
    font-size: 0.9rem;
    font-weight: 700;
    line-height: 1;
  }

  .card-conf-label {
    font-size: 0.5rem;
    color: color-mix(in srgb, var(--color-base-content) 50%, transparent);
    white-space: nowrap;
  }

  .card-chevron {
    flex-shrink: 0;
    font-size: 1.1rem;
    line-height: 1;
    color: color-mix(in srgb, var(--color-base-content) 35%, transparent);
    align-self: center;
    user-select: none;
  }
</style>
