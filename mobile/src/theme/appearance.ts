// The framework-free half of GlimStone's appearance module, taken across.
//
// The shared reference (github.com/junkerderprovinz/glimstone,
// reference/appearance.ts) is deliberately free of any UI framework so a
// sister app can adopt it unchanged - but only half of it is: the constants
// and the pure functions here travel as they are, while the other half applies
// its answer by setting CSS custom properties on <html>, which React Native
// has neither of. That half lives in AppearanceContext instead, and this file
// stays a straight copy so the two never disagree about what "Sunflower" is.
//
// Anything below that is copied is copied VERBATIM on purpose. Re-picking a
// colour by eye, or rounding a luminance constant, is exactly how two apps
// that both claim one design language stop looking like one product.

export type Shape = 'round' | 'soft' | 'square';

export const SHAPES: Shape[] = ['round', 'soft', 'square'];

/**
 * The accent before anyone touches the picker. A fresh install of every app in
 * the family opens in this colour, so they look like one product from the
 * first launch rather than after a settings visit.
 */
export const DEFAULT_ACCENT = '#FCC419';

// All eight, where this list carried five. The web UI and the extension have
// offered Orange, Teal and Pink the whole time, so an accent chosen in a
// browser had no swatch to be marked on here - it simply appeared as "none of
// these are selected". Found while giving the row its picker; jdp did not
// report it, and it is the same drift as the palette row missing entirely.
export const ACCENTS: { name: string; hex: string }[] = [
  { name: 'Sunflower', hex: '#FCC419' },
  { name: 'Blue', hex: '#1D99F3' },
  { name: 'Green', hex: '#6FDC8C' },
  { name: 'Red', hex: '#FF8389' },
  { name: 'Purple', hex: '#BE95FF' },
  { name: 'Orange', hex: '#FF832B' },
  { name: 'Teal', hex: '#3DDBD9' },
  { name: 'Pink', hex: '#FF7EB6' },
];

/**
 * accentSlot is which of the eight preset positions a colour belongs to.
 *
 * An exact preset answers with itself; anything else answers with the nearest,
 * and that is what gives a hand-mixed accent a home: the row marks one slot
 * whatever the colour is, and that slot wears the live value and re-opens the
 * picker on it. Without it, a colour off the presets is in force everywhere and
 * drawn nowhere.
 *
 * Plain squared RGB distance, deliberately not a perceptual metric: it only has
 * to be stable and unsurprising for eight widely separated hues. Kept identical
 * to the extension's accentSlot (src/appearance.js) and the web's (Look.tsx) so
 * one colour lands in one slot on all three.
 */
