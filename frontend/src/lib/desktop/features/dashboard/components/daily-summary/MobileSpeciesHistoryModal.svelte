<script lang="ts">
  // Self-contained species detection-history modal for the mobile dashboard.
  // Charts daily detection counts for one species over a selectable range using
  // the existing GET /api/v2/analytics/time/daily endpoint. All strings are
  // hardcoded English (no i18n) so the mobile feature stays self-contained.
  import { scaleBand, scaleLinear } from 'd3-scale';
  import { line, curveMonotoneX } from 'd3-shape';
  import { X } from '@lucide/svelte';
  import { buildAppUrl } from '$lib/utils/urlHelpers';
  import { safeArrayAccess } from '$lib/utils/security';
  import { formatDetectionCount } from '../../utils/dailySummaryStats';

  interface Props {
    scientificName: string;
    displayName: string;
    selectedDate: string; // YYYY-MM-DD (range end)
    onClose: () => void;
  }

  let { scientificName, displayName, selectedDate, onClose }: Props = $props();

  interface DailyCount {
    date: string;
    count: number;
  }

  const DAY_MS = 86_400_000;
  const RANGE_PRESETS = [7, 30, 90] as const;
  const AVG_WINDOW = 7;

  let rangeDays = $state<number>(30);
  let points = $state<DailyCount[]>([]);
  let loading = $state(true);
  let error = $state(false);

  // ── Date helpers (UTC math, manual formatting — never toISOString) ──
  function parseYmd(s: string): number {
    const [y, m, d] = s.split('-').map(Number);
    return Date.UTC(y ?? 1970, (m ?? 1) - 1, d ?? 1);
  }

  function formatYmd(utcMs: number): string {
    const dt = new Date(utcMs);
    const y = dt.getUTCFullYear();
    const m = String(dt.getUTCMonth() + 1).padStart(2, '0');
    const d = String(dt.getUTCDate()).padStart(2, '0');
    return `${y}-${m}-${d}`;
  }

  // Short "M/D" label for axis ends.
  function shortDate(ymd: string): string {
    const [, m, d] = ymd.split('-').map(Number);
    return `${m ?? ''}/${d ?? ''}`;
  }

  function dateRange(startMs: number, endMs: number): string[] {
    const out: string[] = [];
    for (let t = startMs; t <= endMs; t += DAY_MS) out.push(formatYmd(t));
    return out;
  }

  function isDailyCount(v: unknown): v is DailyCount {
    if (typeof v !== 'object' || v === null) return false;
    const o = v as Record<string, unknown>;
    return typeof o.date === 'string' && typeof o.count === 'number';
  }

  // ── Data loading (aborts the in-flight request on range change / unmount) ──
  async function load(days: number, signal: AbortSignal): Promise<void> {
    loading = true;
    error = false;
    const endMs = parseYmd(selectedDate);
    const startMs = endMs - (days - 1) * DAY_MS;
    const start = formatYmd(startMs);
    const url = buildAppUrl(
      `/api/v2/analytics/time/daily?species=${encodeURIComponent(scientificName)}&start_date=${start}&end_date=${selectedDate}`
    );
    try {
      const res = await fetch(url, { signal });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const raw: unknown = await res.json();
      const byDate = new Map<string, number>();
      if (Array.isArray(raw)) {
        for (const entry of raw) {
          if (isDailyCount(entry)) byDate.set(entry.date, entry.count);
        }
      }
      // Fill every day in the range (API omits zero days) and sort ascending.
      points = dateRange(startMs, endMs).map(date => ({ date, count: byDate.get(date) ?? 0 }));
      loading = false;
    } catch {
      if (signal.aborted) return;
      error = true;
      loading = false;
    }
  }

  $effect(() => {
    const controller = new AbortController();
    void load(rangeDays, controller.signal);
    return () => controller.abort();
  });

  // ── Derived chart geometry ──
  const totalCount = $derived(points.reduce((sum, p) => sum + p.count, 0));
  const maxCount = $derived(Math.max(1, ...points.map(p => p.count)));

  function movingAverage(values: number[], window: number): number[] {
    return values.map((_, i) => {
      const from = Math.max(0, i - window + 1);
      const slice = values.slice(from, i + 1);
      return slice.reduce((a, b) => a + b, 0) / slice.length;
    });
  }

  const CHART_W = 340;
  const CHART_H = 140;
  const MARGIN = { top: 10, right: 8, bottom: 20, left: 26 };

  const chart = $derived.by(() => {
    const innerW = CHART_W - MARGIN.left - MARGIN.right;
    const innerH = CHART_H - MARGIN.top - MARGIN.bottom;
    const dates = points.map(p => p.date);
    const x = scaleBand<string>().domain(dates).range([0, innerW]).padding(0.15);
    const y = scaleLinear().domain([0, maxCount]).range([innerH, 0]).nice();

    const bars = points.map(p => {
      const bx = x(p.date) ?? 0;
      const by = y(p.count);
      return { key: p.date, x: bx, y: by, w: x.bandwidth(), h: innerH - by };
    });

    const avg = movingAverage(
      points.map(p => p.count),
      AVG_WINDOW
    );
    const avgPoints = points.map((p, i) => ({
      cx: (x(p.date) ?? 0) + x.bandwidth() / 2,
      cy: y(safeArrayAccess(avg, i, 0) ?? 0),
    }));
    const avgPath =
      line<{ cx: number; cy: number }>()
        .x(d => d.cx)
        .y(d => d.cy)
        .curve(curveMonotoneX)(avgPoints) ?? '';

    return { innerW, innerH, bars, avgPath, yTop: Math.round(y.domain()[1] ?? maxCount) };
  });

  // ── Modal interaction ──
  function handleBackdrop(e: MouseEvent) {
    // Only the backdrop itself closes; clicks inside the panel do not.
    if (e.target === e.currentTarget) onClose();
  }

  // Stop touches/clicks from reaching the swipe-to-change-day handler that wraps
  // the species list in DailySummaryCard.
  function stopTouch(e: TouchEvent) {
    e.stopPropagation();
  }
