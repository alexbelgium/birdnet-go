/**
 * Pure helpers for the mobile species detection-history modal.
 *
 * Data source is GET /api/v2/analytics/time/daily which returns a wrapper
 * object: { start_date, end_date, species, data: [{date, count}...], total }.
 * All date math is UTC-based on "YYYY-MM-DD" strings (never toISOString).
 * Strings are hardcoded English so the mobile feature stays self-contained.
 */

export const DAY_MS = 86_400_000;

/** One day of detection counts, date as "YYYY-MM-DD". */
export interface DailyCount {
  date: string;
  count: number;
}

/** Aggregation granularity used to keep long ranges readable. */
export type Bucket = 'day' | 'week' | 'month';

/** Range presets shown in the modal's bottom selector. */
export type RangeKey = '7d' | '30d' | '90d' | '1y' | '2y' | 'all';

export const RANGE_PRESETS: { key: RangeKey; label: string }[] = [
  { key: '7d', label: '7d' },
  { key: '30d', label: '30d' },
  { key: '90d', label: '90d' },
  { key: '1y', label: '1y' },
  { key: '2y', label: '2y' },
  { key: 'all', label: 'All' },
];

/** Days covered by each fixed-width range preset. */
const RANGE_DAYS = new Map<RangeKey, number>([
  ['7d', 7],
  ['30d', 30],
  ['90d', 90],
  ['1y', 365],
  ['2y', 730],
]);

/** One aggregated chart bar. endMs is the inclusive last day of the bucket. */
export interface BucketPoint {
  key: string;
  startMs: number;
  endMs: number;
  count: number;
}

const MONTHS_SHORT = [
  'Jan',
  'Feb',
  'Mar',
  'Apr',
  'May',
  'Jun',
  'Jul',
  'Aug',
  'Sep',
  'Oct',
  'Nov',
  'Dec',
] as const;

// ── Date helpers ──

/** Parses "YYYY-MM-DD" to UTC milliseconds. */
export function parseYmd(s: string): number {
  const parts = s.split('-').map(Number);
  return Date.UTC(parts.at(0) ?? 1970, (parts.at(1) ?? 1) - 1, parts.at(2) ?? 1);
}

/** Formats UTC milliseconds as "YYYY-MM-DD". */
export function formatYmd(utcMs: number): string {
  const dt = new Date(utcMs);
  const y = dt.getUTCFullYear();
  const m = String(dt.getUTCMonth() + 1).padStart(2, '0');
  const d = String(dt.getUTCDate()).padStart(2, '0');
  return `${y}-${m}-${d}`;
}

function monthShort(utcMs: number): string {
  return MONTHS_SHORT[new Date(utcMs).getUTCMonth()] ?? '';
}

/** "Jun 12" style label for a single day. */
export function shortDayLabel(utcMs: number): string {
  const dt = new Date(utcMs);
  return `${monthShort(utcMs)} ${dt.getUTCDate()}`;
}

// ── API response parsing ──

function isDailyCount(v: unknown): v is DailyCount {
  if (typeof v !== 'object' || v === null) return false;
  const o = v as Record<string, unknown>;
  return typeof o.date === 'string' && typeof o.count === 'number' && Number.isFinite(o.count);
}

/**
 * Extracts the daily counts from the /analytics/time/daily response.
 * The endpoint wraps the series in a `data` array; a bare array is accepted
 * too for robustness. Malformed entries are dropped.
 */
export function parseDailyResponse(raw: unknown): DailyCount[] {
  let list: unknown;
  if (Array.isArray(raw)) {
    list = raw;
  } else if (typeof raw === 'object' && raw !== null) {
    list = (raw as Record<string, unknown>).data;
  }
  if (!Array.isArray(list)) return [];
  return list.filter(isDailyCount).map(e => ({ date: e.date, count: Math.max(0, e.count) }));
}

// ── Range → concrete date window ──

/**
 * Computes the inclusive [startMs, endMs] window for a range preset ending at
 * endMs. For 'all', the window starts at the first detection; null means the
 * species has no detections at all (nothing to chart).
 */
export function rangeWindow(
  range: RangeKey,
  endMs: number,
  firstDetectionMs: number | null
): { startMs: number; endMs: number } | null {
  if (range === 'all') {
    if (firstDetectionMs === null || firstDetectionMs > endMs) return null;
    return { startMs: firstDetectionMs, endMs };
  }
  const days = RANGE_DAYS.get(range) ?? 30;
  return { startMs: endMs - (days - 1) * DAY_MS, endMs };
}

/** Bucket granularity per range: daily up to 90d, weekly for 1-2y, adaptive for all. */
export function bucketFor(range: RangeKey, spanDays: number): Bucket {
  switch (range) {
    case '7d':
    case '30d':
    case '90d':
      return 'day';
    case '1y':
    case '2y':
      return 'week';
    case 'all':
      if (spanDays <= 90) return 'day';
      if (spanDays <= 400) return 'week';
      return 'month';
  }
}

// ── Aggregation ──

/** First day of the UTC month after utcMs. */
function nextMonthStart(utcMs: number): number {
  const dt = new Date(utcMs);
  return Date.UTC(dt.getUTCFullYear(), dt.getUTCMonth() + 1, 1);
}

/**
 * Aggregates a sparse byDate map into contiguous zero-filled buckets covering
 * [startMs, endMs]. Day buckets are single days; week buckets are 7-day chunks
 * anchored at startMs (last one may be shorter); month buckets follow calendar
 * months (first/last may be partial).
 */
