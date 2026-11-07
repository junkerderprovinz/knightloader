// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from 'vitest';

import {
  LEAF_TAPS,
  RAINBOW,
  applyAccent,
  applyCachedAppearance,
  applyRainbow,
  applyShape,
  cacheAppearance,
  leafTap,
  resolveAccent,
  setFollowsSystemAccent,
  setSystemAccent,
  subscribeSystemAccent,
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
    return Array.from({ length: n }, (_, i) => {
      const b = document.createElement('button');
      b.className = i % 2 ? 'glim-btn-bare' : 'glim-btn-subtle';
      document.body.append(b);
      return b;
    });
  };
  const tint = (el: HTMLElement) => el.style.getPropertyValue('--btn-tint');

  it('gives a surface button and the glyph button beside it different colours under rainbow', () => {
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

describe('the accent on the root', () => {
  const root = () => document.documentElement.style;

  it('gives a pale accent its own light ink and leaves the dark one to the stylesheet', () => {
    applyAccent('#FFFF00');
    expect(root().getPropertyValue('--accent-ink-light')).toMatch(/^#[0-9A-F]{6}$/);
    expect(root().getPropertyValue('--accent-ink-dark')).toBe('');
  });

  it('takes the inks away again for a preset and for the default', () => {
    applyAccent('#0000FF');
    expect(root().getPropertyValue('--accent-ink-dark')).not.toBe('');
    applyAccent('#1D99F3');
    expect(root().getPropertyValue('--accent-ink-dark')).toBe('');
    applyAccent('#FFFF00');
    applyAccent(undefined);
    expect(root().getPropertyValue('--accent-ink-light')).toBe('');
    expect(root().getPropertyValue('--accent')).toBe('');
  });

  it('puts near-black on Blue, where white reaches only 3:1', () => {
    applyAccent('#1D99F3');
    expect(root().getPropertyValue('--accent-contrast')).toBe('#161616');
  });
});

describe('the system colour', () => {
  const accent = () => document.documentElement.style.getPropertyValue('--accent');

  beforeEach(() => {
    localStorage.clear();
    setSystemAccent(undefined);
    setFollowsSystemAccent(false);
  });

  it('wears the instance accent, a source or not, until the wand is chosen', () => {
    applyAccent('#1D99F3');
    setSystemAccent('#E0383E');
    expect(accent()).toBe('#1D99F3');
  });

  it('paints the source once the wand is chosen, and follows the source as it changes', () => {
    applyAccent('#1D99F3');
    setSystemAccent('#E0383E');
    setFollowsSystemAccent(true);
    expect(accent()).toBe('#E0383E');
    setSystemAccent('#62BA46');
    expect(accent()).toBe('#62BA46');
  });

  it('falls back to the instance accent where the device has no colour', () => {
    applyAccent('#FF8389');
    setFollowsSystemAccent(true);
    expect(accent()).toBe('#FF8389');
  });

  it('goes back to the instance accent when the wand is left', () => {
    applyAccent('#1D99F3');
    setSystemAccent('#E0383E');
    setFollowsSystemAccent(true);
    expect(localStorage.getItem('kl-accent-system')).toBe('system');
    setFollowsSystemAccent(false);
    expect(accent()).toBe('#1D99F3');
    expect(localStorage.getItem('kl-accent-system')).toBeNull();
  });

  it('keeps the choice and the last colour in this browser, so the first frame after a reload wears them', () => {
    cacheAppearance('soft', '#1D99F3');
    localStorage.setItem('kl-accent-system', 'system');
    localStorage.setItem('kl-system-accent', '#E0383E');
    applyCachedAppearance();
    expect(accent()).toBe('#E0383E');
  });

  it('tells whoever draws the wand when the colour or the choice changes', () => {
    let heard = 0;
    const stop = subscribeSystemAccent(() => heard++);
    setSystemAccent('#E0383E');
    setSystemAccent('#E0383E');
    setFollowsSystemAccent(true);
    stop();
    setSystemAccent(undefined);
    expect(heard).toBe(2);
  });

  it('resolves to the source only while the wand is chosen and the source has a colour', () => {
    expect(resolveAccent('#1D99F3', true, '#E0383E')).toBe('#E0383E');
    expect(resolveAccent('#1D99F3', false, '#E0383E')).toBe('#1D99F3');
    expect(resolveAccent('#1D99F3', true, undefined)).toBe('#1D99F3');
    expect(resolveAccent('', true, undefined)).toBeUndefined();
  });
});
