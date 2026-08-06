// Appearance is the set of looks the user owns: how rounded the interface is,
// and what colour it uses for activity — one accent, or a palette handed out by
// position. All of it is applied to the document root, so every component picks
// it up through the same tokens it already reads and nothing has to be told
// about the change.
//
// This file stays free of React on purpose: it is the piece a sibling app
// copies, and a design language should not arrive with a framework attached.
// The React binding is the small hook in useRainbow.ts.

export type Shape = 'round' | 'soft' | 'square';

export const SHAPES: Shape[] = ['round', 'soft', 'square'];

/** The built-in accent. Empty in settings means this. */
export const DEFAULT_ACCENT = '#E9A83C';

/**
 * ACCENTS are the presets offered in the picker. A free colour field sits
 * beside them, so this list is a shortcut rather than a restriction.
 */
export const ACCENTS: { name: string; hex: string }[] = [
  { name: 'Gold', hex: '#E9A83C' },
  { name: 'Ember', hex: '#E2703A' },
  { name: 'Crimson', hex: '#D9534F' },
  { name: 'Moss', hex: '#5CA271' },
  { name: 'Steel', hex: '#5B8DBE' },
  { name: 'Iris', hex: '#8C7AE6' },
  { name: 'Rose', hex: '#D2688F' },
  { name: 'Slate', hex: '#8A8F98' },
];

/**
 * RAINBOW is the default palette: a full turn of the wheel, but tuned to the
 * same warm, slightly dusty register as the accent presets, so switching the
 * mode on changes how much colour there is and not which family it belongs to.
 * The length is fixed — colours are handed out by position, so a palette that
 * could grow would re-colour every existing row the moment one was added.
 */
export const RAINBOW: string[] = [
  '#D9534F', // crimson
  '#E2703A', // ember
  '#E9A83C', // gold
  '#5CA271', // moss
  '#3FA9A6', // teal
  '#5B8DBE', // steel
  '#8C7AE6', // iris
  '#D2688F', // rose
];

