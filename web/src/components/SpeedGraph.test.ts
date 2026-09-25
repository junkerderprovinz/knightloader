import { describe, expect, it } from 'vitest';

import { meterWindow, spanLabel } from './SpeedGraph';

describe('spanLabel', () => {
  it('writes the span in the symbols the window field writes', () => {
    expect(spanLabel(30)).toBe('-30s');
    expect(spanLabel(90)).toBe('-1.5min');
    expect(spanLabel(3600)).toBe('-1h');
  });
});

describe('meterWindow', () => {
  it('keeps whole seconds below a minute and half minutes from there, within the hour', () => {
    expect([5, 45.4, 59.6, 75, 100, 125, 4000].map(meterWindow)).toEqual([10, 45, 60, 90, 90, 120, 3600]);
  });

  it('gives the coarse ring a whole number of its ten-second steps past two minutes', () => {
    for (let s = 121; s <= 3600; s += 7) expect(meterWindow(s) % 10).toBe(0);
  });
});