export function accentSlot(hex: string): number {
  const rgb = (h: string) => {
    const n = parseInt(h.slice(1), 16);
    return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
  };
  if (!/^#[0-9a-fA-F]{6}$/.test(hex)) return 0;
  const [r, g, b] = rgb(hex);
  let best = 0;
  let bestD = Infinity;
  ACCENTS.forEach((a, i) => {
    const [pr, pg, pb] = rgb(a.hex);
    const d = (r - pr) ** 2 + (g - pg) ** 2 + (b - pb) ** 2;
    if (d < bestD) {
      bestD = d;
      best = i;
    }
  });
  return best;
}

/**
 * RAINBOW is the default palette: a full turn of the wheel, but tuned to the
 * same warm, slightly dusty register as the accent presets, so switching the
 * mode on changes how much colour there is, not which family it belongs to.
 * The length is fixed - colours are handed out by position, so a palette that
 * could grow would re-colour every existing row the moment one was added.
 */
export const RAINBOW: string[] = [
  '#FF8389', // red 30
  '#FF832B', // orange 40
  '#FCC419', // sunflower - the default accent, so one row always matches it
  '#6FDC8C', // green 30
  '#3DDBD9', // teal 30
  '#1D99F3', // blue
  '#BE95FF', // purple 30
  '#FF7EB6', // magenta 30
];

export interface RainbowState {
  on: boolean;
  reactive: boolean;
  rotate: boolean;
  seed: number;
  palette: string[];
}

export const RAINBOW_OFF: RainbowState = {
  on: false,
  reactive: false,
  rotate: false,
  seed: 0,
  palette: RAINBOW,
};

/**
 * rainbowAt is the colour for one list POSITION.
 *
 * By position and not by a hash of the item's id, which sounds better and is
 * not: a hash keeps a row's colour when the rows above it finish, but with
 * three rows and eight colours it routinely gives two neighbours the same one -
 * which is the single thing this mode exists to prevent.
 */
export function rainbowAt(state: RainbowState, i: number): string {
  const p = state.palette.length > 0 ? state.palette : RAINBOW;
  const off = state.rotate ? state.seed : 0;
  const n = ((Math.trunc(i) % p.length) + p.length) % p.length;
  const color = p[(n + off) % p.length];
  if (color === undefined) throw new Error('rainbowAt: palette is empty');
  return color;
}

/**
 * rainbowColor is what a component asks for: the colour this item should use,
 * or undefined when the mode is off and the single accent applies. Undefined
 * rather than the accent, so the caller keeps reading the accent from the
 * theme and a theme change still reaches it.
 */
export function rainbowColor(state: RainbowState, i: number): string | undefined {
  return state.on ? rainbowAt(state, i) : undefined;
}

/** contrastOn is the ink to put ON a colour - black or white, decided rather
 *  than configured. Asking for a second colour to make the first one readable
 *  is not a setting, it is a trap. */
export function contrastOn(hex: string): string {
  if (!valid(hex)) return '#FFFFFF';
  const { r, g, b } = parse(hex);
  // Carbon's own ink, not a warm near-black: on a yellow accent a
  // brown-tinted black reads as a smudge.
  return luminance(r, g, b) > 0.55 ? '#161616' : '#FFFFFF';
}

export function valid(hex: string | undefined): hex is string {
  return !!hex && /^#[0-9a-fA-F]{6}$/.test(hex);
}

function parse(hex: string): { r: number; g: number; b: number } {
  const n = parseInt(hex.slice(1), 16);
  return { r: (n >> 16) & 255, g: (n >> 8) & 255, b: n & 255 };
}

/**
 * luminance is the perceptual brightness used to decide black or white on top.
 * The sRGB channels are linearised first, because the raw values overstate how
 * bright blue is and understate green - which is exactly the case that produces
 * an unreadable button.
 */
function luminance(r: number, g: number, b: number): number {
  const lin = (c: number) => {
    const v = c / 255;
    return v <= 0.04045 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4);
  };
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
}

/** softOn is the wash a row carries when it owns a colour - the same 12-14%
 *  the web build lays down, expressed as rgba because React Native has no
 *  colour-mix. */
export function softOn(hex: string, alpha = 0.14): string {
  if (!valid(hex)) return 'transparent';
  const { r, g, b } = parse(hex);
  return `rgba(${r}, ${g}, ${b}, ${alpha})`;
}

/** The appearance an instance reports, which is where these settings live -
 *  see AppearanceContext for why the instance leads and the app may override. */
export interface InstanceAppearance {
  shape?: string;
  accent?: string;
  rainbow?: boolean;
  rainbowReactive?: boolean;
  rainbowRotate?: boolean;
  rainbowSeed?: number;
  rainbowPalette?: string[];
}

/**
 * rainbowFromSettings turns what an instance reports into the state this app
 * draws with.
 *
 * The palette is all-or-nothing: seven good colours plus one that is not a
 * colour is not an 87%-safe palette, it is an invisible row. The server
 * enforces the same rule; this repeats it because a client that trusts a
 * server's validation is a client that breaks when the server changes.
 */
export function rainbowFromSettings(s: InstanceAppearance | undefined): RainbowState {
  const p = s?.rainbowPalette;
  const palette = Array.isArray(p) && p.length === RAINBOW.length && p.every(valid) ? p : RAINBOW;
  // The palette travels even when the mode is OFF, and that is not tidiness.
  //
  // Returning RAINBOW_OFF wholesale threw the instance's own eight colours away
  // and handed back the defaults, which was invisible for as long as nothing
  // drew them - the mode being off makes rainbowColor answer undefined either
  // way. The settings screen now shows the palette as a dimmed row, so an
  // instance with a custom palette would display the DEFAULT eight while the
  // mode was off and eight different ones the moment it went on. `on` is what
  // switches the mode; the palette is only ever data.
  return {
    on: !!s?.rainbow,
    reactive: !!s?.rainbowReactive,
    rotate: !!s?.rainbowRotate,
    seed: Number.isFinite(s?.rainbowSeed) ? Math.trunc(s?.rainbowSeed as number) : 0,
    palette,
  };
}

export function asShape(v: string | undefined): Shape | undefined {
  return SHAPES.includes(v as Shape) ? (v as Shape) : undefined;
}
