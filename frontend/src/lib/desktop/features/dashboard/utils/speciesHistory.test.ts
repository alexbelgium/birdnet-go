import { describe, expect, it } from 'vitest';
import {
  DAY_MS,
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
} from './speciesHistory';

describe('parseYmd / formatYmd', () => {
  it('round-trips a date string', () => {
    expect(formatYmd(parseYmd('2026-07-06'))).toBe('2026-07-06');
  });

  it('parses to UTC midnight', () => {
    expect(parseYmd('2026-01-01')).toBe(Date.UTC(2026, 0, 1));
  });
});

describe('parseDailyResponse', () => {
  it('reads the {data: [...]} wrapper returned by /analytics/time/daily', () => {
    // Exact shape produced by GetDailyAnalytics in internal/api/v2/analytics.
    const raw = {
      start_date: '2026-06-01',
      end_date: '2026-06-03',
      species: 'Turdus merula',
      data: [
        { date: '2026-06-01', count: 5 },
        { date: '2026-06-03', count: 2 },
      ],
      total: 7,
    };
    expect(parseDailyResponse(raw)).toEqual([
      { date: '2026-06-01', count: 5 },
      { date: '2026-06-03', count: 2 },
    ]);
  });

  it('accepts a bare array', () => {
    expect(parseDailyResponse([{ date: '2026-06-01', count: 1 }])).toEqual([
      { date: '2026-06-01', count: 1 },
    ]);
  });

  it('drops malformed entries and clamps negative counts', () => {
    const raw = {
      data: [{ date: '2026-06-01', count: -3 }, { date: 42, count: 1 }, 'junk', null],
    };
    expect(parseDailyResponse(raw)).toEqual([{ date: '2026-06-01', count: 0 }]);
  });

  it('returns empty for non-object payloads', () => {
    expect(parseDailyResponse(null)).toEqual([]);
    expect(parseDailyResponse('oops')).toEqual([]);
    expect(parseDailyResponse({ total: 3 })).toEqual([]);
  });
});

describe('rangeWindow', () => {
  const end = parseYmd('2026-07-06');

  it('computes fixed windows inclusive of the end date', () => {
    expect(rangeWindow('7d', end, null)).toEqual({ startMs: end - 6 * DAY_MS, endMs: end });
    expect(rangeWindow('1y', end, null)).toEqual({ startMs: end - 364 * DAY_MS, endMs: end });
  });

  it('starts "all" at the first detection', () => {
    const first = parseYmd('2024-03-15');
    expect(rangeWindow('all', end, first)).toEqual({ startMs: first, endMs: end });
  });

  it('returns null for "all" when there are no detections', () => {
    expect(rangeWindow('all', end, null)).toBeNull();
  });

  it('returns null for "all" when the first detection is after the end date', () => {
    expect(rangeWindow('all', end, end + DAY_MS)).toBeNull();
  });
});

describe('bucketFor', () => {
  it('uses daily buckets up to 90d and weekly for 1-2y', () => {
    expect(bucketFor('7d', 7)).toBe('day');
    expect(bucketFor('90d', 90)).toBe('day');
    expect(bucketFor('1y', 365)).toBe('week');
    expect(bucketFor('2y', 730)).toBe('week');
  });

  it('adapts "all" to the actual span', () => {
    expect(bucketFor('all', 45)).toBe('day');
    expect(bucketFor('all', 200)).toBe('week');
    expect(bucketFor('all', 1200)).toBe('month');
  });
});

describe('bucketize', () => {
  const byDate = new Map([
    ['2026-06-01', 3],
    ['2026-06-02', 1],
    ['2026-06-10', 7],
    ['2026-07-01', 2],
  ]);

  it('zero-fills day buckets across the window', () => {
    const out = bucketize(byDate, parseYmd('2026-05-31'), parseYmd('2026-06-03'), 'day');
    expect(out.map(b => b.count)).toEqual([0, 3, 1, 0]);
    expect(out[1]?.key).toBe('2026-06-01');
    expect(out[0]?.startMs).toBe(out[0]?.endMs);
  });

  it('groups 7-day week buckets anchored at the window start', () => {
    const out = bucketize(byDate, parseYmd('2026-06-01'), parseYmd('2026-06-16'), 'week');
    expect(out).toHaveLength(3);
    // Week 1: Jun 1-7 → 3+1; week 2: Jun 8-14 → 7; week 3 (partial): Jun 15-16 → 0.
    expect(out.map(b => b.count)).toEqual([4, 7, 0]);
    expect(formatYmd(out[2]?.endMs ?? 0)).toBe('2026-06-16');
  });

  it('groups calendar-month buckets with partial edges', () => {
    const out = bucketize(byDate, parseYmd('2026-05-20'), parseYmd('2026-07-06'), 'month');
    expect(out).toHaveLength(3);
    expect(out.map(b => b.count)).toEqual([0, 11, 2]);
    expect(out.map(b => b.key)).toEqual(['2026-05-20', '2026-06-01', '2026-07-01']);
  });

  it('returns empty when the window is inverted', () => {
    expect(bucketize(byDate, parseYmd('2026-06-02'), parseYmd('2026-06-01'), 'day')).toEqual([]);
  });
});

