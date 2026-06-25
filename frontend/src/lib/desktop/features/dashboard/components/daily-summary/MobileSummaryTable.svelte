<script lang="ts">
  import type { DailySpeciesSummary } from '$lib/types/detection.types';
  import { buildAppUrl } from '$lib/utils/urlHelpers';
  import { localizeSpeciesName } from '$lib/utils/speciesDisplay';
  import { computeConfidenceColor, formatDetectionCount } from '../../utils/dailySummaryStats';
  import { buildHourlyDetectionUrl } from '$lib/utils/detectionUrls';
  import { getLocalDateString } from '$lib/utils/date';
  import HourlyMiniChart from './HourlyMiniChart.svelte';
  import MobileSpeciesExpandedCard from './MobileSpeciesExpandedCard.svelte';

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

  // Per-row expansion state — scientific name of the expanded row, or null.
  let expandedSpecies: string | null = $state(null);

  // Timer map for distinguishing single-tap (→ species) from double-tap (→ daily detections).
  const clickTimers = new Map<string, ReturnType<typeof setTimeout>>();

  // Full-day detections URL for the selected date.
  const dailyUrl = $derived(buildHourlyDetectionUrl(selectedDate, 0, 24));

  // Truncate chart at current hour when viewing today (avoids empty future bars).
  const isToday = $derived(selectedDate === getLocalDateString());
  const currentHour = $derived(isToday ? new Date().getHours() : 23);
</script>

<!--
  Compact mobile species table — replaces the heatmap grid on screens <768 px.
  Interaction model:
    - Tap species name → navigate to species detections (250 ms window)
    - Double-tap name → navigate to daily detections
    - Tap chart column → expand/collapse that row's chart inline
    - Tap eBird icon → open species on eBird (new tab)
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
    {@const isExpanded = expandedSpecies === item.scientific_name}

    {#if isExpanded}
      <!-- Expanded: full species card replaces the compact row -->
      <MobileSpeciesExpandedCard
        {item}
        {sunriseHour}
        {sunsetHour}
        {displayName}
        speciesUrl={getSpeciesUrl(item)}
        maxHour={currentHour}
        onCollapse={() => {
          expandedSpecies = null;
        }}
        {dailyUrl}
      />
    {:else}
      <!-- Compact row — no eBird icon; chart truncated at currentHour -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        class="mobile-summary-row"
        data-scientific={item.scientific_name}
        ondblclick={(e: MouseEvent) => {
          const target = e.target as HTMLElement | null;
          if (!target?.closest('a') && !target?.closest('[role="button"]')) {
            window.location.href = dailyUrl;
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

        <!-- Species name + scientific name (no eBird icon in compact view) -->
        <div class="col-name-group">
          <div class="col-name-row">
            <a
              href={getSpeciesUrl(item)}
              class="col-name text-sm font-medium truncate leading-tight"
              aria-label="{displayName}: {pct}% confidence, {formatDetectionCount(
                item.count
              )} detections"
              onclick={(e: MouseEvent) => {
                const sci = item.scientific_name;
                if (clickTimers.has(sci)) {
                  clearTimeout(clickTimers.get(sci)!);
                  clickTimers.delete(sci);
                  e.preventDefault();
                  window.location.href = dailyUrl;
                } else {
                  const t = setTimeout(() => {
                    clickTimers.delete(sci);
                  }, 250);
                  clickTimers.set(sci, t);
                  // single-tap: default href navigation proceeds after 250 ms window
                }
              }}
            >
              {displayName}
            </a>
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

        <!-- Hourly chart truncated at currentHour — tap to expand into card -->
        <div
          class="col-chart"
          role="button"
          tabindex="0"
          aria-expanded={false}
          aria-label="Expand hourly chart for {displayName}"
          onclick={(e: MouseEvent) => {
            e.stopPropagation();
            expandedSpecies = item.scientific_name;
          }}
          onkeydown={(e: KeyboardEvent) => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault();
              expandedSpecies = item.scientific_name;
            }
          }}
        >
          <HourlyMiniChart {item} {sunriseHour} {sunsetHour} maxHour={currentHour} />
        </div>
      </div>
    {/if}
  {/each}

  {#if data.length === 0}
    <div class="py-8 text-center text-sm text-[var(--color-base-content)]/60">
      No species detected
    </div>
  {/if}

  {#if data.length > 0}
    <div class="zoom-hint" aria-hidden="true">tap chart to expand · double-tap for daily log</div>
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

  /* ─── Portrait default: 4-column grid, no thumbnail ─── */

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
    padding: 0 0.125rem;
    min-height: 1.25rem;
    border-radius: 0.375rem;
    color: var(--color-base-content);
  }

  /* Thumbnail column hidden by default (portrait) */
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
    flex: 1;
    color: var(--color-base-content);
    text-decoration: none;
  }

  .col-name:hover {
    text-decoration: underline;
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
    cursor: pointer;
    border-radius: 0.25rem;
    padding: 0.125rem 0;
  }

  .col-chart:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 2px;
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

  /* ─── Landscape: restore thumbnail column ─── */
  @media (orientation: landscape) {
    .mobile-summary-header {
      grid-template-columns: var(--thumb-w) 1.618fr var(--col-conf-w) var(--col-count-w) minmax(
          var(--col-chart-min),
          1fr
        );
    }

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

    .col-scientific-name {
      display: block;
    }
  }
</style>
