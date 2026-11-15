// The contrast arithmetic of WCAG 2 and the inks the colour engine computes
// with it (GlimStone's rule 45). Every ink handed out here reaches 4.5:1, the
// minimum for body text, whatever colour somebody picked or the system gave.

/** The ratio body text needs, and the floor of every ink computed here. */
export const MIN_TEXT_CONTRAST = 4.5;

// Carbon's own ink: on a yellow accent a brown-tinted black reads as a smudge.
const DARK_INK = '#161616';
const WHITE = '#FFFFFF';
const BLACK = '#000000';

// The card of each theme in index.css, where accent text mostly sits. OLED's
// card is darker than dark's, so the dark ink holds there as well.
const LIGHT_CARD = '#FFFFFF';
const DARK_CARD = '#262626';
// The share of the accent in --accent-soft, which applyAccent writes.
const SOFT_SHARE = 0.14;

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
 * The lightness steps of the accent as ink, index.css's --ink-l-light and
 * --ink-l-dark: on the light theme the ink sits at this OKLab lightness or
 * below, on the dark ones at this one or above. The contrast step takes the
 * second pair.
 */
export const INK_LIGHTNESS = { light: 0.52, dark: 0.74, lightMore: 0.4, darkMore: 0.84 } as const;

type Oklab = [number, number, number];

const linear = (v: number) => {
  const s = v / 255;
  return s <= 0.04045 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
};
const gamma = (v: number) => (v <= 0.0031308 ? 12.92 * v : 1.055 * Math.pow(v, 1 / 2.4) - 0.055);

// OKLab's lightness follows what the eye sees, so one step gives every hue
// nearly the same contrast, where a mix toward black in sRGB lands blue far
// darker than yellow.
function oklabOf(hex: string): Oklab {
  const [r, g, b] = rgb(hex).map(linear) as Rgb;
  const l = Math.cbrt(0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b);
  const m = Math.cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b);
  const s = Math.cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b);
  return [
    0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s,
    1.9779984951 * l - 2.428592205 * m + 0.4505937099 * s,
    0.0259040371 * l + 0.7827717662 * m - 0.808675766 * s,
  ];
}

// Each channel is clipped into sRGB, as the browser does: a step toward white
// can leave it by a hair.
function srgbOf([L, a, b]: Oklab): string {
  const l = (L + 0.3963377774 * a + 0.2158037573 * b) ** 3;
  const m = (L - 0.1055613458 * a - 0.0638541728 * b) ** 3;
  const s = (L - 0.0894841775 * a - 1.291485548 * b) ** 3;
  const c = [
    4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s,
    -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s,
    -0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s,
  ];
  return hexOf(c.map((v) => Math.round(Math.min(1, Math.max(0, gamma(v))) * 255)) as Rgb);
}

/** darkenTo is `hex` moved toward black in OKLab until its lightness is at most `lightness`. */
export function darkenTo(hex: string, lightness: number): string {
  const [l, a, b] = oklabOf(hex);
  if (l <= lightness) return hex.toUpperCase();
  const k = lightness / l;
  return srgbOf([lightness, a * k, b * k]);
}

/** lightenTo is `hex` moved toward white in OKLab until its lightness is at least `lightness`. */
export function lightenTo(hex: string, lightness: number): string {
  const [l, a, b] = oklabOf(hex);
  if (l >= lightness) return hex.toUpperCase();
  const k = (1 - lightness) / (1 - l);
  return srgbOf([lightness, a * k, b * k]);
}

/**
 * inkCss is the CSS that moves `colour` onto each theme's step the way
 * darkenTo and lightenTo do, for an ink the stylesheet derives from a colour it
 * only knows at run time, such as a rainbow position.
 */
export function inkCss(colour: string): string {
  const down = 'var(--ink-l-light)';
  const up = 'var(--ink-l-dark)';
  const light = `oklab(from ${colour} min(l, ${down}) calc(a * min(1, ${down} / l)) calc(b * min(1, ${down} / l)))`;
  const dark = `oklab(from ${colour} max(l, ${up}) calc(a * min(1, (1 - ${up}) / (1 - l))) calc(b * min(1, (1 - ${up}) / (1 - l))))`;
  return `light-dark(${light}, ${dark})`;
}

/**
 * accentInks is the accent as text on each theme where index.css's own step
 * falls short of 4.5:1 on that theme's card or on the accent's own wash there
 * (--accent-soft, a picked row or a selected badge), and undefined where the
 * step holds, which it does for every preset. Each starts from the step and,
 * should that fall short, goes on in sRGB: the light ink toward black, the
 * dark one toward white.
 */
export function accentInks(hex: string): { light: string | undefined; dark: string | undefined } {
  if (!validHex(hex)) return { light: undefined, dark: undefined };
  const accent = rgb(hex);
  const light = rgb(darkenTo(hex, INK_LIGHTNESS.light));
  const dark = rgb(lightenTo(hex, INK_LIGHTNESS.dark));
  // The wash darkens white and lightens the dark card, so it is the tighter
  // ground on either theme.
  const lightWash = mix(rgb(LIGHT_CARD), accent, SOFT_SHARE).map(Math.round) as Rgb;
  const darkWash = mix(rgb(DARK_CARD), accent, SOFT_SHARE).map(Math.round) as Rgb;
  const reads = (ink: Rgb, card: Rgb, wash: Rgb) => Math.min(ratio(ink, card), ratio(ink, wash)) >= MIN_TEXT_CONTRAST;
  return {
    light: reads(light, rgb(LIGHT_CARD), lightWash) ? undefined : pushed(light, rgb(BLACK), lightWash),
    dark: reads(dark, rgb(DARK_CARD), darkWash) ? undefined : pushed(dark, rgb(WHITE), darkWash),
  };
}

// --carbon-text of each theme, which a filled control's hover moves toward.
const LIGHT_TEXT = '#161616';
const DARK_TEXT = '#F4F4F4';
// The hover's step, color-mix(accent 85%, text) in index.css.
const HOVER_STEP = 0.15;

/**
 * accentHovers is the hover of an accent fill on each theme where index.css's
 * step toward the text colour would take the fill's ink under 4.5:1. That
 * happens on the mid-tones, where the ink only just clears the fill: a grey
 * such as #777777 carries a black ink, and on the light theme its hover darkens
 * toward that black. The hover then takes the same step away from the ink,
 * which only adds contrast. Undefined means the stylesheet's step holds, as it
 * does for every preset.
 */
export function accentHovers(hex: string): { light: string | undefined; dark: string | undefined } {
  if (!validHex(hex)) return { light: undefined, dark: undefined };
  const fill = rgb(hex);
  const ink = rgb(contrastOn(hex));
  const away = luminance(ink) > luminance(fill) ? rgb(BLACK) : rgb(WHITE);
  const hover = (text: string) => {
    const toward = mix(fill, rgb(text), HOVER_STEP).map(Math.round) as Rgb;
    return ratio(ink, toward) >= MIN_TEXT_CONTRAST ? undefined : hexOf(mix(fill, away, HOVER_STEP));
  };
  return { light: hover(LIGHT_TEXT), dark: hover(DARK_TEXT) };
}
