// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from 'vitest';

import {
  LEAF_TAPS,
  RAINBOW,
  applyCachedAppearance,
  applyRainbow,
  applyShape,
  cacheAppearance,
  leafTap,
  takePageHue,
} from './appearance';

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

describe('takePageHue', () => {
  const buttons = (n: number) => {
    document.body.innerHTML = '';
    return Array.from({ length: n }, () => {
      const b = document.createElement('button');
      b.className = 'glim-btn';
      document.body.append(b);
      return b;
    });
  };
  const tint = (el: HTMLElement) => el.style.getPropertyValue('--btn-tint');

  it('gives two neighbouring buttons different colours under rainbow', () => {
    applyRainbow({ on: true });
    const [a, b] = buttons(2);
    takePageHue(a!);
    takePageHue(b!);
    expect(tint(a!)).toContain('--rb-0');
    expect(tint(b!)).toContain('--rb-1');
  });

  it('starts the palette over after its last colour', () => {
    applyRainbow({ on: true });
    const all = buttons(RAINBOW.length + 1);
    takePageHue(all[RAINBOW.length]!);
    expect(tint(all[RAINBOW.length]!)).toContain('--rb-0');
  });

  it('leaves the tint to the accent with rainbow off', () => {
    applyRainbow({ on: true });
    const [a] = buttons(1);
    takePageHue(a!);
    applyRainbow({ on: false });
    takePageHue(a!);
    expect(tint(a!)).toBe('');
  });
});
