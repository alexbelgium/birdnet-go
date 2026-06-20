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
