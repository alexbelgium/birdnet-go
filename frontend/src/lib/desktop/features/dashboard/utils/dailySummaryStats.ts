import type { DailySpeciesSummary } from '$lib/types/detection.types';
import { getLocalDateString } from '$lib/utils/date';
import { safeArrayAccess } from '$lib/utils/security';

export const EBIRD_BASE_URL = 'https://ebird.org/species';
export const EBIRD_REGION = 'BE-WAL';
export const EBIRD_DEFAULT_LANG = 'fr';

/** Returns true when code is a valid, all-lowercase eBird species code. */
export function isValidEbirdCode(code: string | undefined): boolean {
  return !!code && code === code.toLowerCase();
}

/** Builds the full eBird species page URL for the given locale. */
export function buildEbirdUrl(speciesCode: string, locale: string): string {
  const lang = locale === 'nb' ? 'no' : locale || EBIRD_DEFAULT_LANG;
  return `${EBIRD_BASE_URL}/${speciesCode}/${EBIRD_REGION}?siteLanguage=${lang}`;
}

// Confidence gradient anchor points (hue degrees) — smooth red(0)→orange(30)→green(120).
const CONFIDENCE_HUE_ORANGE = 30; // hue at the orange knee (70%)
const CONFIDENCE_HUE_GREEN = 120; // hue at 100%
const CONFIDENCE_ORANGE_PERCENT = 70; // percent where the knee sits

/**
 * Returns an HSL color for a confidence percentage (0–100).
 * 0% = red (hue 0), 70% = orange (hue 30), 100% = green (hue 120).
 * Piecewise linear on the hue channel so every value in 70–100 is
 * visually distinct; fixed saturation/lightness keeps white text readable.
 */
export function computeConfidenceColor(percent: number): string {
  const p = Math.max(0, Math.min(100, percent));
  const hue =
    p <= CONFIDENCE_ORANGE_PERCENT
      ? (p / CONFIDENCE_ORANGE_PERCENT) * CONFIDENCE_HUE_ORANGE
      : CONFIDENCE_HUE_ORANGE +
        ((p - CONFIDENCE_ORANGE_PERCENT) / (100 - CONFIDENCE_ORANGE_PERCENT)) *
          (CONFIDENCE_HUE_GREEN - CONFIDENCE_HUE_ORANGE);
  return `hsl(${Math.round(hue)}, 75%, 42%)`;
}

// Color palette for mini bar chart — mirrors daylight class colors from DailySummaryCard.svelte
// but as solid hex values suitable for SVG fill attributes.
const DAYLIGHT_COLORS = {
  deepNight: '#1e1b4b',
  night: '#312e81',
  preDawn: '#6366f1',
  sunrise: '#fb923c',
  earlyDay: '#fbbf24',
  day: '#86efac',
  midDay: '#4ade80',
  lateDay: '#86efac',
  sunset: '#f472b6',
  dusk: '#a78bfa',
  evening: '#4338ca',
} as const;

const DAWN_DUSK_OFFSET = 2;
const MIDDAY_THRESHOLD = 0.3;
const DAY_THRESHOLD = 0.7;

/**
 * Returns a solid CSS hex color for an SVG bar at `hour`, based on its
 * position relative to sunrise and sunset. Mirrors getDaylightClass() in
 * DailySummaryCard.svelte but as a pure, exportable function.
 * Falls back to day/night split at hours 6 and 20 when sun times are unknown.
 */
export function computeHourDaylightColor(
  hour: number,
  sunriseHour: number | null,
  sunsetHour: number | null
): string {
  if (sunriseHour === null || sunsetHour === null) {
    return hour >= 6 && hour < 20 ? DAYLIGHT_COLORS.day : DAYLIGHT_COLORS.deepNight;
  }
  if (hour === sunriseHour) return DAYLIGHT_COLORS.sunrise;
  if (hour === sunsetHour) return DAYLIGHT_COLORS.sunset;
  if (hour >= sunriseHour - DAWN_DUSK_OFFSET && hour < sunriseHour) return DAYLIGHT_COLORS.preDawn;
  if (hour > sunsetHour && hour <= sunsetHour + DAWN_DUSK_OFFSET) return DAYLIGHT_COLORS.dusk;
  if (hour > sunriseHour && hour < sunsetHour) {
    const midday = (sunriseHour + sunsetHour) / 2;
    const halfDay = (sunsetHour - sunriseHour) / 2;
    const dist = Math.abs(hour - midday) / halfDay;
    if (dist < MIDDAY_THRESHOLD) return DAYLIGHT_COLORS.midDay;
    if (dist < DAY_THRESHOLD) return DAYLIGHT_COLORS.day;
    return hour < midday ? DAYLIGHT_COLORS.earlyDay : DAYLIGHT_COLORS.lateDay;
  }
  if (hour <= 3 || hour >= 22) return DAYLIGHT_COLORS.deepNight;
  if (hour <= 5 || hour >= 20) return DAYLIGHT_COLORS.night;
  return DAYLIGHT_COLORS.evening;
}

/**
 * Formats a detection count for compact display (≤4 chars).
 * < 1000 → "987", 1000–9999 → "1.2k", ≥ 10000 → "12k".
 */
export function formatDetectionCount(n: number): string {
  if (n < 1000) return String(n);
  if (n < 10000) return `${(n / 1000).toFixed(1)}k`;
  return `${Math.round(n / 1000)}k`;
}

export interface OverviewStats {
  total: number;
  lastHour: number;
  speciesCount: number;
  isToday: boolean;
}

/**
 * Computes summary stats for the overview bar.
 * lastHour reflects the current clock hour and is 0 for past dates.
 */
export function computeOverviewStats(
  data: DailySpeciesSummary[],
  selectedDate: string,
  now: Date = new Date()
): OverviewStats {
  const isToday = selectedDate === getLocalDateString(now);
  const currentHour = now.getHours();

  let total = 0;
  let lastHour = 0;
  for (const item of data) {
    total += item.count;
    if (isToday) {
      const hourCount = safeArrayAccess(item.hourly_counts, currentHour, 0) ?? 0;
      lastHour += hourCount;
    }
  }

  return { total, lastHour, speciesCount: data.length, isToday };
}
