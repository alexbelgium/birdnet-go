<script lang="ts">
  import type { DailySpeciesSummary } from '$lib/types/detection.types';
  import { fade } from 'svelte/transition';
  import { buildAppUrl } from '$lib/utils/urlHelpers';
  import { localizeSpeciesName } from '$lib/utils/speciesDisplay';
  import { computeConfidenceColor, formatDetectionCount } from '../../utils/dailySummaryStats';
  import { createPinchDetector } from '../../utils/pinchGesture';
  import HourlyMiniChart from './HourlyMiniChart.svelte';
  import MobileSpeciesDetail from './MobileSpeciesDetail.svelte';

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

  // Zoom state: 0 = compact, 1 = expanded rows, 2 = single-species detail.
  let zoomLevel: 0 | 1 | 2 = $state(0);
  let focusedSpecies: DailySpeciesSummary | null = $state(null);
  let containerEl: HTMLDivElement | undefined = $state();

  $effect(() => {
    if (!containerEl) return;
    const detector = createPinchDetector();
    return detector.attach(containerEl, (scale, midX, midY) => {
      if (scale > 1.3 && zoomLevel < 2) {
        if (zoomLevel === 1) {
          // Identify which species row the pinch midpoint falls on.
          const el = document.elementFromPoint(midX, midY);
          const row = el?.closest('[data-scientific]');
          const sci = row?.getAttribute('data-scientific') ?? null;
          focusedSpecies =
            (sci !== null ? data.find(d => d.scientific_name === sci) : undefined) ??
            data[0] ??
            null;
        }
        zoomLevel = (zoomLevel + 1) as 0 | 1 | 2;
      } else if (scale < 0.75 && zoomLevel > 0) {
        const newLevel = (zoomLevel - 1) as 0 | 1 | 2;
        zoomLevel = newLevel;
        if (newLevel === 0) focusedSpecies = null;
      }
    });
  });
</script>

<!--
  Compact mobile species table — replaces the heatmap grid on screens <768 px.
  Supports three pinch-driven zoom levels:
    0 = compact rows (default)
    1 = expanded rows — all species, scientific name visible, thumbnail always shown
    2 = single-species detail (MobileSpeciesDetail)
-->
<div
  bind:this={containerEl}
  class="mobile-summary-table w-full zoom-{zoomLevel}"
  aria-label="Species detected on {selectedDate}"
