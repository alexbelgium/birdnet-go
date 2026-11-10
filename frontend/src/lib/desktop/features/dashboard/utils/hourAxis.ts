// hourAxis.ts - Shared hour-axis tick math for the daily-summary hourly charts.
//
// The compact per-row chart (MobileSummaryTable header) and the expanded
// SpeciesDetailCard both label the same 0..maxHour bar chart. Keeping the tick
// selection and the bar-centre positioning here means the header ticks line up
// pixel-for-pixel with the bars the detail card draws.

/** Bar-centre offset within a bar cell, in bars: HourlyMiniChart bars are
 *  BAR_WIDTH(3) wide on a BAR_STRIDE(4) grid, so the centre sits at 1.5/4 = 0.375. */
const BAR_CENTRE_FRACTION = 0.375;

/** Default tick step in hours: the compact table header labels 0/6/12/18. */
const DEFAULT_TICK_STEP = 6;

/**
 * Minimum gap between the last two ticks, in hours: two thirds of the step
 * (4 h for the 6-hour compact axis, 2 h for the 3-hour detail axis).
 *
 * Hours rather than a fraction of the axis, because the compact chart column is
 * sized at BAR_STRIDE px per hour: an hour is always 4 px wide there, so four
 * hours is 16 px, which clears a two-digit label (~10 px). Without this,
 * computeAxisTicks appended maxHour unconditionally and drew "12" and "14" on
 * top of each other at 14:00.
 */
function minTickSpacingHours(step: number): number {
  return Math.ceil((step * 2) / 3);
}

/**
 * Adaptive axis ticks for a 0..maxHour chart: the stepped candidates that fall
 * within range, plus maxHour itself so the axis always ends at the last bar.
 * Naturally thins out for short "today" charts (e.g. maxHour=3 → [0, 3]).
 *
 * A trailing candidate too close to maxHour is dropped rather than drawn, unless
 * it is the only one left — the origin tick always survives.
 */
export function computeAxisTicks(maxHour: number, step = DEFAULT_TICK_STEP): number[] {
  const base = Array.from({ length: Math.floor(maxHour / step) + 1 }, (_, index) => index * step);
  const last = base[base.length - 1];
  if (last === maxHour) return base;
  if (base.length > 1 && maxHour - last < minTickSpacingHours(step)) {
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
