// The framework-free half of GlimStone's appearance module, taken across.
//
// The shared reference (github.com/junkerderprovinz/glimstone,
// reference/appearance.ts) is free of any UI framework, but only half of it
// travels: the constants and the pure functions here are a straight copy, while
// the half that applies its answer through CSS custom properties on <html> has
// no equivalent in React Native and lives in AppearanceContext instead.
//
// Everything copied is copied as it stands. Re-picking a colour by eye or
// rounding a luminance constant is how two apps on one design language stop
// looking like one product.

import { colourAt, type Loop } from './discoLoop';

/**
 * The corner shapes, a wire format shared with the instance's settings and
 * storage. `leaf` is a real shape that no picker offers; leafTap reveals it.
 */
export type Shape = 'round' | 'soft' | 'square' | 'leaf';

/** The shapes a picker shows. */
export const SHAPES: Shape[] = ['round', 'soft', 'square'];

/**
 * The shapes a stored value may hold. Validate against this and populate a
 * picker from SHAPES, or a found leaf forgets itself on reload.
 */
export const SHAPES_STORED: Shape[] = [...SHAPES, 'leaf'];

/** Only reaches somebody with no stored shape; a stored choice stays. */
export const DEFAULT_SHAPE: Shape = 'soft';

/** How many taps on `square`, once it is chosen, reveal the leaf. */
export const LEAF_TAPS = 5;

/**
 * leafTap counts the gesture that reveals the leaf, the storm's gesture on the
 * shape picker: with the shape at `square`, tap `square` five more times.
 * Tapping another shape resets the count. As with the storm, the caller keeps
 * `found` and the count in the state of the screen that found it, never in
 * storage.
 *
 * Returns the shape to switch to, or undefined when the tap was not the fifth.
 */
export function leafTap(state: { taps: number }, tapped: string, current: string): Shape | undefined {
  if (tapped !== 'square' || current !== 'square') {
    state.taps = 0;
    return undefined;
  }
  state.taps += 1;
  if (state.taps < LEAF_TAPS) return undefined;
  state.taps = 0;
  return 'leaf';
}

/**
 * The accent before anyone touches the picker. A fresh install of every app in
 * the family opens in this colour, so they look like one product from the
 * first launch rather than after a settings visit.
 */
export const DEFAULT_ACCENT = '#FCC419';

// All eight the web UI and the extension offer. A shorter list here leaves an
// accent chosen in a browser with no swatch to be marked on.
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
 * Plain squared RGB distance rather than a perceptual metric: it only has to be
 * stable and unsurprising for eight widely separated hues. Identical to the
 * extension's accentSlot (src/appearance.js) and the web's (Look.tsx), so one
 * colour lands in one slot on all three.
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
 * The length is fixed: colours are handed out by position, so a palette that
 * could grow would re-colour every existing row the moment one was added.
 */
