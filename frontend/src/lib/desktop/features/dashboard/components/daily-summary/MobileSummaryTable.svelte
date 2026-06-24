<script lang="ts">
  import type { DailySpeciesSummary } from '$lib/types/detection.types';
  import { buildAppUrl } from '$lib/utils/urlHelpers';
  import { localizeSpeciesName } from '$lib/utils/speciesDisplay';
  import { computeConfidenceColor, formatDetectionCount } from '../../utils/dailySummaryStats';
  import HourlyMiniChart from './HourlyMiniChart.svelte';

  interface Props {
    data: DailySpeciesSummary[];
    sunriseHour: number | null;
    sunsetHour: number | null;
    getSpeciesUrl: (_item: DailySpeciesSummary) => string;
    showThumbnails: boolean;
    selectedDate: string;
  }

  let { data, sunriseHour, sunsetHour, getSpeciesUrl, showThumbnails, selectedDate }: Props =
    $props();

  // 12-color palette — same as DailySummaryCard badge colors for visual consistency.
  const BADGE_COLORS = [
    '#10b981',
    '#f59e0b',
    '#ef4444',
    '#8b5cf6',
    '#06b6d4',
    '#ec4899',
    '#84cc16',
    '#f97316',
    '#6366f1',
    '#14b8a6',
    '#a855f7',
    '#eab308',
  ] as const;

  function getSpeciesBadgeColor(speciesName: string): string {
    let hash = 0;
    for (let i = 0; i < speciesName.length; i++) {
      hash = speciesName.charCodeAt(i) + ((hash << 5) - hash);
    }
    return BADGE_COLORS[Math.abs(hash) % BADGE_COLORS.length];
  }

  function getSpeciesInitials(commonName: string): string {
    const words = commonName.trim().split(/\s+/).filter(Boolean);
    if (words.length === 0) return '??';
    if (words.length === 1) return (words[0] ?? '').substring(0, 2).toUpperCase();
    return ((words[0] ?? '')[0] + (words[1] ?? '')[0]).toUpperCase();
  }
</script>

<!--
  Compact mobile species table — replaces the heatmap grid on screens <768 px.
  Each row links to species detections for the selected date.
  Columns: thumbnail | species name | confidence % | count | 24-bar mini chart.
-->
<div class="mobile-summary-table w-full" aria-label="Species detected on {selectedDate}">
  <!-- Header -->
  <div class="mobile-summary-header" aria-hidden="true">
    <div class="col-header-species">Species</div>
    <div class="col-header-conf">Conf.</div>
    <div class="col-header-count">Cnt</div>
    <div class="col-header-chart">Hourly</div>
  </div>

  {#each data as item (item.scientific_name)}
    {@const displayName = localizeSpeciesName(item.scientific_name, item.common_name)}
    {@const pct = Math.round(Math.max(0, Math.min(1, item.max_confidence ?? 0)) * 100)}
    <a
      href={getSpeciesUrl(item)}
      class="mobile-summary-row"
      aria-label="{displayName}: {pct}% confidence, {formatDetectionCount(item.count)} detections"
    >
      <!-- Thumbnail or initials badge -->
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

      <!-- Species name -->
      <span class="col-name text-sm font-medium truncate leading-tight">
        {displayName}
      </span>

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

      <!-- 24-bar hourly frequency chart -->
      <div class="col-chart">
        <HourlyMiniChart {item} {sunriseHour} {sunsetHour} />
      </div>
    </a>
  {/each}

  {#if data.length === 0}
    <div class="py-8 text-center text-sm text-[var(--color-base-content)]/60">
      No species detected
    </div>
  {/if}
</div>

<style>
  .mobile-summary-table {
    --thumb-w: 2rem;
    --col-conf-w: 2.75rem;
    --col-count-w: 2.5rem;
    --col-chart-w: 6.25rem;
  }

  .mobile-summary-header {
    display: grid;

    /* thumbnail column + name + conf + count + chart */
    grid-template-columns: var(--thumb-w) 1fr var(--col-conf-w) var(--col-count-w) var(
        --col-chart-w
      );
    align-items: center;
    gap: 0.375rem;
    padding: 0 0.25rem 0.375rem;
    margin-bottom: 0.125rem;
    border-bottom: 1px solid var(--color-base-200);
    font-size: 0.625rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: color-mix(in srgb, var(--color-base-content) 50%, transparent);
  }

  /* The header's first cell spans thumbnail+name columns */
  .col-header-species {
    grid-column: 1 / 3;
  }

  .col-header-conf,
  .col-header-count,
  .col-header-chart {
    text-align: right;
  }

  .mobile-summary-row {
    display: grid;
    grid-template-columns: var(--thumb-w) 1fr var(--col-conf-w) var(--col-count-w) var(
        --col-chart-w
      );
    align-items: center;
    gap: 0.375rem;
    padding: 0.2rem 0.25rem;
    min-height: 2.25rem;
    border-radius: 0.375rem;
    text-decoration: none;
    color: var(--color-base-content);
    transition: background-color 0.1s ease;
  }

  .mobile-summary-row:hover,
  .mobile-summary-row:active {
    background-color: color-mix(in srgb, var(--color-base-content) 8%, transparent);
  }

  .mobile-thumb {
    width: var(--thumb-w);
    height: 1.5rem;
    border-radius: 0.25rem;
    object-fit: cover;
    display: block;
    flex-shrink: 0;
  }

  .mobile-badge {
    display: flex;
    align-items: center;
    justify-content: center;
    width: var(--thumb-w);
    height: 1.5rem;
    border-radius: 0.25rem;
    font-size: 0.5rem;
    font-weight: 700;
    color: white;
    text-shadow: 0 1px 2px rgb(0 0 0 / 0.3);
    flex-shrink: 0;
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
  }
</style>