export function bucketize(
  byDate: Map<string, number>,
  startMs: number,
  endMs: number,
  bucket: Bucket
): BucketPoint[] {
  if (endMs < startMs) return [];
  const out: BucketPoint[] = [];
  let cursor = startMs;
  while (cursor <= endMs) {
    let bucketEnd: number;
    if (bucket === 'day') {
      bucketEnd = cursor;
    } else if (bucket === 'week') {
      bucketEnd = Math.min(cursor + 6 * DAY_MS, endMs);
    } else {
      bucketEnd = Math.min(nextMonthStart(cursor) - DAY_MS, endMs);
    }
    let count = 0;
    for (let day = cursor; day <= bucketEnd; day += DAY_MS) {
      count += byDate.get(formatYmd(day)) ?? 0;
    }
    out.push({ key: formatYmd(cursor), startMs: cursor, endMs: bucketEnd, count });
    cursor = bucketEnd + DAY_MS;
  }
  return out;
}

/** Trailing moving average over `window` values (partial windows at the start). */
export function movingAverage(values: number[], window: number): number[] {
  const out: number[] = [];
  let sum = 0;
  for (let i = 0; i < values.length; i++) {
    sum += values.at(i) ?? 0;
    if (i >= window) sum -= values.at(i - window) ?? 0;
    out.push(sum / Math.min(i + 1, window));
  }
  return out;
}

// ── Labels ──

/** Tooltip / stats label for one bucket, e.g. "Jun 12, 2026" or "Jun 12 – 18, 2026". */
export function bucketLabel(bp: BucketPoint, bucket: Bucket): string {
  const start = new Date(bp.startMs);
  const year = start.getUTCFullYear();
  if (bucket === 'day') {
    return `${shortDayLabel(bp.startMs)}, ${year}`;
  }
  if (bucket === 'month') {
    return `${monthShort(bp.startMs)} ${year}`;
  }
  const end = new Date(bp.endMs);
  const sameMonth =
    start.getUTCMonth() === end.getUTCMonth() && start.getUTCFullYear() === end.getUTCFullYear();
  const endLabel = sameMonth ? `${end.getUTCDate()}` : shortDayLabel(bp.endMs);
  return `${shortDayLabel(bp.startMs)} – ${endLabel}, ${end.getUTCFullYear()}`;
}

/** Compact x-axis label for one bucket, e.g. "Jun 12" or "Jun '26". */
export function axisLabel(bp: BucketPoint, bucket: Bucket): string {
  if (bucket === 'month') {
    const yy = String(new Date(bp.startMs).getUTCFullYear() % 100).padStart(2, '0');
    return `${monthShort(bp.startMs)} '${yy}`;
  }
  return shortDayLabel(bp.startMs);
}

/**
 * Picks up to maxTicks evenly spaced bucket indices for the x axis, always
 * including the first and last bucket.
 */
export function buildXTickIndices(bucketCount: number, maxTicks = 5): number[] {
  if (bucketCount <= 0) return [];
  const tickCount = Math.min(maxTicks, bucketCount);
  if (tickCount === 1) return [0];
  const out: number[] = [];
  for (let i = 0; i < tickCount; i++) {
    const idx = Math.round((i * (bucketCount - 1)) / (tickCount - 1));
    if (out[out.length - 1] !== idx) out.push(idx);
  }
  return out;
}

/**
 * Y-axis ticks [0, mid, max] with a clean even maximum (2/4/6/8/10 ladder per
 * decade) so the midpoint is always an integer.
 */
export function buildYTicks(maxCount: number): number[] {
  if (maxCount <= 1) return [0, 1];
  if (maxCount <= 2) return [0, 1, 2];
  const ladder = [2, 4, 6, 8, 10];
  let magnitude = 1;
  for (let guard = 0; guard < 12; guard++) {
    for (const step of ladder) {
      const candidate = step * magnitude;
      if (candidate >= maxCount) return [0, candidate / 2, candidate];
    }
    magnitude *= 10;
  }
  return [0, maxCount];
}

/**
 * Selects bucket indices whose counts get a direct label: the global maximum
 * plus significant local maxima (≥ 30% of the max), greedily keeping a minimum
 * index gap so labels never collide. Highest counts win ties.
 */
export function selectPeakIndices(counts: number[], maxLabels = 4): number[] {
  const n = counts.length;
  if (n === 0) return [];
  const max = Math.max(...counts);
  if (max <= 0) return [];
  const minGap = Math.max(1, Math.ceil(n * 0.12));
  const isLocalMax = (i: number): boolean => {
    const c = counts.at(i) ?? 0;
    return c > 0 && c >= (i > 0 ? (counts.at(i - 1) ?? 0) : 0) && c >= (counts.at(i + 1) ?? 0);
  };
  const candidates: number[] = [];
  for (let i = 0; i < n; i++) {
    if (isLocalMax(i) && (counts.at(i) ?? 0) >= max * 0.3) candidates.push(i);
  }
  candidates.sort((a, b) => (counts.at(b) ?? 0) - (counts.at(a) ?? 0) || a - b);
  const kept: number[] = [];
  for (const idx of candidates) {
    if (kept.length >= maxLabels) break;
    if (kept.every(k => Math.abs(k - idx) >= minGap)) kept.push(idx);
  }
  return kept.sort((a, b) => a - b);
}

/**
 * SVG path for a bar with a rounded data-end (top) and a square baseline,
 * per chart mark spec. Radius shrinks on very thin or very short bars.
 */
export function topRoundedBarPath(x: number, y: number, w: number, h: number, r = 4): string {
  if (w <= 0 || h <= 0) return '';
  const radius = Math.min(r, w / 2, h);
  const right = x + w;
  const bottom = y + h;
  return (
    `M${x},${bottom} L${x},${y + radius} Q${x},${y} ${x + radius},${y} ` +
    `L${right - radius},${y} Q${right},${y} ${right},${y + radius} L${right},${bottom} Z`
  );
}
