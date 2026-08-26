import { describe, expect, it } from 'vitest';
import { computeAxisTicks, tickPositionPercent } from './hourAxis';

describe('computeAxisTicks', () => {
  it('uses the fixed candidates plus the last hour on a full day', () => {
    expect(computeAxisTicks(23)).toEqual([0, 6, 12, 18, 23]);
  });

  it('returns the candidates unchanged when the last hour is one of them', () => {
    expect(computeAxisTicks(12)).toEqual([0, 6, 12]);
    expect(computeAxisTicks(6)).toEqual([0, 6]);
    expect(computeAxisTicks(0)).toEqual([0]);
  });

  it('drops a trailing candidate that would collide with the last hour', () => {
    // The axis is ~80px wide whatever it holds: "12" and "14" overlapped.
    expect(computeAxisTicks(14)).toEqual([0, 6, 14]);
    expect(computeAxisTicks(13)).toEqual([0, 6, 13]);
    expect(computeAxisTicks(7)).toEqual([0, 7]);
    expect(computeAxisTicks(19)).toEqual([0, 6, 12, 19]);
    expect(computeAxisTicks(22)).toEqual([0, 6, 12, 22]);
  });

  it('keeps a trailing candidate that clears the last hour', () => {
    expect(computeAxisTicks(10)).toEqual([0, 6, 10]);
    expect(computeAxisTicks(16)).toEqual([0, 6, 12, 16]);
  });

  it('measures separation against the axis, not in hours', () => {
    // 6 -> 9 is only three hours, but on a ten-bar axis that is 30% of the width,
    // so the tick is legible and survives. The same three hours at the end of a
    // full day would be 12% of the axis and would be dropped.
    expect(computeAxisTicks(9)).toEqual([0, 6, 9]);
    expect(computeAxisTicks(8)).toEqual([0, 6, 8]);
    expect(computeAxisTicks(21)).toEqual([0, 6, 12, 21]);
  });

  it('never drops the origin tick', () => {
    expect(computeAxisTicks(1)).toEqual([0, 1]);
    expect(computeAxisTicks(2)).toEqual([0, 2]);
  });

  it('never emits a tick past the last hour', () => {
    for (let maxHour = 0; maxHour <= 23; maxHour++) {
      const ticks = computeAxisTicks(maxHour);
      expect(Math.max(...ticks)).toBe(maxHour);
      // Strictly ascending, so labels are laid out left to right.
      expect([...ticks].sort((a, b) => a - b)).toEqual(ticks);
      expect(new Set(ticks).size).toBe(ticks.length);
    }
  });
});

describe('tickPositionPercent', () => {
  it('puts a tick at its bar centre', () => {
    // Bars are BAR_WIDTH(3) on a BAR_STRIDE(4) grid → centre at 1.5/4 = 0.375.
    expect(tickPositionPercent(0, 23)).toBeCloseTo((0.375 / 24) * 100, 5);
    expect(tickPositionPercent(12, 23)).toBeCloseTo((12.375 / 24) * 100, 5);
  });
});
