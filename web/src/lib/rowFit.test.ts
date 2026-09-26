import { describe, expect, it } from 'vitest';

import { nextFold } from './rowFit';

describe('nextFold', () => {
  it('folds one more step while the content is wider than the row', () => {
    expect(nextFold(0, 3, 1200, 944)).toBe(1);
    expect(nextFold(2, 3, 960, 944, 1300)).toBe(3);
  });

  it('stays at the last step when even that does not fit', () => {
    expect(nextFold(3, 3, 1000, 400, 1000)).toBe(3);
  });

  it('unfolds once the content the step below asked for fits again', () => {
    expect(nextFold(2, 3, 880, 1600, 1310)).toBe(1);
    expect(nextFold(1, 3, 1310, 1600, 1623)).toBe(1);
  });

  it('keeps a step whose way back has not been measured', () => {
    expect(nextFold(1, 3, 800, 2000)).toBe(1);
  });
});
