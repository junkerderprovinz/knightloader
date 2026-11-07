import { describe, expect, it } from 'vitest';

import { ACCENTS, RAINBOW } from './appearance';
import { MIN_TEXT_CONTRAST, accentInks, contrastOn, contrastRatio } from './contrast';

const EXTREMES = {
  'near white': '#FAFAFA',
  'near black': '#050505',
  'mid grey': '#777777',
  'saturated blue': '#0000FF',
  yellow: '#FFFF00',
  'the crossover grey': '#7B7B7B',
};

// The grounds accent text sits on in each theme: the page and the card.
const LIGHT_GROUNDS = ['#FFFFFF'];
const DARK_GROUNDS = ['#161616', '#262626'];
const OLED_GROUNDS = ['#000000', '#161616'];

// index.css's light ink, color-mix(in srgb, accent 55%, black).
function lightMix(hex: string): string {
  const n = parseInt(hex.slice(1), 16);
  return '#' + [16, 8, 0].map((s) => Math.round(((n >> s) & 255) * 0.55).toString(16).padStart(2, '0')).join('');
}

const lightInk = (hex: string) => accentInks(hex).light ?? lightMix(hex);
const darkInk = (hex: string) => accentInks(hex).dark ?? hex;

describe('the contrast ratio', () => {
  it('is 21:1 for black on white and 1:1 for a colour on itself', () => {
    expect(contrastRatio('#000000', '#FFFFFF')).toBe(21);
    expect(contrastRatio('#FCC419', '#FCC419')).toBe(1);
  });

  it('is the same whichever colour comes first', () => {
    expect(contrastRatio('#1D99F3', '#161616')).toBe(contrastRatio('#161616', '#1D99F3'));
  });
});

describe('every computed ink', () => {
  for (const [name, hex] of Object.entries(EXTREMES)) {
    it(`reaches 4.5:1 on a ${name} fill`, () => {
      expect(contrastRatio(contrastOn(hex), hex)).toBeGreaterThanOrEqual(MIN_TEXT_CONTRAST);
    });

    it(`reads at 4.5:1 for a ${name} accent as text in every theme`, () => {
      for (const g of LIGHT_GROUNDS) expect(contrastRatio(lightInk(hex), g), `light on ${g}`).toBeGreaterThanOrEqual(4.5);
      for (const g of DARK_GROUNDS) expect(contrastRatio(darkInk(hex), g), `dark on ${g}`).toBeGreaterThanOrEqual(4.5);
      for (const g of OLED_GROUNDS) expect(contrastRatio(darkInk(hex), g), `OLED on ${g}`).toBeGreaterThanOrEqual(4.5);
    });
  }
});

describe('the ink on a fill', () => {
  it('is near-black on a light fill and white on a dark one', () => {
    expect(contrastOn('#FAFAFA')).toBe('#161616');
    expect(contrastOn('#FFFF00')).toBe('#161616');
    expect(contrastOn('#050505')).toBe('#FFFFFF');
    expect(contrastOn('#0000FF')).toBe('#FFFFFF');
  });

  it('goes on toward black just far enough where neither ink reaches 4.5:1', () => {
    const ink = contrastOn('#7B7B7B');
    expect(ink).not.toBe('#161616');
    expect(ink).not.toBe('#FFFFFF');
    const v = parseInt(ink.slice(1, 3), 16);
    expect(v).toBeLessThan(0x16);
    const lighter = '#' + (v + 1).toString(16).padStart(2, '0').repeat(3);
    expect(contrastRatio(lighter, '#7B7B7B')).toBeLessThan(MIN_TEXT_CONTRAST);
  });

  it('is near-black on every preset, also on Blue, Red, Purple, Orange and Pink, where white reaches only 2.3 to 3.0:1', () => {
    for (const a of ACCENTS) expect(contrastOn(a.hex), a.name).toBe('#161616');
  });

  it('reaches 4.5:1 on every preset and every rainbow colour', () => {
    for (const hex of [...ACCENTS.map((a) => a.hex), ...RAINBOW]) {
      expect(contrastRatio(contrastOn(hex), hex), hex).toBeGreaterThanOrEqual(MIN_TEXT_CONTRAST);
    }
  });

  it('is white for anything but a six-digit hex', () => {
    expect(contrastOn('red')).toBe('#FFFFFF');
  });
});

describe('the accent as text', () => {
  it('keeps the stylesheet ink on every preset', () => {
    for (const a of ACCENTS) expect(accentInks(a.hex), a.name).toEqual({ light: undefined, dark: undefined });
  });

  it('pushes a pale accent toward black in the light theme only as far as it needs', () => {
    const { light, dark } = accentInks('#FFFF00');
    expect(dark).toBeUndefined();
    expect(contrastRatio(light!, '#FFFFFF')).toBeLessThan(4.6);
  });

  it('pushes a dark accent toward white in the dark theme', () => {
    const { light, dark } = accentInks('#0000FF');
    expect(light).toBeUndefined();
    expect(contrastRatio(dark!, '#262626')).toBeLessThan(4.6);
  });

  it('sets nothing for a colour that is not a six-digit hex', () => {
    expect(accentInks('#FFF')).toEqual({ light: undefined, dark: undefined });
  });
});
