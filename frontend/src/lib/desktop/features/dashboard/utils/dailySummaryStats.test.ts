import { describe, expect, it } from 'vitest';
import type { DailySpeciesSummary } from '$lib/types/detection.types';
import {
  EBIRD_BASE_URL,
  EBIRD_DEFAULT_LANG,
  buildEbirdUrl,
  computeConfidenceColor,
  computeOverviewStats,
  formatDetectionCount,
  isValidEbirdCode,
} from './dailySummaryStats';

/** Extracts the integer hue from an `hsl(H, S%, L%)` string. */
function hueOf(hsl: string): number {
  const match = /hsl\((\d+),/.exec(hsl);
  expect(match).not.toBeNull();
  return Number(match?.[1]);
}

describe('isValidEbirdCode', () => {
  it('returns true for valid lowercase alphabetic code', () => {
    expect(isValidEbirdCode('blujay')).toBe(true);
  });

  it('returns true for valid lowercase alphanumeric code', () => {
    expect(isValidEbirdCode('amecro')).toBe(true);
    expect(isValidEbirdCode('abc123')).toBe(true);
  });

  it('returns false for uppercase code', () => {
    expect(isValidEbirdCode('BluJay')).toBe(false);
  });

  it('returns false for empty string', () => {
    expect(isValidEbirdCode('')).toBe(false);
  });

  it('returns false for undefined', () => {
    expect(isValidEbirdCode(undefined)).toBe(false);
  });

  it('returns false for code with slash', () => {
    expect(isValidEbirdCode('foo/bar')).toBe(false);
  });

  it('returns false for code with space', () => {
    expect(isValidEbirdCode(' ')).toBe(false);
    expect(isValidEbirdCode('foo bar')).toBe(false);
  });
});

describe('buildEbirdUrl', () => {
  it('builds correct URL without region for standard locale', () => {
    const url = buildEbirdUrl('blujay', 'en');
    expect(url).toBe(`${EBIRD_BASE_URL}/blujay?siteLanguage=en`);
  });

  it('maps nb locale to no', () => {
    const url = buildEbirdUrl('blujay', 'nb');
    expect(url).toBe(`${EBIRD_BASE_URL}/blujay?siteLanguage=no`);
  });

  it('falls back to default lang when locale is empty', () => {
    const url = buildEbirdUrl('blujay', '');
    expect(url).toBe(`${EBIRD_BASE_URL}/blujay?siteLanguage=${EBIRD_DEFAULT_LANG}`);
  });

  it('uses en as default language constant', () => {
    expect(EBIRD_DEFAULT_LANG).toBe('en');
  });

  it('encodes special characters in species code', () => {
    const url = buildEbirdUrl('foo bar', 'en');
    expect(url).toBe(`${EBIRD_BASE_URL}/foo%20bar?siteLanguage=en`);
  });
});

const makeItem = (
  count: number,
  hourly_counts: number[],
  max_confidence?: number,
  novelty?: Partial<
    Pick<DailySpeciesSummary, 'is_new_species' | 'is_new_this_year' | 'is_new_this_season'>
  >
): DailySpeciesSummary => ({
  scientific_name: 'Test Bird',
  common_name: 'Test Bird',
  species_code: 'testbi',
  count,
  hourly_counts,
  high_confidence: true,
  max_confidence,
  first_heard: '',
  latest_heard: '',
  thumbnail_url: '',
  ...novelty,
});

describe('formatDetectionCount', () => {
  it('returns the number as string for values under 1000', () => {
    expect(formatDetectionCount(0)).toBe('0');
    expect(formatDetectionCount(999)).toBe('999');
    expect(formatDetectionCount(1)).toBe('1');
  });

  it('returns truncated k-notation for 1000–9999', () => {
    expect(formatDetectionCount(1000)).toBe('1.0k');
    expect(formatDetectionCount(1234)).toBe('1.2k');
    expect(formatDetectionCount(9999)).toBe('9.9k');
  });

  it('never rounds up to 10.0k within the < 10000 range', () => {
    // 9999 previously rounded to "10.0k" (5 chars); must stay "9.9k" (4 chars)
    expect(formatDetectionCount(9999)).toBe('9.9k');
    expect(formatDetectionCount(9950)).toBe('9.9k');
  });

  it('returns rounded k-notation for values >= 10000', () => {
    expect(formatDetectionCount(10000)).toBe('10k');
    expect(formatDetectionCount(12345)).toBe('12k');
    expect(formatDetectionCount(99999)).toBe('100k');
  });
});

describe('computeOverviewStats', () => {
  const hourly24 = Array(24).fill(0) as number[];

  it('returns zeros for empty data', () => {
    const now = new Date('2024-06-20T10:00:00');
    const result = computeOverviewStats([], '2024-06-20', now);
    expect(result).toEqual({
      total: 0,
      lastHour: 0,
      speciesCount: 0,
      newSpecies: 0,
      isToday: true,
    });
  });

  it('counts species new in any tracked period as newSpecies', () => {
    const data = [
      makeItem(5, hourly24, 0.9, { is_new_species: true }), // lifetime
      makeItem(3, hourly24, 0.8, { is_new_this_year: true }), // year
      makeItem(2, hourly24, 0.7, { is_new_this_season: true }), // season
      makeItem(9, hourly24, 0.95), // not new
    ];
    const now = new Date('2024-06-20T10:00:00');
    const result = computeOverviewStats(data, '2024-06-20', now);
    expect(result.newSpecies).toBe(3);
    expect(result.speciesCount).toBe(4);
  });

  it('reports zero newSpecies when nothing is new', () => {
    const data = [makeItem(5, hourly24), makeItem(3, hourly24)];
    const now = new Date('2024-06-20T10:00:00');
    expect(computeOverviewStats(data, '2024-06-20', now).newSpecies).toBe(0);
  });

  it('sums total detections and species count', () => {
    const data = [makeItem(5, hourly24), makeItem(3, hourly24)];
    const now = new Date('2024-06-20T10:00:00');
    const result = computeOverviewStats(data, '2024-06-20', now);
    expect(result.total).toBe(8);
    expect(result.speciesCount).toBe(2);
  });

  it('computes lastHour for today using current clock hour', () => {
    const hourlyToday = Array(24).fill(0) as number[];
    hourlyToday[10] = 4; // 10:xx detections
    hourlyToday[11] = 7;
    const data = [makeItem(11, hourlyToday), makeItem(3, hourlyToday)];
    const now = new Date('2024-06-20T10:30:00'); // hour = 10
    const result = computeOverviewStats(data, '2024-06-20', now);
    expect(result.lastHour).toBe(8); // 4 + 4
    expect(result.isToday).toBe(true);
  });

  it('sets lastHour to 0 for past dates', () => {
    const hourlyPast = Array(24).fill(5) as number[];
    const data = [makeItem(120, hourlyPast)];
    const now = new Date('2024-06-20T10:00:00');
    const result = computeOverviewStats(data, '2024-06-19', now);
    expect(result.lastHour).toBe(0);
    expect(result.isToday).toBe(false);
  });

  it('correctly identifies today vs past date', () => {
    const now = new Date('2024-06-20T08:00:00');
    expect(computeOverviewStats([], '2024-06-20', now).isToday).toBe(true);
    expect(computeOverviewStats([], '2024-06-19', now).isToday).toBe(false);
  });
});

describe('computeConfidenceColor', () => {
  it('is pure red at 0%', () => {
    expect(hueOf(computeConfidenceColor(0))).toBe(0);
  });

  it('is orange at 70%', () => {
    expect(hueOf(computeConfidenceColor(70))).toBe(30);
  });

  it('is green at 100%', () => {
    expect(hueOf(computeConfidenceColor(100))).toBe(120);
  });

  it('interpolates between red and orange below 70%', () => {
    const hue = hueOf(computeConfidenceColor(50));
    expect(hue).toBeGreaterThan(0);
    expect(hue).toBeLessThan(30);
  });

  it('interpolates between orange and green above 70%', () => {
    const hue = hueOf(computeConfidenceColor(85));
    expect(hue).toBeGreaterThan(30);
    expect(hue).toBeLessThan(120);
  });

  it('clamps values below 0 to red', () => {
    expect(hueOf(computeConfidenceColor(-5))).toBe(0);
  });

  it('clamps values above 100 to green', () => {
    expect(hueOf(computeConfidenceColor(110))).toBe(120);
  });

  it('returns a valid hsl string', () => {
    expect(computeConfidenceColor(75)).toMatch(/^hsl\(\d+, \d+%, \d+%\)$/);
  });
});
