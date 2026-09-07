// hourAxis.ts - Shared hour-axis tick math for the daily-summary hourly charts.
//
// The compact per-row chart (MobileSummaryTable header) and the expanded
// SpeciesDetailCard both label the same 0..maxHour bar chart. Keeping the tick
// selection and the bar-centre positioning here means the header ticks line up
// pixel-for-pixel with the bars the detail card draws.

/** Bar-centre offset within a bar cell, in bars: HourlyMiniChart bars are
 *  BAR_WIDTH(3) wide on a BAR_STRIDE(4) grid, so the centre sits at 1.5/4 = 0.375. */
const BAR_CENTRE_FRACTION = 0.375;

/** Fixed candidate ticks; the last hour is always appended when not already present. */
const CANDIDATE_TICKS = [0, 6, 12, 18] as const;

/**
 * Minimum gap between the last two ticks, in hours.
 *
 * Hours rather than a fraction of the axis, because the chart column is sized at
 * BAR_STRIDE px per hour: the axis grows with the day, so an hour is always 4 px
 * wide and an hour count converts straight to pixels. Four hours is 16 px, which
 * clears a two-digit label (~10 px). Without this, computeAxisTicks appended
 * maxHour unconditionally and drew "12" and "14" on top of each other at 14:00.
 */
const MIN_TICK_SPACING_HOURS = 4;

/**
 * Adaptive axis ticks for a 0..maxHour chart: the fixed candidates that fall
 * within range, plus maxHour itself so the axis always ends at the last bar.
 * Naturally thins out for short "today" charts (e.g. maxHour=3 → [0, 3]).
 *
 * A trailing candidate too close to maxHour is dropped rather than drawn, unless
 * it is the only one left — the origin tick always survives.
 */
export function computeAxisTicks(maxHour: number): number[] {
  const base = CANDIDATE_TICKS.filter(h => h <= maxHour);
  const last = base[base.length - 1];
  if (last === maxHour) return base;
  if (base.length > 1 && maxHour - last < MIN_TICK_SPACING_HOURS) {
    return [...base.slice(0, -1), maxHour];
  }
  return [...base, maxHour];
}

/**
 * Horizontal position (0–100) of an hour's bar centre, matching the SVG layout:
 * (h × STRIDE + CENTRE) / ((maxHour + 1) × STRIDE) = (h + 0.375) / (maxHour + 1).
 */
export function tickPositionPercent(hour: number, maxHour: number): number {
  return ((hour + BAR_CENTRE_FRACTION) / (maxHour + 1)) * 100;
}

/** Same as tickPositionPercent but formatted as a CSS percentage string. */
export function tickPositionCss(hour: number, maxHour: number): string {
  return `${tickPositionPercent(hour, maxHour).toFixed(1)}%`;
}
