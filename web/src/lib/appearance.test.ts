// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from 'vitest';

import { LEAF_TAPS, applyCachedAppearance, applyShape, cacheAppearance, leafTap } from './appearance';

const shapeOnRoot = () => document.documentElement.getAttribute('data-shape');

describe('the shape at boot', () => {
  beforeEach(() => {
    localStorage.clear();
    document.documentElement.removeAttribute('data-shape');
  });

  it('starts on soft where nothing is stored', () => {
    applyCachedAppearance();
    expect(shapeOnRoot()).toBe('soft');
  });

  it('keeps a stored round', () => {
    cacheAppearance('round', '');
    applyCachedAppearance();
    expect(shapeOnRoot()).toBe('round');
  });

  it('keeps a found leaf across a reload', () => {
    cacheAppearance('leaf', '');
    applyCachedAppearance();
    expect(shapeOnRoot()).toBe('leaf');
  });

  it('puts soft on the root for a value no picker offers', () => {
    applyShape('oval');
    expect(shapeOnRoot()).toBe('soft');
  });
});

describe('leafTap', () => {
  const tapSquare = (state: { taps: number }, times: number) => {
    let last;
    for (let i = 0; i < times; i++) last = leafTap(state, 'square', 'square');
    return last;
  };

  it('reveals the leaf on the fifth tap on a chosen square', () => {
    const state = { taps: 0 };
    expect(tapSquare(state, LEAF_TAPS - 1)).toBeUndefined();
    expect(leafTap(state, 'square', 'square')).toBe('leaf');
  });

  it('does not count the tap that chooses square', () => {
    const state = { taps: 0 };
    expect(leafTap(state, 'square', 'round')).toBeUndefined();
    expect(tapSquare(state, LEAF_TAPS - 1)).toBeUndefined();
  });

  it('starts over after a tap on another shape', () => {
    const state = { taps: 0 };
    tapSquare(state, LEAF_TAPS - 1);
    leafTap(state, 'round', 'square');
    expect(tapSquare(state, LEAF_TAPS - 1)).toBeUndefined();
  });
});
