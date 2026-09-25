// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';

import { fmtEta, fmtUptime } from './format';
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
