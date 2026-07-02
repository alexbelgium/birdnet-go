<script lang="ts">
  import type { DailySpeciesSummary } from '$lib/types/detection.types';
  import { buildAppUrl } from '$lib/utils/urlHelpers';
  import { localizeSpeciesName } from '$lib/utils/speciesDisplay';
  import { computeConfidenceColor, formatDetectionCount } from '../../utils/dailySummaryStats';
  import { resolveNoveltyCategory, noveltyCategoryColorVar } from '../../utils/noveltyCategory';
  import { buildHourlyDetectionUrl } from '$lib/utils/detectionUrls';
  import { getLocalDateString } from '$lib/utils/date';
  import { Star } from '@lucide/svelte';
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

  // Timer map for distinguishing single-tap (→ expand) from double-tap (→ daily detections).
  const rowTimers = new Map<string, ReturnType<typeof setTimeout>>();

  // Full-day detections URL for the selected date.
  const dailyUrl = $derived(buildHourlyDetectionUrl(selectedDate, 0, 24));

  // Truncate chart at current hour when viewing today (avoids empty future bars).
  const isToday = $derived(selectedDate === getLocalDateString());
  const currentHour = $derived(isToday ? new Date().getHours() : 23);

  // Chart column tracks the chart's actual rendered width (must match HourlyMiniChart's
  // BAR_WIDTH + gap = 4px/bar) so leftover space goes to the species name instead of
  // sitting empty next to a shorter-than-24h chart.
  const CHART_BAR_STRIDE = 4;
  const chartColWidthPx = $derived((currentHour + 1) * CHART_BAR_STRIDE);

  // Gates the SSE row-flash animation for users who prefer reduced motion.
  const prefersReducedMotion =
    typeof window !== 'undefined'
      ? (window.matchMedia?.('(prefers-reduced-motion: reduce)')?.matches ?? false)
      : false;
</script>

<!--
  Compact mobile species table — replaces the heatmap grid on screens <768 px.
  Interaction model:
    - Single tap anywhere on a row → expand into species card (300 ms guard)
    - Double-tap anywhere on a row → navigate to daily detections
-->
<div
  class="mobile-summary-table w-full"
  aria-label="Species detected on {selectedDate}"
  style:--col-chart-w="{chartColWidthPx}px"
>
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
    {@const novelty = resolveNoveltyCategory(item)}

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
        {selectedDate}
      />
    {:else}
      <!-- Compact row — single tap → expand; double tap → daily log -->
      <div
        class="mobile-summary-row"
        class:row-updated={((item.hourlyUpdated?.length ?? 0) > 0 ||
          item.countIncreased === true) &&
          !prefersReducedMotion}
        role="button"
        tabindex="0"
        aria-label="{displayName}: {pct}% confidence, {formatDetectionCount(
          item.count
        )} detections. Tap to expand."
        onclick={(_e: MouseEvent) => {
          const sci = item.scientific_name;
          if (rowTimers.has(sci)) {
            const t = rowTimers.get(sci);
            if (t !== undefined) clearTimeout(t);
            rowTimers.delete(sci);
            window.location.href = dailyUrl;
          } else {
            const t = setTimeout(() => {
              rowTimers.delete(sci);
              expandedSpecies = sci;
            }, 300);
            rowTimers.set(sci, t);
          }
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
    <div class="zoom-hint" aria-hidden="true">tap to expand · double-tap for daily log</div>
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

  /* ─── 5-column grid; portrait uses a compact thumbnail, landscape a larger one ─── */

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

    /* Pinned while the (potentially long) species list scrolls */
    position: sticky;
    top: 0;
    z-index: 10;
    background: var(--color-base-100);
  }

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
    flex: 1;
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

  /* ─── Landscape: larger thumbnail + scientific name ─── */
  @media (orientation: landscape) {
    .mobile-summary-table {
      --thumb-w: 2rem;
      --thumb-h: 1.5rem;
    }

    .col-scientific-name {
      display: block;
    }
  }
</style>
