// The contrast arithmetic of WCAG 2 and the inks the colour engine computes
// with it (GlimStone's rule 45). Every ink handed out here reaches 4.5:1, the
// minimum for body text, whatever colour somebody picked or the system gave.
//
// The same file as web/src/lib/contrast.ts, so the app and its own web
// interface compute one ink for one colour. check-contrast.mjs holds the two
// together.

/** The ratio body text needs, and the floor of every ink computed here. */
export const MIN_TEXT_CONTRAST = 4.5;

// Carbon's own ink: on a yellow accent a brown-tinted black reads as a smudge.
const DARK_INK = '#161616';
const WHITE = '#FFFFFF';
const BLACK = '#000000';

// The card of each theme in tokens.ts, where accent text mostly sits. OLED's
// card is darker than dark's, so the dark ink holds there as well.
const LIGHT_CARD = '#FFFFFF';
const DARK_CARD = '#262626';

// The light theme's ink mix, tokens.ts's inkFor and the web's --ink-mix.
const LIGHT_INK_MIX = 0.55;

type Rgb = [number, number, number];

export function validHex(hex: string | undefined): hex is string {
  return !!hex && /^#[0-9a-fA-F]{6}$/.test(hex);
}

function rgb(hex: string): Rgb {
  const n = parseInt(hex.slice(1), 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}

function hexOf(c: Rgb): string {
  return '#' + c.map((v) => Math.round(v).toString(16).padStart(2, '0')).join('').toUpperCase();
}

/** `amount` of `to` in `from`, the way color-mix() in srgb mixes. */
function mix(from: Rgb, to: Rgb, amount: number): Rgb {
  return [0, 1, 2].map((i) => from[i] + (to[i] - from[i]) * amount) as Rgb;
}

// Relative luminance. The channels are linearised first, since raw sRGB
// overstates blue and understates green.
function luminance(c: Rgb): number {
  const lin = (v: number) => {
    const s = v / 255;
    return s <= 0.04045 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
  };
  return 0.2126 * lin(c[0]) + 0.7152 * lin(c[1]) + 0.0722 * lin(c[2]);
}

function ratio(a: Rgb, b: Rgb): number {
  const x = luminance(a);
  const y = luminance(b);
  return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
}

/** contrastRatio is the WCAG ratio of two `#rrggbb` colours, from 1 to 21. */
export function contrastRatio(a: string, b: string): number {
  return ratio(rgb(a), rgb(b));
}

/**
 * pushed moves `from` toward `to` just far enough to reach the minimum against
 * `ground`. The ratio only grows on the way while `to` lies on the far side of
 * `from` from the ground, so halving finds the least amount that does it.
 */
function pushed(from: Rgb, to: Rgb, ground: Rgb): string {
  const reaches = (t: number) => ratio(mix(from, to, t).map(Math.round) as Rgb, ground) >= MIN_TEXT_CONTRAST;
  let lo = 0;
  let hi = 1;
  for (let i = 0; i < 16; i++) {
    const mid = (lo + hi) / 2;
    if (reaches(mid)) hi = mid;
    else lo = mid;
  }
  return hexOf(mix(from, to, hi));
}

/**
 * contrastOn is the ink for text or a glyph on a fill of the given colour:
 * white or Carbon's near-black, whichever reads better. On the mid-tones where
 * neither reaches 4.5:1 the near-black goes on toward black until it does,
 * which it always can.
 */
export function contrastOn(hex: string): string {
  if (!validHex(hex)) return WHITE;
  const fill = rgb(hex);
  const onWhite = ratio(fill, rgb(WHITE));
  const onDark = ratio(fill, rgb(DARK_INK));
  if (onWhite > onDark && onWhite >= MIN_TEXT_CONTRAST) return WHITE;
  if (onDark >= MIN_TEXT_CONTRAST) return DARK_INK;
  return pushed(rgb(DARK_INK), rgb(BLACK), fill);
}

/**
 * accentInks is the accent as text on each theme where the 55% mix of the
 * light theme, or the accent itself on dark, falls short of 4.5:1 on that
 * theme's card, and undefined where the mix
 * holds, which it does for every preset. The light ink starts from the mix and
 * goes on toward black, the dark one starts from the accent and goes toward
 * white.
 */
export function accentInks(hex: string): { light: string | undefined; dark: string | undefined } {
  if (!validHex(hex)) return { light: undefined, dark: undefined };
  const accent = rgb(hex);
  const light = mix(accent, rgb(BLACK), 1 - LIGHT_INK_MIX);
  return {
    light: ratio(light, rgb(LIGHT_CARD)) >= MIN_TEXT_CONTRAST ? undefined : pushed(light, rgb(BLACK), rgb(LIGHT_CARD)),
    dark: ratio(accent, rgb(DARK_CARD)) >= MIN_TEXT_CONTRAST ? undefined : pushed(accent, rgb(WHITE), rgb(DARK_CARD)),
  };
}
