// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';

import { SIZE_UNITS, fmtBytes, fmtEta, fmtRate, fmtSpeed, fmtTotal, fmtUptime, splitRate } from './format';
import { fmtSkew } from './selftest';

describe('durations', () => {
  it('writes minutes as min in an ETA, as the speed window field does', () => {
    expect(fmtEta(0, 30, 1)).toBe('30s');
    expect(fmtEta(0, 90, 1)).toBe('2min');
    expect(fmtEta(0, 3900, 1)).toBe('1h 5min');
  });

  it('writes an uptime and a clock difference in the same symbols', () => {
    expect(fmtUptime(125)).toBe('2min');
    expect(fmtUptime(3725)).toBe('1h 2min');
    expect(fmtUptime(90000)).toBe('1d 1h');
    expect(fmtSkew(-150_000)).toBe('3min');
    expect(fmtSkew(3_900_000)).toBe('1h 5min');
  });
});

describe('sizes and speeds', () => {
  it('counts in thousands, as disks and lines do', () => {
    expect(fmtBytes(999)).toBe('999 B');
    expect(fmtBytes(1000)).toBe('1.0 kB');
    expect(fmtBytes(1_500_000)).toBe('1.5 MB');
    expect(fmtBytes(4_700_000_000)).toBe('4.7 GB');
    expect(fmtBytes(2e12)).toBe('2.0 TB');
    expect(fmtTotal(0)).toBe('0 B');
  });

  it('keeps a round limit round', () => {
    expect(fmtSpeed(30_000_000)).toBe('30 MB/s');
    expect(fmtSpeed(500_000)).toBe('500 kB/s');
    expect(fmtRate(0)).toBe('0 B/s');
  });

  it('splits a stored limit into the number and unit a person would type', () => {
    expect(splitRate(30_000_000)).toEqual({ value: 30, unit: 'MB/s' });
    expect(splitRate(1_500_000)).toEqual({ value: 1.5, unit: 'MB/s' });
    expect(splitRate(900_000)).toEqual({ value: 900, unit: 'kB/s' });
    expect(splitRate(0)).toEqual({ value: 0, unit: 'kB/s' });
  });

  it('enters a size in the same units without the "/s"', () => {
    expect(SIZE_UNITS.map((u) => [u.label, u.factor])).toEqual([
      ['kB', 1000],
      ['MB', 1e6],
      ['GB', 1e9],
    ]);
  });
});