describe('movingAverage', () => {
  it('averages a trailing window with partial windows at the start', () => {
    expect(movingAverage([2, 4, 6, 8], 2)).toEqual([2, 3, 5, 7]);
  });

  it('handles an empty series', () => {
    expect(movingAverage([], 7)).toEqual([]);
  });
});

describe('labels', () => {
  it('formats day, week and month bucket labels', () => {
    const day = {
      key: '2026-06-12',
      startMs: parseYmd('2026-06-12'),
      endMs: parseYmd('2026-06-12'),
      count: 1,
    };
    expect(bucketLabel(day, 'day')).toBe('Jun 12, 2026');

    const week = {
      key: '2026-06-12',
      startMs: parseYmd('2026-06-12'),
      endMs: parseYmd('2026-06-18'),
      count: 1,
    };
    expect(bucketLabel(week, 'week')).toBe('Jun 12 – 18, 2026');

    const weekAcross = {
      key: '2026-06-29',
      startMs: parseYmd('2026-06-29'),
      endMs: parseYmd('2026-07-05'),
      count: 1,
    };
    expect(bucketLabel(weekAcross, 'week')).toBe('Jun 29 – Jul 5, 2026');

    const month = {
      key: '2026-06-01',
      startMs: parseYmd('2026-06-01'),
      endMs: parseYmd('2026-06-30'),
      count: 1,
    };
    expect(bucketLabel(month, 'month')).toBe('Jun 2026');
  });

  it('formats compact axis labels', () => {
    const day = {
      key: '2026-06-12',
      startMs: parseYmd('2026-06-12'),
      endMs: parseYmd('2026-06-12'),
      count: 1,
    };
    expect(axisLabel(day, 'day')).toBe('Jun 12');
    const month = {
      key: '2026-06-01',
      startMs: parseYmd('2026-06-01'),
      endMs: parseYmd('2026-06-30'),
      count: 1,
    };
    expect(axisLabel(month, 'month')).toBe("Jun '26");
  });

  it('shortDayLabel drops the year', () => {
    expect(shortDayLabel(parseYmd('2026-01-03'))).toBe('Jan 3');
  });
});

describe('buildXTickIndices', () => {
  it('always includes the first and last bucket', () => {
    expect(buildXTickIndices(30, 5)).toEqual([0, 7, 15, 22, 29]);
  });

  it('handles fewer buckets than ticks without duplicates', () => {
    expect(buildXTickIndices(2, 5)).toEqual([0, 1]);
    expect(buildXTickIndices(1, 5)).toEqual([0]);
    expect(buildXTickIndices(0, 5)).toEqual([]);
  });
});

describe('buildYTicks', () => {
  it('uses a clean even maximum with an integer midpoint', () => {
    expect(buildYTicks(1)).toEqual([0, 1]);
    expect(buildYTicks(2)).toEqual([0, 1, 2]);
    expect(buildYTicks(5)).toEqual([0, 3, 6]);
    expect(buildYTicks(47)).toEqual([0, 30, 60]);
    expect(buildYTicks(100)).toEqual([0, 50, 100]);
    expect(buildYTicks(750)).toEqual([0, 400, 800]);
  });
});

describe('selectPeakIndices', () => {
  it('labels the global maximum', () => {
    expect(selectPeakIndices([0, 1, 9, 1, 0])).toContain(2);
  });

  it('returns nothing for an all-zero series', () => {
    expect(selectPeakIndices([0, 0, 0])).toEqual([]);
    expect(selectPeakIndices([])).toEqual([]);
  });

  it('skips minor local maxima below 30% of the peak', () => {
    const counts = [0, 1, 0, 0, 0, 0, 0, 0, 0, 10];
    expect(selectPeakIndices(counts)).toEqual([9]);
  });

  it('keeps a minimum gap between labeled peaks', () => {
    // Two adjacent peaks: only the larger one is labeled.
    const counts = [0, 8, 9, 0, 0, 0, 0, 0, 0, 0];
    expect(selectPeakIndices(counts)).toEqual([2]);
  });

  it('labels well-separated comparable peaks', () => {
    const counts = new Array<number>(30).fill(0);
    counts[3] = 8;
    counts[15] = 10;
    counts[27] = 6;
    expect(selectPeakIndices(counts)).toEqual([3, 15, 27]);
  });
});

describe('topRoundedBarPath', () => {
  it('produces a closed path with the radius capped by bar width', () => {
    const path = topRoundedBarPath(10, 20, 4, 30);
    expect(path.startsWith('M10,50')).toBe(true);
    expect(path.endsWith('Z')).toBe(true);
    // Radius capped at w/2 = 2.
    expect(path).toContain('L10,22');
  });

  it('returns an empty path for degenerate bars', () => {
    expect(topRoundedBarPath(0, 0, 0, 10)).toBe('');
    expect(topRoundedBarPath(0, 0, 10, 0)).toBe('');
  });
});
