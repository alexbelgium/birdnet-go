<script module lang="ts">
  // Session-scoped cache of full detection histories so re-opening the modal
  // for a species (or switching ranges) never refetches.
  interface FullSeries {
    byDate: Map<string, number>;
    firstMs: number | null;
  }

  const historyCache = new Map<string, FullSeries>();
  const HISTORY_CACHE_MAX = 20;
</script>

<script lang="ts">
  // Self-contained species detection-history modal, opened from the daily
  // summary detail card on both mobile and desktop.
  // Charts detection counts for one species over 7d/30d/90d/1y/2y/all-time
  // using the existing GET /api/v2/analytics/time/daily endpoint (which wraps
  // the series in {data: [...]}). Long ranges are aggregated client-side into
  // week/month buckets so the chart stays readable. All strings are hardcoded
  // English (no i18n) so the feature stays self-contained.
  import { line as d3Line, curveMonotoneX } from 'd3-shape';
  import { X } from '@lucide/svelte';
  import { buildAppUrl } from '$lib/utils/urlHelpers';
  import { formatDetectionCount } from '../../utils/dailySummaryStats';
  import {
    RANGE_PRESETS,
    type Bucket,
    type BucketPoint,
    type DailyCount,
    type RangeKey,
    axisLabel,
    bucketFor,
    bucketLabel,
    bucketize,
    buildXTickIndices,
    buildYTicks,
    formatYmd,
    movingAverage,
    parseDailyResponse,
    parseYmd,
    rangeWindow,
    selectPeakIndices,
    shortDayLabel,
    topRoundedBarPath,
  } from '../../utils/speciesHistory';

  interface Props {
    scientificName: string;
    displayName: string;
    selectedDate: string; // YYYY-MM-DD (range end)
    onClose: () => void;
  }

  let { scientificName, displayName, selectedDate, onClose }: Props = $props();

  const DEFAULT_RANGE: RangeKey = '30d';
  const ALL_TIME_START = '2000-01-01'; // pre-dates any BirdNET-Go install
  const AVG_WINDOW = 7;
  const AVG_MIN_POINTS = 14; // moving average is noise below this many days
  const CHART_MARGIN = { top: 26, right: 8, bottom: 22, left: 34 } as const;
  const BAR_MAX_W = 24;
  const BAR_RADIUS = 4;
  const TICK_CLAMP = 20; // keeps edge x-labels inside the plot
  const TOOLTIP_CLAMP = 64;

  const RANGE_LABELS: Record<RangeKey, string> = {
    '7d': 'last 7 days',
    '30d': 'last 30 days',
    '90d': 'last 90 days',
    '1y': 'last 12 months',
    '2y': 'last 2 years',
    all: 'all time',
  };

  // Deterministic skeleton bar heights (percent) shown before first data.
  const SKELETON_HEIGHTS = [35, 55, 40, 70, 50, 82, 45, 62, 38, 68, 52, 74, 44, 58];

  let range = $state<RangeKey>(DEFAULT_RANGE);
  let fullData = $state<FullSeries | null>(null);
  let fullError = $state(false);
  let partial = $state<{ range: RangeKey; byDate: Map<string, number> } | null>(null);
  let rangeError = $state(false);
  let retryToken = $state(0);
  let hoverIdx = $state<number | null>(null);
  let chartW = $state(0);
  let chartH = $state(0);

  // ── Data loading ──

  async function fetchSeries(
    startYmd: string,
    endYmd: string,
    signal: AbortSignal
  ): Promise<DailyCount[]> {
    const url = buildAppUrl(
      `/api/v2/analytics/time/daily?species=${encodeURIComponent(scientificName)}&start_date=${startYmd}&end_date=${endYmd}`
    );
    const res = await fetch(url, { signal });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    return parseDailyResponse(await res.json());
  }

  function toByDate(list: DailyCount[]): Map<string, number> {
    const out = new Map<string, number>();
    for (const entry of list) out.set(entry.date, entry.count);
    return out;
  }

  // Full-history fetch: one request serves every range once it lands, making
  // later range switches instant. Cached across modal opens.
  $effect(() => {
    void retryToken;
    const key = `${scientificName}|${selectedDate}`;
    fullError = false;
    const cached = historyCache.get(key);
    if (cached) {
      fullData = cached;
      return;
    }
    fullData = null;
    const controller = new AbortController();
    fetchSeries(ALL_TIME_START, selectedDate, controller.signal)
      .then(list => {
        let firstMs: number | null = null;
        for (const entry of list) {
          if (entry.count > 0) {
            const ms = parseYmd(entry.date);
            if (firstMs === null || ms < firstMs) firstMs = ms;
          }
        }
        const series: FullSeries = { byDate: toByDate(list), firstMs };
        historyCache.set(key, series);
        if (historyCache.size > HISTORY_CACHE_MAX) {
          const oldest = historyCache.keys().next().value;
          if (oldest !== undefined) historyCache.delete(oldest);
        }
        fullData = series;
      })
      .catch(() => {
        if (!controller.signal.aborted) fullError = true;
      });
    return () => controller.abort();
  });

  // Fast first paint: fetch just the selected range while the full history is
  // still in flight. Skipped (and aborted) once fullData is available.
  $effect(() => {
    void retryToken;
    const r = range;
    if (fullData !== null) return;
    if (r === 'all') return; // supplied by the full-history fetch
    if (partial?.range === r) return;
    rangeError = false;
    const win = rangeWindow(r, parseYmd(selectedDate), null);
    if (!win) return;
    const controller = new AbortController();
    fetchSeries(formatYmd(win.startMs), selectedDate, controller.signal)
      .then(list => {
        partial = { range: r, byDate: toByDate(list) };
      })
      .catch(() => {
        if (!controller.signal.aborted) rangeError = true;
      });
    return () => controller.abort();
  });

  // ── Derived chart data ──

  const endMs = $derived(parseYmd(selectedDate));

  const source = $derived.by((): FullSeries | null => {
    if (fullData) return fullData;
    if (partial && partial.range === range) return { byDate: partial.byDate, firstMs: null };
    return null;
  });

  interface ChartView {
    range: RangeKey;
    bucket: Bucket;
    buckets: BucketPoint[];
    counts: number[];
    total: number;
    max: number;
    peakIdx: number;
    avgPerDay: number;
    showAvg: boolean;
    avg: number[];
    empty: boolean;
  }

  const view = $derived.by((): ChartView | null => {
    const src = source;
    if (!src) return null;
    const win = rangeWindow(range, endMs, src.firstMs);
    const spanDays = win ? Math.round((win.endMs - win.startMs) / 86_400_000) + 1 : 0;
    const bucket = win ? bucketFor(range, spanDays) : 'day';
    const buckets = win ? bucketize(src.byDate, win.startMs, win.endMs, bucket) : [];
    const counts = buckets.map(b => b.count);
    const total = counts.reduce((a, b) => a + b, 0);
    const max = counts.length > 0 ? Math.max(...counts) : 0;
    const showAvg = bucket === 'day' && counts.length >= AVG_MIN_POINTS && max > 0;
    return {
      range,
      bucket,
      buckets,
      counts,
      total,
      max,
      peakIdx: counts.indexOf(max),
      avgPerDay: spanDays > 0 ? total / spanDays : 0,
      showAvg,
      avg: showAvg ? movingAverage(counts, AVG_WINDOW) : [],
      empty: total === 0,
    };
  });

  // Last successfully computed view: held on screen (dimmed) while a range
  // switch is still waiting on data, so the frame never flashes empty.
  let displayed = $state<ChartView | null>(null);
  $effect(() => {
    const v = view;
    if (v) displayed = v;
  });

  const showError = $derived(source === null && (range === 'all' ? fullError : rangeError));
  const showSkeleton = $derived(displayed === null && !showError);
  const refreshing = $derived(view === null && displayed !== null && !showError);

  const geom = $derived.by(() => {
    const v = displayed;
    if (!v || v.empty || chartW < 80 || chartH < 80) return null;
    const innerW = chartW - CHART_MARGIN.left - CHART_MARGIN.right;
    const innerH = chartH - CHART_MARGIN.top - CHART_MARGIN.bottom;
    const n = v.buckets.length;
    if (n === 0 || innerW <= 0 || innerH <= 0) return null;
    const step = innerW / n;
    const gap = Math.min(2, step * 0.2);
    const barW = Math.min(BAR_MAX_W, Math.max(1, step - gap));
    const yTicks = buildYTicks(v.max);
    const yMax = yTicks[yTicks.length - 1] ?? 1;
    const yOf = (val: number): number => innerH - (val / yMax) * innerH;
    const centerOf = (i: number): number => i * step + step / 2;
    const bars = v.buckets.map((b, i) => ({
      key: b.key,
      path:
        b.count > 0
          ? topRoundedBarPath(
              centerOf(i) - barW / 2,
              yOf(b.count),
              barW,
              innerH - yOf(b.count),
              BAR_RADIUS
            )
          : '',
    }));
    const avgPath = v.showAvg
      ? (d3Line<number>()
          .x((_, i) => centerOf(i))
          .y(d => yOf(d))
          .curve(curveMonotoneX)(v.avg) ?? '')
      : '';
    const xTicks = buildXTickIndices(n).flatMap(i => {
      const bp = v.buckets.at(i);
      if (!bp) return [];
      const x = Math.min(Math.max(centerOf(i), TICK_CLAMP), innerW - TICK_CLAMP);
      return [{ x, label: axisLabel(bp, v.bucket) }];
    });
    const peaks = selectPeakIndices(v.counts).flatMap(i => {
      const count = v.counts.at(i);
      if (count === undefined) return [];
      const x = Math.min(Math.max(centerOf(i), 10), innerW - 10);
      return [{ x, y: yOf(count), value: count }];
    });
    return { innerW, innerH, step, yTicks, yMax, yOf, centerOf, bars, avgPath, xTicks, peaks, n };
  });

  const tooltip = $derived.by(() => {
    const g = geom;
    const v = displayed;
    if (g === null || v === null || hoverIdx === null) return null;
    const bp = v.buckets.at(hoverIdx);
    const count = v.counts.at(hoverIdx);
    if (!bp || count === undefined) return null;
    const cx = CHART_MARGIN.left + g.centerOf(hoverIdx);
    return {
      hairlineX: g.centerOf(hoverIdx),
      left: Math.min(Math.max(cx, TOOLTIP_CLAMP), chartW - TOOLTIP_CLAMP),
      value: count,
      label: bucketLabel(bp, v.bucket),
    };
  });

  const peakSubLabel = $derived.by(() => {
    const v = displayed;
    if (!v || v.peakIdx < 0) return '';
    const bp = v.buckets.at(v.peakIdx);
    return bp ? axisLabel(bp, v.bucket) : '';
  });

  const subtitle = $derived.by(() => {
    if (showError) return 'History unavailable';
    const v = displayed;
    if (!v) return 'Loading history…';
    let text = `${formatDetectionCount(v.total)} detections · ${RANGE_LABELS[v.range]}`;
    if (v.range === 'all' && fullData?.firstMs != null) {
      const firstYear = new Date(fullData.firstMs).getUTCFullYear();
      text += ` (since ${shortDayLabel(fullData.firstMs)}, ${firstYear})`;
    }
    return text;
  });

  function formatAvg(avg: number): string {
    if (avg === 0) return '0';
    if (avg < 0.1) return '<0.1';
    if (avg < 10) return avg.toFixed(1);
    return formatDetectionCount(Math.round(avg));
  }

  // ── Chart scrubbing (pointer events cover mouse + touch) ──

  function hoverFromEvent(e: globalThis.PointerEvent): void {
    const g = geom;
    if (!g) return;
    const rect = (e.currentTarget as SVGSVGElement).getBoundingClientRect();
    const px = e.clientX - rect.left - CHART_MARGIN.left;
    const idx = Math.floor(px / g.step);
    hoverIdx = Math.min(Math.max(idx, 0), g.n - 1);
  }

  function clearHover(e: globalThis.PointerEvent): void {
    // A tap leaves its readout visible; mouse hover clears on leave.
    if (e.pointerType !== 'touch') hoverIdx = null;
  }

  // ── Modal interaction ──

  function handleBackdrop(e: MouseEvent): void {
    // Only the backdrop itself closes; clicks inside the panel do not.
    if (e.target === e.currentTarget) onClose();
  }

  // Stop touches/clicks from reaching the swipe-to-change-day handler that wraps
  // the species list in DailySummaryCard.
  function stopTouch(e: TouchEvent): void {
    e.stopPropagation();
  }

  function selectRange(preset: RangeKey): void {
    hoverIdx = null;
    range = preset;
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
        <span class="hist-subtitle">{subtitle}</span>
      </div>
      <button class="hist-close" aria-label="Close history" onclick={onClose}>
        <X class="size-4" />
      </button>
    </div>

    <!-- Stats strip -->
    {#if displayed && !displayed.empty}
      <div class="hist-stats" class:stale={refreshing}>
        <div class="hist-stat">
          <span class="hist-stat-label">Total</span>
          <span class="hist-stat-value">{formatDetectionCount(displayed.total)}</span>
        </div>
        <div class="hist-stat">
          <span class="hist-stat-label">Peak</span>
          <span class="hist-stat-value">
            {formatDetectionCount(displayed.max)}
            {#if peakSubLabel}<span class="hist-stat-sub">{peakSubLabel}</span>{/if}
          </span>
        </div>
        <div class="hist-stat">
          <span class="hist-stat-label">Avg/day</span>
          <span class="hist-stat-value">{formatAvg(displayed.avgPerDay)}</span>
        </div>
      </div>
    {/if}

    <!-- Chart -->
    <div class="hist-body">
      {#if showSkeleton}
        <div class="hist-skeleton" role="status" aria-label="Loading detection history">
          {#each SKELETON_HEIGHTS as h, i (i)}
            <div class="hist-skel-bar" style:height="{h}%"></div>
          {/each}
        </div>
      {:else if showError}
        <div class="hist-state" role="alert">
          <span>Failed to load history</span>
          <button class="hist-retry" onclick={() => (retryToken += 1)}>Retry</button>
        </div>
      {:else if displayed?.empty}
        <div class="hist-state">
          <span>No detections in this period</span>
          {#if displayed.range !== 'all'}
            <span class="hist-state-hint">Try a longer range</span>
          {/if}
        </div>
      {:else if displayed}
        <div
          class="hist-chart-wrap"
          class:stale={refreshing}
          bind:clientWidth={chartW}
          bind:clientHeight={chartH}
        >
          {#if geom}
            {@const g = geom}
            <svg
              class="hist-chart"
              width={chartW}
              height={chartH}
              viewBox="0 0 {chartW} {chartH}"
              role="img"
              aria-label="{displayName}: {displayed.total} detections over {RANGE_LABELS[
                displayed.range
              ]}, peaking at {displayed.max}"
              onpointerdown={hoverFromEvent}
              onpointermove={hoverFromEvent}
              onpointerleave={clearHover}
              onpointercancel={clearHover}
            >
              <g transform="translate({CHART_MARGIN.left},{CHART_MARGIN.top})">
                <!-- Gridlines + y ticks -->
                {#each g.yTicks as tick (tick)}
                  <line x1="0" y1={g.yOf(tick)} x2={g.innerW} y2={g.yOf(tick)} class="hist-grid" />
                  <text x="-6" y={g.yOf(tick) + 3} class="hist-tick" text-anchor="end">
                    {formatDetectionCount(tick)}
                  </text>
                {/each}
                <!-- Scrub hairline -->
                {#if tooltip}
                  <line
                    x1={tooltip.hairlineX}
                    y1="-4"
                    x2={tooltip.hairlineX}
                    y2={g.innerH}
                    class="hist-hairline"
                  />
                {/if}
                <!-- Bars -->
                {#each g.bars as bar, i (bar.key)}
                  {#if bar.path}
                    <path d={bar.path} class="hist-bar" class:hovered={hoverIdx === i} />
                  {/if}
                {/each}
                <!-- 7-day moving average -->
                {#if g.avgPath}
                  <path d={g.avgPath} class="hist-avg" fill="none" />
                {/if}
                <!-- Peak value labels -->
                {#each g.peaks as peak (peak.x)}
                  <text x={peak.x} y={peak.y - 5} class="hist-peak" text-anchor="middle">
                    {formatDetectionCount(peak.value)}
                  </text>
                {/each}
                <!-- Baseline -->
                <line x1="0" y1={g.innerH} x2={g.innerW} y2={g.innerH} class="hist-axis" />
                <!-- X ticks -->
                {#each g.xTicks as tick (tick.x)}
                  <text x={tick.x} y={g.innerH + 15} class="hist-tick" text-anchor="middle">
                    {tick.label}
                  </text>
                {/each}
              </g>
            </svg>
            {#if tooltip}
              <div class="hist-tooltip" style:left="{tooltip.left}px" aria-hidden="true">
                <span class="hist-tooltip-value">{tooltip.value}</span>
                <span class="hist-tooltip-label">{tooltip.label}</span>
              </div>
            {/if}
          {/if}
          {#if refreshing}
            <div class="hist-refresh-pill" role="status">Loading…</div>
          {/if}
        </div>
        {#if displayed.showAvg}
          <div class="hist-legend">
            <span class="hist-legend-item"><span class="hist-swatch-bar"></span>daily</span>
            <span class="hist-legend-item"><span class="hist-swatch-avg"></span>7-day avg</span>
          </div>
        {/if}
      {/if}
    </div>

    <!-- Bottom range selector -->
    <div class="hist-ranges" role="group" aria-label="History range">
      {#each RANGE_PRESETS as preset (preset.key)}
        <button
          class="hist-range-btn"
          class:active={range === preset.key}
          aria-pressed={range === preset.key}
          onclick={() => selectRange(preset.key)}
        >
          {preset.label}
        </button>
      {/each}
    </div>
  </div>
</div>

<style>
  .hist-overlay {
    position: fixed;
    inset: 0;
    z-index: 60;
    display: flex;
    padding: 1rem;
    background: rgb(0 0 0 / 0.5);

    /* Scrolls when the panel is taller than the viewport (landscape phones). */
    overflow-y: auto;
  }

  .hist-panel {
    /* margin auto centers in the flex overlay yet keeps the top reachable
       when the panel overflows a short viewport. */
    margin: auto;
    width: 100%;
    max-width: 32rem;
    background: var(--color-base-100);
    border-radius: 1rem;
    border: 1px solid color-mix(in srgb, var(--color-base-content) 12%, transparent);
    box-shadow: 0 10px 30px rgb(0 0 0 / 0.35);
    padding: 0.875rem;
    display: flex;
    flex-direction: column;
    gap: 0.625rem;
  }

  /* Desktop gets a roomier chart */
  @media (min-width: 1024px) {
    .hist-panel {
      max-width: 40rem;
    }
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
    font-size: 0.95rem;
    font-weight: 700;
    color: var(--color-base-content);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .hist-subtitle {
    font-size: 0.68rem;
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

  /* ── Stats strip ── */

  .hist-stats {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 0.375rem;
    transition: opacity 0.15s ease;
  }

  .hist-stat {
    display: flex;
    flex-direction: column;
    gap: 0.125rem;
    padding: 0.375rem 0.5rem;
    border-radius: 0.5rem;
    background: color-mix(in srgb, var(--color-base-content) 5%, transparent);
    min-width: 0;
  }

  .hist-stat-label {
    font-size: 0.6rem;
    text-transform: uppercase;
    letter-spacing: 0.03em;
    color: color-mix(in srgb, var(--color-base-content) 55%, transparent);
  }

  .hist-stat-value {
    font-size: 0.9rem;
    font-weight: 600;
    color: var(--color-base-content);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .hist-stat-sub {
    font-size: 0.62rem;
    font-weight: 500;
    color: color-mix(in srgb, var(--color-base-content) 55%, transparent);
    margin-left: 0.125rem;
  }

  /* ── Chart area ── */

  .hist-body {
    display: flex;
    flex-direction: column;
    justify-content: center;
    min-height: 11.5rem;
  }

  .hist-chart-wrap {
    position: relative;
    width: 100%;

    /* Scales with width on phones but yields to short landscape viewports. */
    height: clamp(150px, min(48vw, 34vh), 240px);
    transition: opacity 0.15s ease;
  }

  .hist-chart-wrap.stale,
  .hist-stats.stale {
    opacity: 0.45;
  }

  .hist-chart {
    display: block;
    touch-action: none;
    cursor: crosshair;
  }

  .hist-bar {
    fill: var(--color-primary);
  }

  .hist-bar.hovered {
    fill: color-mix(in srgb, var(--color-primary) 75%, white);
  }

  .hist-avg {
    stroke: var(--color-info);
    stroke-width: 2;
    stroke-linejoin: round;
    stroke-linecap: round;
  }

  .hist-grid {
    stroke: color-mix(in srgb, var(--color-base-content) 8%, transparent);
    stroke-width: 1;
  }

  .hist-axis {
    stroke: color-mix(in srgb, var(--color-base-content) 20%, transparent);
    stroke-width: 1;
  }

  .hist-hairline {
    stroke: color-mix(in srgb, var(--color-base-content) 30%, transparent);
    stroke-width: 1;
  }

  .hist-tick {
    font-size: 0.6rem;
    font-variant-numeric: tabular-nums;
    fill: color-mix(in srgb, var(--color-base-content) 55%, transparent);
  }

  .hist-peak {
    font-size: 0.62rem;
    font-weight: 600;
    font-variant-numeric: tabular-nums;
    fill: color-mix(in srgb, var(--color-base-content) 75%, transparent);
  }

  /* Scrub readout: pinned above the plot so a finger never covers it. */
  .hist-tooltip {
    position: absolute;
    top: 0;
    transform: translateX(-50%);
    display: flex;
    align-items: baseline;
    gap: 0.375rem;
    padding: 0.125rem 0.5rem;
    border-radius: 9999px;
    background: var(--color-base-100);
    border: 1px solid color-mix(in srgb, var(--color-base-content) 15%, transparent);
    box-shadow: 0 1px 4px rgb(0 0 0 / 0.15);
    pointer-events: none;
    white-space: nowrap;
  }

  .hist-tooltip-value {
    font-size: 0.75rem;
    font-weight: 700;
    font-variant-numeric: tabular-nums;
    color: var(--color-base-content);
  }

  .hist-tooltip-label {
    font-size: 0.65rem;
    color: color-mix(in srgb, var(--color-base-content) 60%, transparent);
  }

  .hist-refresh-pill {
    position: absolute;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    padding: 0.25rem 0.75rem;
    border-radius: 9999px;
    background: color-mix(in srgb, var(--color-base-100) 90%, transparent);
    border: 1px solid color-mix(in srgb, var(--color-base-content) 15%, transparent);
    font-size: 0.7rem;
    color: color-mix(in srgb, var(--color-base-content) 70%, transparent);
  }

  /* ── Skeleton / empty / error states ── */

  .hist-skeleton {
    display: flex;
    align-items: flex-end;
    gap: 0.375rem;
    height: clamp(150px, min(48vw, 34vh), 240px);
    padding: 1.5rem 0.5rem 1.375rem;
    animation: hist-pulse 1.4s ease-in-out infinite;
  }

  .hist-skel-bar {
    flex: 1;
    border-radius: 3px 3px 0 0;
    background: color-mix(in srgb, var(--color-base-content) 10%, transparent);
  }

  @keyframes hist-pulse {
    0%,
    100% {
      opacity: 1;
    }

    50% {
      opacity: 0.45;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .hist-skeleton {
      animation: none;
    }
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

  .hist-state-hint {
    font-size: 0.65rem;
    color: color-mix(in srgb, var(--color-base-content) 45%, transparent);
  }

  .hist-retry {
    padding: 0.25rem 0.875rem;
    border-radius: 0.375rem;
    border: 1px solid color-mix(in srgb, var(--color-base-content) 20%, transparent);
    background: none;
    color: var(--color-base-content);
    font-size: 0.7rem;
    font-weight: 600;
    cursor: pointer;
  }

  .hist-retry:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 2px;
  }

  /* ── Legend ── */

  .hist-legend {
    display: flex;
    justify-content: center;
    gap: 0.75rem;
    margin-top: 0.25rem;
  }

  .hist-legend-item {
    display: inline-flex;
    align-items: center;
    gap: 0.25rem;
    font-size: 0.6rem;
    color: color-mix(in srgb, var(--color-base-content) 60%, transparent);
  }

  .hist-swatch-bar {
    width: 0.625rem;
    height: 0.625rem;
    border-radius: 0.125rem;
    display: inline-block;
    background: var(--color-primary);
  }

  .hist-swatch-avg {
    width: 0.75rem;
    height: 2px;
    border-radius: 1px;
    display: inline-block;
    background: var(--color-info);
  }

  /* ── Bottom range selector ── */

  .hist-ranges {
    display: flex;
    gap: 0.25rem;
    padding: 0.1875rem;
    border-radius: 0.625rem;
    background: color-mix(in srgb, var(--color-base-content) 6%, transparent);
  }

  .hist-range-btn {
    flex: 1;
    font-size: 0.72rem;
    font-weight: 600;
    padding: 0.375rem 0;
    border-radius: 0.4375rem;
    border: none;
    background: none;
    color: color-mix(in srgb, var(--color-base-content) 70%, transparent);
    cursor: pointer;
  }

  .hist-range-btn.active {
    background: var(--color-primary);
    color: var(--color-primary-content, white);
    box-shadow: 0 1px 2px rgb(0 0 0 / 0.15);
  }

  .hist-range-btn:focus-visible {
    outline: 2px solid var(--color-primary);
    outline-offset: 2px;
  }
</style>