export interface RainbowState {
  on: boolean;
  /** Rest neutral, colour on hover, keep the colour on the active item. */
  reactive: boolean;
  /** Offset the palette by seed, so a run does not always start on crimson. */
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

/** applyShape sets the attribute the radius tokens key off. */
export function applyShape(shape: Shape | string | undefined): void {
  const s = SHAPES.includes(shape as Shape) ? (shape as Shape) : 'round';
  document.documentElement.setAttribute('data-shape', s);
}

/**
 * applyAccent overrides the accent tokens, or clears the override so the
 * theme's own gold comes back. The contrast colour is computed rather than
 * configured: a light accent with white text on it is unreadable, and asking
 * the user to pick a second colour to fix the first one is not a setting, it is
 * a trap.
 */
export function applyAccent(hex: string | undefined): void {
  const root = document.documentElement.style;
  if (!valid(hex)) {
    root.removeProperty('--accent');
    root.removeProperty('--accent-contrast');
    root.removeProperty('--accent-soft');
    return;
  }
  const { r, g, b } = parse(hex);
  root.setProperty('--accent', hex);
  root.setProperty('--accent-contrast', contrastOn(hex));
  root.setProperty('--accent-soft', `rgba(${r}, ${g}, ${b}, 0.14)`);
}

// ---------------------------------------------------------------------------
// Rainbow
//
// The live state is module-level because it is a property of the document, not
// of any one component: the sidebar and the download list must agree on which
// colour position three is, and they never meet in the tree. Readers subscribe
// instead of being handed a prop through six intermediate components.
// ---------------------------------------------------------------------------

let state: RainbowState = RAINBOW_OFF;
const listeners = new Set<() => void>();

/** rainbowState is the current snapshot. Stable identity between changes. */
export function rainbowState(): RainbowState {
  return state;
}

export function subscribeRainbow(fn: () => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

/**
 * applyRainbow stores the new state, mirrors it onto the document root and
 * wakes the readers. The custom properties are set even when the mode is off so
 * that a stylesheet can reference `--rb-3` without having to know; the
 * `data-rainbow` attribute is what actually turns the look on.
 */
export function applyRainbow(next: Partial<RainbowState> | undefined): void {
  const merged: RainbowState = { ...RAINBOW_OFF, ...next };
  merged.palette = usablePalette(merged.palette);
  merged.seed = Number.isFinite(merged.seed) ? Math.abs(Math.trunc(merged.seed)) % RAINBOW.length : 0;
  state = merged;

  const root = document.documentElement;
  for (let i = 0; i < RAINBOW.length; i++) {
    root.style.setProperty(`--rb-${i}`, rainbowAt(i));
  }
  if (!merged.on) root.removeAttribute('data-rainbow');
  else root.setAttribute('data-rainbow', merged.reactive ? 'reactive' : 'on');

  for (const fn of listeners) fn();
}

/**
 * rainbowAt is the colour at a position, rotation applied. It answers even when
 * the mode is off, because the settings page has to show the palette it is
 * editing.
 */
export function rainbowAt(i: number): string {
  const p = state.palette;
  const off = state.rotate ? state.seed : 0;
  const n = ((Math.trunc(i) % p.length) + p.length) % p.length;
  return p[(n + off) % p.length];
}

/**
 * rainbowColor is what a component asks for: the colour this item should use,
 * or undefined when the mode is off and the single accent applies. Returning
 * undefined rather than the accent keeps the accent in CSS, where a theme
 * change still reaches it.
 */
export function rainbowColor(i: number): string | undefined {
  return state.on ? rainbowAt(i) : undefined;
}

/**
 * hueVars are the inline custom properties an element carrying a palette
 * position sets on itself. The matching `.glim-hue` rules in index.css decide
 * whether the hue is shown at rest or held back until hover, so a component
 * only has to say which colour it owns, never which mode is active.
 */
export function hueVars(hex: string | undefined): Record<string, string> {
  if (!valid(hex)) return {};
  const { r, g, b } = parse(hex);
  return {
    '--item-hue': hex,
    '--item-hue-ink': contrastOn(hex),
    '--item-hue-soft': `rgba(${r}, ${g}, ${b}, 0.14)`,
    // The wash covers a whole row, so it sits far below the soft tint: at 14%
    // eight rows of eight hues stop being a download list and start being a
    // colour chart.
    '--item-hue-wash': `rgba(${r}, ${g}, ${b}, 0.07)`,
  };
}

/**
 * rainbowFromSettings maps the server's flat fields onto the state this module
 * keeps. The parameter is structural rather than the imported Settings type so
 * this file can be lifted into a sibling app unchanged.
 */
export function rainbowFromSettings(s: {
  rainbow?: boolean;
  rainbowReactive?: boolean;
  rainbowRotate?: boolean;
  rainbowSeed?: number;
  rainbowPalette?: string[] | null;
}): RainbowState {
  return {
    on: !!s.rainbow,
    reactive: !!s.rainbowReactive,
    rotate: !!s.rainbowRotate,
    seed: s.rainbowSeed ?? 0,
    palette: usablePalette(s.rainbowPalette ?? undefined),
  };
}

/** A palette is taken only in full — see the matching rule on the server. */
function usablePalette(p: string[] | undefined): string[] {
  if (!p || p.length !== RAINBOW.length || !p.every(valid)) return RAINBOW;
  return p;
}

/** contrastOn is black or white, whichever is readable on the given colour. */
export function contrastOn(hex: string): string {
  if (!valid(hex)) return '#FFFFFF';
  const { r, g, b } = parse(hex);
  return luminance(r, g, b) > 0.55 ? '#17130E' : '#FFFFFF';
}

function valid(hex: string | undefined): hex is string {
  return !!hex && /^#[0-9a-fA-F]{6}$/.test(hex);
}

function parse(hex: string): { r: number; g: number; b: number } {
  const n = parseInt(hex.slice(1), 16);
  return { r: (n >> 16) & 255, g: (n >> 8) & 255, b: n & 255 };
}

/**
 * luminance is the perceptual brightness used to decide black or white on top.
 * The sRGB channels are linearised first, because the raw values overstate how
 * bright blue is and understate green, which is exactly the case that produces
 * unreadable buttons.
 */
function luminance(r: number, g: number, b: number): number {
  const lin = (c: number) => {
    const v = c / 255;
    return v <= 0.04045 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4);
  };
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
}

/**
 * Appearance is mirrored into localStorage purely so the first paint after a
 * reload is already right. The settings on the server stay the source of truth;
 * this only avoids a flash of the default look while they are being fetched.
 */
const CACHE = 'kl-appearance';

interface Cached {
  shape?: string;
  accent?: string;
  rainbow?: RainbowState;
}

export function cacheAppearance(shape: string, accent: string, rainbow?: RainbowState): void {
  try {
    localStorage.setItem(CACHE, JSON.stringify({ shape, accent, rainbow } satisfies Cached));
  } catch {
    // A browser with storage disabled simply pays one flash per load.
  }
}

/** Applied at boot, before React renders anything. */
export function applyCachedAppearance(): void {
  try {
    const raw = localStorage.getItem(CACHE);
    if (!raw) {
      applyShape('round');
      applyRainbow(undefined);
      return;
    }
    const { shape, accent, rainbow } = JSON.parse(raw) as Cached;
    applyShape(shape);
    applyAccent(accent);
    applyRainbow(rainbow);
  } catch {
    applyShape('round');
    applyRainbow(undefined);
  }
}
