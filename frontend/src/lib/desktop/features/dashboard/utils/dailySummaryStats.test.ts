import { describe, expect, it } from 'vitest';
import type { DailySpeciesSummary } from '$lib/types/detection.types';
import {
  EBIRD_BASE_URL,
  EBIRD_DEFAULT_LANG,
  EBIRD_REGION,
  buildEbirdUrl,
  computeOverviewStats,
  isValidEbirdCode,
} from './dailySummaryStats';

describe('isValidEbirdCode', () => {
  it('returns true for valid lowercase code', () => {
    expect(isValidEbirdCode('blujay')).toBe(true);
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

  it('returns true for numeric-looking lowercase code', () => {
    expect(isValidEbirdCode('amecro')).toBe(true);
  });
});

describe('buildEbirdUrl', () => {
  it('builds correct URL for standard locale', () => {
    const url = buildEbirdUrl('blujay', 'en');
    expect(url).toBe(`${EBIRD_BASE_URL}/blujay/${EBIRD_REGION}?siteLanguage=en`);
  });

  it('maps nb locale to no', () => {
    const url = buildEbirdUrl('blujay', 'nb');
    expect(url).toBe(`${EBIRD_BASE_URL}/blujay/${EBIRD_REGION}?siteLanguage=no`);
  });

  it('falls back to default lang when locale is empty', () => {
    const url = buildEbirdUrl('blujay', '');
    expect(url).toBe(`${EBIRD_BASE_URL}/blujay/${EBIRD_REGION}?siteLanguage=${EBIRD_DEFAULT_LANG}`);
  });

  it('uses fr as default language constant', () => {
    expect(EBIRD_DEFAULT_LANG).toBe('fr');
  });

  it('uses correct region constant', () => {
    expect(EBIRD_REGION).toBe('BE-WAL');
  });
});

const makeItem = (
  count: number,
  hourly_counts: number[],
  max_confidence?: number
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
});

describe('computeOverviewStats', () => {
  const hourly24 = Array(24).fill(0) as number[];

  it('returns zeros for empty data', () => {
    const now = new Date('2024-06-20T10:00:00');
    const result = computeOverviewStats([], '2024-06-20', now);
    expect(result).toEqual({ total: 0, lastHour: 0, speciesCount: 0, isToday: true });
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