export const RAINBOW: string[] = [
  '#FF8389', // red 30
  '#FF832B', // orange 40
  '#FCC419', // sunflower, the default accent, so one row always matches it
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
 * rainbowAt is the colour for one list position.
 *
 * By position rather than by a hash of the item's id: a hash keeps a row's
 * colour when the rows above it finish, but with three rows and eight colours
 * it routinely gives two neighbours the same one, which is what this mode
 * exists to prevent.
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

// Disco, the colour engine's easter egg from reference/disco.ts: while it is
// on, every hued element glides along the palette together, round the colour
// wheel. A phone has no root variables for a frame loop to write, so the walk
// is a theme that renders again on a timer (AppearanceContext), over the loop
// in discoLoop.ts, which is the reference's file as it is.

/** The walk covers one palette colour's worth of loop every 2.4 seconds, a
 *  full turn of eight in 19.2. Stepping, it moves one colour on at the same
 *  interval, well under the 3Hz flicker threshold. */
export const DISCO_TICK_MS = 2400;

/** How often the glide redraws: ten times a second is smooth enough for a
 *  colour that takes 2.4 seconds to arrive, and cheap enough for a whole
 *  screen of hued controls. */
export const DISCO_FRAME_MS = 100;

/**
 * The colour disco has walked position `i` to. `travelled` is how much of a
 * full turn the walk has covered, from 0 up to 1, and `start` the palette
 * entry position 0 sits on at rest. Stepping, the walk jumps from palette
 * colour to palette colour instead of gliding between them.
 */
export function walkedColour(loop: Loop, start: number, travelled: number, i: number, steps: boolean): string {
  const n = loop.palette.length;
  const at = (((Math.trunc(i) + start) % n) + n) % n;
  if (steps) return loop.palette[(at + Math.floor(travelled * n)) % n]!;
  return colourAt(loop, loop.at[at]! + travelled * loop.at[n]!);
}

/** Turn-ons of rainbow mode that unlock it. */
export const DISCO_UNLOCK_TURN_ONS = 5;

/** The longest pause between two turn-ons of one run. Without it, somebody
 *  comparing the app with and without the rainbow over a minute unlocks a
 *  mode they never went looking for. */
export const DISCO_UNLOCK_WINDOW_MS = 3000;

/**
 * discoTap counts the unlock gesture: five turn-ons of rainbow mode, each
 * within DISCO_UNLOCK_WINDOW_MS of the last, and true on the fifth. Turn-ons
 * rather than presses, so the gesture ends with the rainbow on, the one state
 * in which the reward can be seen. The count lives with the caller and never
 * in storage, or finding the mode once would leave its switch in the settings
 * for good.
 */
export function discoTap(state: { taps: number; last: number }, turnedOn: boolean, now: number): boolean {
  if (!turnedOn) return false;
  const gap = now - state.last;
  state.last = now;
  state.taps = state.taps > 0 && gap <= DISCO_UNLOCK_WINDOW_MS ? state.taps + 1 : 1;
  if (state.taps < DISCO_UNLOCK_TURN_ONS) return false;
  state.taps = 0;
  return true;
}

/** contrastOn is the ink to put on a colour, black or white, decided rather
 *  than configured: asking for a second colour to make the first one readable
 *  is a trap, not a setting. */
export function contrastOn(hex: string): string {
  if (!valid(hex)) return '#FFFFFF';
  const { r, g, b } = parse(hex);
  // Carbon's own ink rather than a warm near-black, which reads as a smudge on
  // a yellow accent.
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
 * bright blue is and understate green, which is what produces an unreadable
 * button.
 */
function luminance(r: number, g: number, b: number): number {
  const lin = (c: number) => {
    const v = c / 255;
    return v <= 0.04045 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4);
  };
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
}

/** softOn is the wash a row carries when it owns a colour, the same 12 to 14%
 *  the web build lays down, as rgba because React Native has no colour-mix. */
export function softOn(hex: string, alpha = 0.14): string {
  if (!valid(hex)) return 'transparent';
  const { r, g, b } = parse(hex);
  return `rgba(${r}, ${g}, ${b}, ${alpha})`;
}

/** The appearance an instance reports, which is where these settings live. See
 *  AppearanceContext for why the instance leads and the app may override. */
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
  // The palette travels even while the mode is off. The settings screen shows
  // it as a dimmed row, so handing back RAINBOW_OFF wholesale would display the
  // default eight while the mode was off and the instance's eight the moment it
  // went on. `on` switches the mode; the palette is data.
  return {
    on: !!s?.rainbow,
    reactive: !!s?.rainbowReactive,
    rotate: !!s?.rainbowRotate,
    seed: Number.isFinite(s?.rainbowSeed) ? Math.trunc(s?.rainbowSeed as number) : 0,
    palette,
  };
}

export function asShape(v: string | undefined): Shape | undefined {
  return SHAPES_STORED.includes(v as Shape) ? (v as Shape) : undefined;
}