>
  {#if zoomLevel === 2 && focusedSpecies !== null}
    {@const rank = data.indexOf(focusedSpecies) + 1}
    <div transition:fade={{ duration: 180 }}>
      <MobileSpeciesDetail
        item={focusedSpecies}
        {rank}
        {sunriseHour}
        {sunsetHour}
        {getSpeciesUrl}
        {selectedDate}
        onBack={() => {
          zoomLevel = 1;
        }}
      />
    </div>
  {:else}
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
        data-scientific={item.scientific_name}
        aria-label="{displayName}: {pct}% confidence, {formatDetectionCount(item.count)} detections"
      >
        <!-- Thumbnail or initials badge — portrait hides at zoom 0, shown at zoom 1+ -->
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

        <!-- Species name + scientific name (scientific hidden at zoom 0) -->
        <div class="col-name-group">
          <span class="col-name text-sm font-medium truncate leading-tight">
            {displayName}
          </span>
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

    {#if data.length > 0}
      <div class="zoom-hint" aria-hidden="true">pinch to zoom</div>
    {/if}
  {/if}
</div>

<style>
  .mobile-summary-table {
    --thumb-w: 2rem;
    --col-conf-w: 2.5rem;
    --col-count-w: 2.25rem;

    /* chart min keeps the 96 px SVG intact; name:chart fr ratio = φ (1.618) */
    --col-chart-min: 6rem;
  }

  /* ─── Portrait default (zoom 0): 4-column grid, no thumbnail ─── */

  /* Name (1.618fr) : chart (1fr) = golden ratio. */
  .mobile-summary-header {
    display: grid;
    grid-template-columns: 1.618fr var(--col-conf-w) var(--col-count-w) minmax(
        var(--col-chart-min),
        1fr
      );
    align-items: center;
    gap: 0.25rem;
    padding: 0 0.125rem 0.375rem;
    margin-bottom: 0.125rem;
    border-bottom: 1px solid var(--color-base-200);
    font-size: 0.625rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: color-mix(in srgb, var(--color-base-content) 50%, transparent);
  }

  .col-header-conf,
  .col-header-count,
  .col-header-chart {
    text-align: right;
  }

  .mobile-summary-row {
    display: grid;
    grid-template-columns: 1.618fr var(--col-conf-w) var(--col-count-w) minmax(
        var(--col-chart-min),
        1fr
      );
    align-items: center;
    gap: 0.25rem;
    padding: 0.15rem 0.125rem;
    min-height: 2.25rem;
    border-radius: 0.375rem;
    text-decoration: none;
    color: var(--color-base-content);
    transition:
      background-color 0.1s ease,
      min-height 0.2s ease,
      padding 0.2s ease;
  }

  .mobile-summary-row:hover,
  .mobile-summary-row:active {
    background-color: color-mix(in srgb, var(--color-base-content) 8%, transparent);
  }

  /* Thumbnail column hidden by default (portrait, zoom 0) */
  .mobile-thumb-col {
    display: none;
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

  .col-name-group {
    display: flex;
    flex-direction: column;
    justify-content: center;
    min-width: 0;
  }

  /* Scientific name hidden at zoom 0 */
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
  }

  /* Subtle "pinch to zoom" hint below the species list */
  .zoom-hint {
    text-align: center;
    font-size: 0.625rem;
    letter-spacing: 0.05em;
    text-transform: uppercase;
    color: color-mix(in srgb, var(--color-base-content) 25%, transparent);
    padding: 0.625rem 0 0.125rem;
  }

  /* ─── Landscape (zoom 0): restore thumbnail column ─── */
  @media (orientation: landscape) {
    .mobile-summary-header {
      grid-template-columns: var(--thumb-w) 1.618fr var(--col-conf-w) var(--col-count-w) minmax(
          var(--col-chart-min),
          1fr
        );
    }

    /* Span thumbnail + name columns in header */
    .col-header-species {
      grid-column: 1 / 3;
    }

    .mobile-summary-row {
      grid-template-columns: var(--thumb-w) 1.618fr var(--col-conf-w) var(--col-count-w) minmax(
          var(--col-chart-min),
          1fr
        );
    }

    .mobile-thumb-col {
      display: flex;
      align-items: center;
    }
  }

  /* ─── Zoom level 1: expanded rows, thumbnail always shown ─── */
  .zoom-1 .mobile-summary-header {
    grid-template-columns: var(--thumb-w) 1.618fr var(--col-conf-w) var(--col-count-w) minmax(
        var(--col-chart-min),
        1fr
      );
  }

  .zoom-1 .col-header-species {
    grid-column: 1 / 3;
  }

  .zoom-1 .mobile-summary-row {
    grid-template-columns: var(--thumb-w) 1.618fr var(--col-conf-w) var(--col-count-w) minmax(
        var(--col-chart-min),
        1fr
      );
    min-height: 4rem;
    padding: 0.375rem 0.125rem;
    align-items: center;
  }

  /* Show thumbnail at zoom 1 regardless of orientation */
  .zoom-1 .mobile-thumb-col {
    display: flex;
    align-items: center;
  }

  /* Grow thumbnail at zoom 1 */
  .zoom-1 .mobile-thumb {
    height: 2.25rem;
  }

  .zoom-1 .mobile-badge {
    height: 2.25rem;
    font-size: 0.625rem;
  }

  /* Reveal scientific name at zoom 1 */
  .zoom-1 .col-scientific-name {
    display: block;
  }

  /* Hide zoom hint at zoom 1 (user already knows the gesture) */
  .zoom-1 .zoom-hint {
    display: none;
  }
</style>
