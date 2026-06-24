<script lang="ts">
  import type { DailySpeciesSummary } from '$lib/types/detection.types';
  import { ChevronLeft } from '@lucide/svelte';
  import { buildAppUrl } from '$lib/utils/urlHelpers';
  import { localizeSpeciesName } from '$lib/utils/speciesDisplay';
  import { computeConfidenceColor, formatDetectionCount } from '../../utils/dailySummaryStats';
  import MobileSpeciesDetailChart from './MobileSpeciesDetailChart.svelte';

  interface Props {
    item: DailySpeciesSummary;
    rank: number;
    sunriseHour: number | null;
    sunsetHour: number | null;
    getSpeciesUrl: (_item: DailySpeciesSummary) => string;
    onBack: () => void;
    selectedDate: string;
  }

  let { item, rank, sunriseHour, sunsetHour, getSpeciesUrl, onBack, selectedDate }: Props =
    $props();

  const displayName = $derived(localizeSpeciesName(item.scientific_name, item.common_name));
  const pct = $derived(Math.round(Math.max(0, Math.min(1, item.max_confidence ?? 0)) * 100));
</script>

<!--
  Level 2 single-species detail view.
  Back button → onBack() (returns to level 1 expanded list).
  Tapping the main content area → navigates to species detections page.
  Pinch-close gesture is handled by the parent MobileSummaryTable.
-->
<div class="species-detail" aria-label="Detail view for {displayName}">
  <!-- Back row: separate from the <a> so it calls onBack, not navigate -->
  <div class="detail-back-row">
    <button
      type="button"
      class="detail-back-btn"
      onclick={onBack}
      aria-label="Back to species list"
    >
      <ChevronLeft class="detail-back-icon" />
      <span>All species</span>
    </button>
  </div>

  <!-- Tappable main area navigates to detections page -->
  <a
    href={getSpeciesUrl(item)}
    class="detail-main"
    aria-label="{displayName} — view detections for {selectedDate}"
  >
    <!-- Hero: image left, info right -->
    <div class="detail-hero">
      <img
        src={item.thumbnail_url
          ? buildAppUrl(item.thumbnail_url)
          : buildAppUrl(
              `/api/v2/media/species-image?name=${encodeURIComponent(item.scientific_name)}`
            )}
        alt={displayName}
        class="detail-image"
        loading="eager"
      />
      <div class="detail-info">
        <p class="detail-rank-name">
          <span class="detail-rank">{rank}.</span>{displayName}
        </p>
        <p class="detail-scientific">{item.scientific_name}</p>
        <div class="detail-stat">
          <span class="detail-stat-label">Max Confidence</span>
          <span class="detail-stat-value" style:color={computeConfidenceColor(pct)}>
            {pct}%
          </span>
        </div>
        <div class="detail-stat">
          <span class="detail-stat-label">Daily Count</span>
          <span class="detail-stat-value detail-count">
            {formatDetectionCount(item.count)}
          </span>
        </div>
      </div>
    </div>

    <!-- Hourly chart -->
    <div class="detail-chart-section">
      <p class="detail-chart-label">Hourly Frequency</p>
      <MobileSpeciesDetailChart {item} {sunriseHour} {sunsetHour} />
    </div>
  </a>

  <!-- Pinch gesture hint -->
  <div class="detail-pinch-hint" aria-hidden="true">
    <span class="detail-pinch-icon">✋</span>
    <span>Pinch out to zoom out to see all species</span>
  </div>
</div>

<style>
  .species-detail {
    display: flex;
    flex-direction: column;
    width: 100%;
  }

  .detail-back-row {
    padding: 0 0 0.375rem;
  }

  .detail-back-btn {
    display: inline-flex;
    align-items: center;
    gap: 0.125rem;
    font-size: 0.75rem;
    color: color-mix(in srgb, var(--color-base-content) 55%, transparent);
    background: none;
    border: none;
    cursor: pointer;
    padding: 0.25rem 0.5rem 0.25rem 0;
    border-radius: 0.25rem;
    transition: color 0.1s ease;
  }

  .detail-back-btn:hover {
    color: var(--color-base-content);
  }

  :global(.detail-back-icon) {
    width: 1rem;
    height: 1rem;
  }

  .detail-main {
    display: flex;
    flex-direction: column;
    text-decoration: none;
    color: var(--color-base-content);
    border-radius: 0.5rem;
    transition: background-color 0.1s ease;
  }

  .detail-main:active {
    background-color: color-mix(in srgb, var(--color-base-content) 8%, transparent);
  }

  .detail-hero {
    display: flex;
    gap: 0.75rem;
    margin-bottom: 0.875rem;
  }

  .detail-image {
    width: 40%;
    aspect-ratio: 1;
    border-radius: 0.5rem;
    object-fit: cover;
    flex-shrink: 0;
  }

  .detail-info {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    min-width: 0;
  }

  .detail-rank-name {
    font-size: 1rem;
    font-weight: 700;
    line-height: 1.25;
    margin: 0;
  }

  .detail-rank {
    font-weight: 400;
    color: color-mix(in srgb, var(--color-base-content) 45%, transparent);
    margin-right: 0.1875rem;
  }

  .detail-scientific {
    font-size: 0.75rem;
    font-style: italic;
    color: color-mix(in srgb, var(--color-base-content) 60%, transparent);
    margin: 0 0 0.25rem;
  }

  .detail-stat {
    display: flex;
    flex-direction: column;
  }

  .detail-stat-label {
    font-size: 0.625rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: color-mix(in srgb, var(--color-base-content) 45%, transparent);
  }

  .detail-stat-value {
    font-size: 1.5rem;
    font-weight: 700;
    line-height: 1.2;
  }

  .detail-count {
    color: var(--color-base-content);
  }

  .detail-chart-section {
    margin-top: 0.125rem;
  }

  .detail-chart-label {
    font-size: 0.75rem;
    font-weight: 600;
    margin: 0 0 0.25rem;
    color: color-mix(in srgb, var(--color-base-content) 75%, transparent);
  }

  .detail-pinch-hint {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    margin-top: 1rem;
    padding: 0.5rem 0.25rem;
    font-size: 0.6875rem;
    color: color-mix(in srgb, var(--color-base-content) 35%, transparent);
    border-top: 1px solid var(--color-base-200);
  }

  .detail-pinch-icon {
    font-size: 1rem;
    flex-shrink: 0;
  }
</style>
