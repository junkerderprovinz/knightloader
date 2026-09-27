import { describe, expect, it } from 'vitest';

import type { VolumeUsage } from './api';
import { foldVolumeCap } from './limitInForce';

const usage = (over: Partial<VolumeUsage>): VolumeUsage => ({
  used: 0,
  cap: 100,
  action: 'throttle',
  throttle: 500,
  periodStart: '',
  periodEnd: '',
  reached: true,
  ...over,
});

describe('foldVolumeCap', () => {
  it('keeps the limit while the cap is not reached or does not throttle', () => {
    expect(foldVolumeCap(1000, null)).toBe(1000);
    expect(foldVolumeCap(1000, usage({ reached: false }))).toBe(1000);
    expect(foldVolumeCap(1000, usage({ action: 'pause' }))).toBe(1000);
    expect(foldVolumeCap(0, usage({ throttle: 0 }))).toBe(0);
  });

  it('takes the smaller of the two once the throttle applies', () => {
    expect(foldVolumeCap(1000, usage({}))).toBe(500);
    expect(foldVolumeCap(200, usage({}))).toBe(200);
    expect(foldVolumeCap(0, usage({}))).toBe(500);
  });
});