</script>

<svelte:window
  onkeydown={(e: KeyboardEvent) => {
    if (e.key === 'Escape') onClose();
  }}
/>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<div
  class="hist-overlay"
  role="dialog"
  tabindex="-1"
  aria-modal="true"
  aria-label="{displayName} detection history"
  onclick={handleBackdrop}
  ontouchstart={stopTouch}
  ontouchend={stopTouch}
>
  <div class="hist-panel">
    <!-- Header -->
    <div class="hist-header">
      <div class="hist-titles">
        <span class="hist-title">{displayName}</span>
        <span class="hist-subtitle">
          {#if loading}
            Loading history…
          {:else if error}
            History unavailable
          {:else}
            {formatDetectionCount(totalCount)} detections · last {rangeDays}d
          {/if}
        </span>
      </div>
      <button class="hist-close" aria-label="Close history" onclick={onClose}>
        <X class="size-4" />
      </button>
    </div>

    <!-- Range presets -->
    <div class="hist-ranges" role="group" aria-label="History range">
      {#each RANGE_PRESETS as preset (preset)}
        <button
          class="hist-range-btn"
          class:active={rangeDays === preset}
          aria-pressed={rangeDays === preset}
          onclick={() => (rangeDays = preset)}
        >
          {preset}d
        </button>
      {/each}
    </div>

    <!-- Content -->
    <div class="hist-body">
      {#if loading}
        <div class="hist-state" role="status">
          <div class="hist-spinner"></div>
          <span>Loading history…</span>
        </div>
      {:else if error}
        <div class="hist-state" role="alert">Failed to load history</div>
      {:else if totalCount === 0}
        <div class="hist-state">No detections in this period</div>
      {:else}
        <svg
          class="hist-chart"
          viewBox="0 0 {CHART_W} {CHART_H}"
          role="img"
          aria-label="Daily detections for {displayName} over the last {rangeDays} days"
        >
          <g transform="translate({MARGIN.left},{MARGIN.top})">
            <!-- Bars -->
            {#each chart.bars as bar (bar.key)}
              <rect x={bar.x} y={bar.y} width={bar.w} height={bar.h} rx="0.5" class="hist-bar" />
            {/each}
            <!-- 7-day moving average -->
            <path d={chart.avgPath} class="hist-avg" fill="none" />
            <!-- Baseline -->
            <line x1="0" y1={chart.innerH} x2={chart.innerW} y2={chart.innerH} class="hist-axis" />
          </g>
          <!-- Y max label -->
          <text x="0" y={MARGIN.top + 4} class="hist-tick">{chart.yTop}</text>
          <text x="0" y={CHART_H - MARGIN.bottom} class="hist-tick">0</text>
          <!-- X end labels -->
          {#if points.length > 0}
            <text x={MARGIN.left} y={CHART_H - 6} class="hist-tick">
              {shortDate(points[0]?.date ?? '')}
            </text>
            <text x={CHART_W - MARGIN.right} y={CHART_H - 6} class="hist-tick" text-anchor="end">
              {shortDate(points[points.length - 1]?.date ?? '')}
            </text>
          {/if}
        </svg>
        <div class="hist-legend">
          <span class="hist-legend-item"><span class="swatch swatch-bar"></span>daily</span>
          <span class="hist-legend-item"><span class="swatch swatch-avg"></span>7-day avg</span>
        </div>
      {/if}
    </div>
  </div>
</div>

<style>
  .hist-overlay {
    position: fixed;
    inset: 0;
    z-index: 60;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 1rem;
    background: rgb(0 0 0 / 0.5);
  }

  .hist-panel {
    width: 100%;
    max-width: 28rem;
    background: var(--color-base-100);
    border-radius: 0.75rem;
    border: 1px solid color-mix(in srgb, var(--color-base-content) 12%, transparent);
    box-shadow: 0 10px 30px rgb(0 0 0 / 0.35);
    padding: 0.75rem;
    display: flex;
    flex-direction: column;
    gap: 0.625rem;
  }

  .hist-header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 0.5rem;
  }

  .hist-titles {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  .hist-title {
    font-size: 0.9rem;
    font-weight: 700;
    color: var(--color-base-content);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .hist-subtitle {
    font-size: 0.65rem;
    color: color-mix(in srgb, var(--color-base-content) 60%, transparent);
  }

  .hist-close {
    flex-shrink: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    width: 1.75rem;
    height: 1.75rem;
    border-radius: 9999px;
    background: color-mix(in srgb, var(--color-base-content) 10%, transparent);
    color: var(--color-base-content);
    border: none;
    cursor: pointer;
  }

  .hist-close:hover {
    background: color-mix(in srgb, var(--color-base-content) 20%, transparent);
  }

  .hist-close:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 2px;
  }

  .hist-ranges {
    display: flex;
    gap: 0.375rem;
  }

  .hist-range-btn {
    flex: 1;
    font-size: 0.7rem;
    font-weight: 600;
    padding: 0.25rem 0;
    border-radius: 0.375rem;
    border: 1px solid color-mix(in srgb, var(--color-base-content) 20%, transparent);
    background: none;
    color: var(--color-base-content);
    cursor: pointer;
  }

  .hist-range-btn.active {
    background: var(--color-primary);
    border-color: var(--color-primary);
    color: var(--color-primary-content, white);
  }

  .hist-range-btn:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 2px;
  }

  .hist-body {
    min-height: 9rem;
    display: flex;
    flex-direction: column;
    justify-content: center;
  }

  .hist-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 0.5rem;
    padding: 2rem 0;
    font-size: 0.75rem;
    color: color-mix(in srgb, var(--color-base-content) 60%, transparent);
  }

  .hist-spinner {
    width: 1.25rem;
    height: 1.25rem;
    border: 2px solid var(--color-primary);
    border-top-color: transparent;
    border-radius: 9999px;
    animation: hist-spin 0.7s linear infinite;
  }

  @keyframes hist-spin {
    to {
      transform: rotate(360deg);
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .hist-spinner {
      animation-duration: 1.5s;
    }
  }

  .hist-chart {
    width: 100%;
    height: auto;
    display: block;
  }

  .hist-bar {
    fill: var(--color-primary);
  }

  .hist-avg {
    stroke: var(--color-info);
    stroke-width: 1.5;
  }

  .hist-axis {
    stroke: color-mix(in srgb, var(--color-base-content) 20%, transparent);
    stroke-width: 1;
  }

  .hist-tick {
    font-size: 0.5rem;
    fill: color-mix(in srgb, var(--color-base-content) 50%, transparent);
  }

  .hist-legend {
    display: flex;
    justify-content: center;
    gap: 0.75rem;
    margin-top: 0.375rem;
  }

  .hist-legend-item {
    display: inline-flex;
    align-items: center;
    gap: 0.25rem;
    font-size: 0.6rem;
    color: color-mix(in srgb, var(--color-base-content) 60%, transparent);
  }

  .swatch {
    width: 0.625rem;
    height: 0.625rem;
    border-radius: 0.125rem;
    display: inline-block;
  }

  .swatch-bar {
    background: var(--color-primary);
  }

  .swatch-avg {
    background: var(--color-info);
  }
</style>
