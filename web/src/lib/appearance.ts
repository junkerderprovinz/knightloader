// Appearance is the set of looks the user owns: what shape the corners take,
// and which colour marks activity, one accent or a palette handed out by
// position. Everything is applied to the document root, so components pick it
// up through the tokens they already read.
//
// No React here: this is the file a sibling app copies. The React binding is
// useRainbow.ts. The reference copy lives at
// https://github.com/junkerderprovinz/glimstone/blob/main/reference/appearance.ts

import { accentInks, contrastOn, validHex as valid } from './contrast';

export { contrastOn };

/**
 * The corner shapes. Like the motion levels these strings are a wire format:
 * they go into `data-shape`, index.css, the server's settings and the cache,
 * and what the user reads comes from the catalogues.
 *
 * `leaf` is a real shape (see `data-shape="leaf"` in index.css) that no picker
 * offers; `leafTap` reveals it.
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

/** The built-in accent, shared with the sibling apps. Empty in settings means this. */
export const DEFAULT_ACCENT = '#FCC419';

/**
 * The picker's presets, the same eight the sibling apps offer, so "Blue" is
 * the same blue everywhere. A free colour field sits beside them.
 */
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
 * The default palette: a full turn of the wheel in the same register as the
 * accent presets. Its length is fixed, since colours are handed out by
 * position and a longer palette would re-colour every existing row.
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
  const s = SHAPES_STORED.includes(shape as Shape) ? (shape as Shape) : DEFAULT_SHAPE;
  document.documentElement.setAttribute('data-shape', s);
  armShapeTransition();
}

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

let shapeTransitionArmed = false;

// armShapeTransition turns on the shape morph (.glim-shape-transitions)
// two frames after the first call, so the first paint never animates and every
// later shape change does.
function armShapeTransition(): void {
  if (shapeTransitionArmed) return;
  shapeTransitionArmed = true;
  requestAnimationFrame(() => {
    requestAnimationFrame(() => {
      document.documentElement.classList.add('glim-shape-transitions');
    });
  });
}

/**
 * The accent mode that takes its colour from the device (GlimStone's rule
 * 44): the desktop's accent in the desktop app, CSS AccentColor in Firefox and
 * Safari. The instance holds one accent for every client, so the choice stays
 * in this browser, as the word and never the colour, and leaving it hands the
 * page back to the instance's accent.
 */
export const SYSTEM_ACCENT = 'system';

const SYSTEM_CHOICE = 'kl-accent-system';
const SYSTEM_COLOUR = 'kl-system-accent';

let instanceAccent: string | undefined;
let followsSystem = false;
let systemColour: string | undefined;
const systemListeners = new Set<() => void>();

/**
 * resolveAccent is the colour the page wears: the device's while the wand is
 * chosen and the device has one, else the instance's, else undefined for the
 * theme's gold.
 */
export function resolveAccent(instance: string | undefined, follows: boolean, system: string | undefined): string | undefined {
  if (follows && valid(system)) return system;
  return valid(instance) ? instance : undefined;
}

/** applyAccent takes the instance's accent and paints whatever is in force. */
export function applyAccent(hex: string | undefined): void {
  instanceAccent = hex;
  paintAccent(resolveAccent(instanceAccent, followsSystem, systemColour));
}

/** followsSystemAccent says whether the wand is chosen in this browser. */
export function followsSystemAccent(): boolean {
  return followsSystem;
}

/** setFollowsSystemAccent chooses or leaves the wand in this browser alone. */
export function setFollowsSystemAccent(on: boolean): void {
  followsSystem = on;
  store(SYSTEM_CHOICE, on ? SYSTEM_ACCENT : undefined);
  paintAccent(resolveAccent(instanceAccent, followsSystem, systemColour));
  for (const fn of systemListeners) fn();
}

/**
 * setSystemAccent feeds the colour the device reports, or undefined where it
 * has none, and repaints while the wand is chosen. The colour is remembered
 * in this browser, so the first frame after a reload is already in it.
 */
export function setSystemAccent(hex: string | undefined): void {
  const next = valid(hex) ? hex : undefined;
  if (next === systemColour) return;
  systemColour = next;
  store(SYSTEM_COLOUR, next);
  if (followsSystem) paintAccent(resolveAccent(instanceAccent, followsSystem, systemColour));
  for (const fn of systemListeners) fn();
}

/** systemAccent is the device's colour, or undefined, which leaves the wand out. */
export function systemAccent(): string | undefined {
  return systemColour;
}

export function subscribeSystemAccent(fn: () => void): () => void {
  systemListeners.add(fn);
  return () => systemListeners.delete(fn);
}

function store(key: string, value: string | undefined): void {
  try {
    if (value) localStorage.setItem(key, value);
    else localStorage.removeItem(key);
  } catch {
    // Without storage the choice holds until the page closes.
  }
}

/**
 * paintAccent overrides the accent tokens, or clears the override so the
 * theme's gold comes back. The ink on the fill is computed, so a light accent
 * never ends up with white text on it. index.css derives the accent as text
 * with color-mix, and where that mix would fall short of 4.5:1 on a theme's
 * card, the theme's own ink is set here as --accent-ink-light or
 * --accent-ink-dark.
 */
function paintAccent(hex: string | undefined): void {
  const root = document.documentElement.style;
  const inks = valid(hex) ? accentInks(hex) : { light: undefined, dark: undefined };
  for (const [theme, ink] of Object.entries(inks)) {
    if (ink) root.setProperty(`--accent-ink-${theme}`, ink);
    else root.removeProperty(`--accent-ink-${theme}`);
  }
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

// The rainbow state is module-level because it belongs to the document: the
// sidebar and the download list must agree on which colour position three is,
// and they never meet in the component tree.

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

// The mode as of the last applyRainbow call, so that only a real mode change
// triggers the colour wipe, not the first apply or a repeat of the same state.
let lastRainbowMode: 'off' | 'on' | 'reactive' | undefined;
let wipeTimeout: ReturnType<typeof setTimeout> | undefined;

/**
 * applyRainbow stores the new state, mirrors it onto the document root and
 * wakes the readers. The `--rb-N` and `--rb-ink-N` properties are set even when
 * the mode is off, because hueVars() points every hued element at them;
 * `data-rainbow` turns the look on.
 */
export function applyRainbow(next: Partial<RainbowState> | undefined): void {
  const merged: RainbowState = { ...RAINBOW_OFF, ...next };
  merged.palette = usablePalette(merged.palette);
  merged.seed = Number.isFinite(merged.seed) ? Math.abs(Math.trunc(merged.seed)) % RAINBOW.length : 0;
  state = merged;

  const root = document.documentElement;
  for (let i = 0; i < RAINBOW.length; i++) {
    root.style.setProperty(`--rb-${i}`, rainbowAt(i));
    root.style.setProperty(`--rb-ink-${i}`, contrastOn(rainbowAt(i)));
  }
  const mode: 'off' | 'on' | 'reactive' = !merged.on ? 'off' : merged.reactive ? 'reactive' : 'on';
  if (mode === 'off') root.removeAttribute('data-rainbow');
  else root.setAttribute('data-rainbow', mode);

  if (lastRainbowMode !== undefined && lastRainbowMode !== mode) {
    triggerColourWipe();
  }
  lastRainbowMode = mode;

  for (const fn of listeners) fn();
}

// triggerColourWipe puts .glim-colour-wipe on the root for the length of
// --motion-wipe-dur, so every hued colour fades over one window instead of
// snapping. The duration is read from the live DOM, so it follows data-motion.
function triggerColourWipe(): void {
  const root = document.documentElement;
  root.classList.add('glim-colour-wipe');
  if (wipeTimeout !== undefined) clearTimeout(wipeTimeout);
  const raw = getComputedStyle(root).getPropertyValue('--motion-wipe-dur').trim();
  const ms = parseFloat(raw);
  wipeTimeout = setTimeout(() => {
    root.classList.remove('glim-colour-wipe');
    wipeTimeout = undefined;
  }, Number.isFinite(ms) ? ms : 0);
}

/**
 * rainbowAt is the colour at a position, rotation applied. It answers even
 * when the mode is off, because the settings page shows the palette it edits.
 */
export function rainbowAt(i: number): string {
  const p = state.palette;
  const off = state.rotate ? state.seed : 0;
  const n = ((Math.trunc(i) % p.length) + p.length) % p.length;
  const color = p[(n + off) % p.length];
  if (color === undefined) {
    // usablePalette never lets the palette go empty; this satisfies the type checker.
    throw new Error('rainbowAt: palette is empty');
  }
  return color;
}

// The buttons that carry rule 40's tint, whatever else an app's own buttons wear.
const TINTED = '.glim-btn-neutral, .glim-btn-subtle, .glim-btn-bare';

/**
 * takePageHue gives a button the palette position of its place among the
 * page's buttons, so under rainbow two buttons side by side light up in
 * different colours. Call it when the pointer or the keyboard reaches the
 * button: the order changes with the page, and only the hover and focus look
 * read the tint.
 */
export function takePageHue(el: HTMLElement): void {
  if (!state.on) {
    el.style.removeProperty('--btn-tint');
    return;
  }
  const i = Array.prototype.indexOf.call(document.querySelectorAll(TINTED), el);
  const n = ((i % RAINBOW.length) + RAINBOW.length) % RAINBOW.length;
  el.style.setProperty('--btn-tint', `color-mix(in srgb, var(--rb-${n}) var(--ink-mix), black)`);
}

/**
 * rainbowColor is the colour an item should use, or undefined when the mode is
 * off. Undefined rather than the accent keeps the accent in CSS, where a theme
 * change still reaches it.
 */
export function rainbowColor(i: number): string | undefined {
  return state.on ? rainbowAt(i) : undefined;
}

/**
 * hueVars are the inline custom properties an element at palette position `i`
 * sets on itself; the `.glim-hue` rules in index.css decide when the hue shows.
 * They point at the root's `--rb-*` properties rather than holding a colour,
 * so a palette edit, a rotation or disco's walk lands on the root alone and no
 * component has to render again. The class and these properties always travel
 * together, which hueStyle() in components/ui.tsx takes care of.
 */
export function hueVars(i: number): Record<string, string> {
  const n = ((Math.trunc(i) % RAINBOW.length) + RAINBOW.length) % RAINBOW.length;
  const hue = `var(--rb-${n})`;
  return {
    '--item-hue': hue,
    '--item-hue-ink': `var(--rb-ink-${n})`,
    '--item-hue-soft': `color-mix(in srgb, ${hue} 22%, transparent)`,
    // A wash covers a whole row, so it stays below the soft tint. Much less
    // than 16% and the mode looks like it does nothing.
    '--item-hue-wash': `color-mix(in srgb, ${hue} 16%, transparent)`,
    // A small badge has no neighbouring rows to repeat its colour and reads as
    // grey at the row wash's strength.
    '--item-hue-badge': `color-mix(in srgb, ${hue} 50%, transparent)`,
    // The focus ring follows the position too, so no gold ring appears around
    // a teal tab.
    '--item-hue-ring': `color-mix(in srgb, ${hue} 55%, transparent)`,
  };
}

const counted = new WeakMap<Element, number>();
let countQueued = false;

/**
 * countSelectorHues hands every selector that sets a value its palette
 * position. Such a selector marks its strip with data-hue-selector and asks
 * for a count whenever it is drawn or leaves. The strips then count through
 * the palette in the order they stand on the page, and those inside a window
 * count on their own, so no page numbers its selectors by hand. The count
 * runs once a turn, before the browser paints, and writes hueVars() onto each
 * strip, from where the segments inherit them.
 */
export function countSelectorHues(): void {
  if (countQueued) return;
  countQueued = true;
  queueMicrotask(() => {
    countQueued = false;
    const next = new Map<Element | Document, number>();
    for (const strip of document.querySelectorAll<HTMLElement>('[data-hue-selector]')) {
      const within = strip.closest('[aria-modal="true"]') ?? document;
      const i = next.get(within) ?? 0;
      next.set(within, i + 1);
      if (counted.get(strip) === i) continue;
      counted.set(strip, i);
      for (const [name, value] of Object.entries(hueVars(i))) strip.style.setProperty(name, value);
    }
  });
}

/**
 * rainbowFromSettings maps the server's flat fields onto this module's state.
 * The parameter is structural rather than the Settings type so the file can be
 * lifted into a sibling app unchanged.
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

/** A palette is taken only in full, as on the server. */
function usablePalette(p: string[] | undefined): string[] {
  if (!p || p.length !== RAINBOW.length || !p.every(valid)) return RAINBOW;
  return p;
}

function parse(hex: string): { r: number; g: number; b: number } {
  const n = parseInt(hex.slice(1), 16);
  return { r: (n >> 16) & 255, g: (n >> 8) & 255, b: n & 255 };
}

// Appearance is mirrored into localStorage only so the first paint after a
// reload is already right. The server's settings stay the source of truth.
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

/**
 * Applied at boot, before React renders anything. The motion level comes first,
 * so the first page entrance already runs at the chosen intensity.
 */
export function applyCachedAppearance(): void {
  applyMotion(readCachedMotionIntensity());
  try {
    followsSystem = localStorage.getItem(SYSTEM_CHOICE) === SYSTEM_ACCENT;
    const colour = localStorage.getItem(SYSTEM_COLOUR) ?? undefined;
    systemColour = valid(colour) ? colour : undefined;
    const raw = localStorage.getItem(CACHE);
    if (raw) {
      const { shape, accent, rainbow } = JSON.parse(raw) as Cached;
      applyShape(shape);
      applyAccent(accent);
      applyRainbow(rainbow);
    } else {
      applyShape(DEFAULT_SHAPE);
      applyRainbow(undefined);
    }
  } catch {
    applyShape(DEFAULT_SHAPE);
    applyRainbow(undefined);
  }
}

// Motion intensity is the third user-owned axis (GlimStone, "Motion
// intensity"). data-motion on <html> selects the duration and distance tokens
// index.css reads. Unlike shape and accent it is not a server setting and has
// its own localStorage key. applyCachedAppearance applies it before first
// paint and Look.tsx on every pick. The cache functions have no counterpart in
// the GlimStone reference, which persists nothing.

/**
 * The levels, quietest first. The strings are a wire format: they go into the
 * `data-motion` attribute, index.css matches them and localStorage stores
 * them. Labels come from the catalogues. `storm` is a real level that no
 * picker offers, so it is not in MOTION_LEVELS. The top visible level was
 * `full` before GlimStone 2.0.0; see MIGRATED_MOTION.
 */
export type Motion = 'off' | 'subtle' | 'wild' | 'storm';

/** What a picker shows. The storm is not in here; see stormTap below. */
export const MOTION_LEVELS: Motion[] = ['off', 'subtle', 'wild'];

/** What a stored value may be. A stored storm is kept, so the hidden level
 *  survives a reload. */
export const MOTION_STORED: Motion[] = [...MOTION_LEVELS, 'storm'];

/**
 * The middle level, because the top one is a statement rather than polish
 * (GlimStone 2.1.0). A changed default never reaches a stored choice, so
 * somebody who picked the top level keeps it. There is no "system" option,
 * because prefers-reduced-motion already gates every animation in index.css.
 */
export const DEFAULT_MOTION: Motion = 'subtle';

/**
 * applyMotion sets the attribute the motion tokens key off. Anything not in
 * MOTION_STORED falls back to the default, so a hand-edited storage entry
 * cannot put an unmatched value on <html>.
 */
export function applyMotion(motion: Motion | string | undefined): void {
  const m: Motion = MOTION_STORED.includes(motion as Motion) ? (motion as Motion) : DEFAULT_MOTION;
  document.documentElement.dataset.motion = m;
}

/** How many taps on the level already chosen open the one below the floor. */
export const STORM_TAPS = 5;

/**
 * stormTap counts taps towards revealing the storm level: set the motion to
 * the top level, then tap that same option five more times. Tapping any other
 * level resets the count.
 *
 * An easter egg that changes behaviour must be switchable back off and must
 * not become a permanent settings entry. So the storm option is offered while
 * it is chosen, and otherwise only while the settings screen stays open: the
 * caller keeps the tap count and the "found" flag in the screen's own state,
 * never in storage. The chosen value persists like any other.
 *
 * Returns the level to switch to, or undefined when the tap was not the fifth.
 */
export function stormTap(state: { taps: number }, tapped: string, current: string): Motion | undefined {
  const top = MOTION_LEVELS[MOTION_LEVELS.length - 1];
  if (tapped !== top || current !== top) {
    state.taps = 0;
    return undefined;
  }
  state.taps += 1;
  if (state.taps < STORM_TAPS) return undefined;
  state.taps = 0;
  return 'storm';
}

const MOTION_CACHE = 'kl-motion';

/** cacheMotionIntensity stores the level so the next load applies it before first paint. */
export function cacheMotionIntensity(m: Motion): void {
  try {
    localStorage.setItem(MOTION_CACHE, m);
  } catch {
    // A browser with storage disabled simply pays one flash per load.
  }
}

/**
 * Retired spellings and what they are now. `full` was renamed `wild` in
 * GlimStone 2.0.0 and is still in users' localStorage, where only somebody who
 * picked it put it, so it maps to the level they chose and not to
 * DEFAULT_MOTION. Not in MOTION_STORED: it is translated once and written
 * back.
 */
const MIGRATED_MOTION: Record<string, Motion> = { full: 'wild' };

/**
 * readCachedMotionIntensity returns the stored level, applied at boot by
 * applyCachedAppearance. Anything unexpected, including a storage error, gives
 * DEFAULT_MOTION. A retired spelling is rewritten in place, once per browser,
 * so the alias does not have to be kept forever.
 */
export function readCachedMotionIntensity(): Motion {
  try {
    const raw = localStorage.getItem(MOTION_CACHE);
    if (raw !== null && raw in MIGRATED_MOTION) {
      const now = MIGRATED_MOTION[raw] as Motion;
      cacheMotionIntensity(now);
      return now;
    }
    return MOTION_STORED.includes(raw as Motion) ? (raw as Motion) : DEFAULT_MOTION;
  } catch {
    return DEFAULT_MOTION;
  }
}

// Disco, the colour engine's easter egg, walks in disco.ts, which is
// GlimStone's reference copied as it is. Its switch is stored per browser like
// the motion level, since the server's settings know nothing of it. Having
// found it is never stored: see Look.tsx.
const DISCO_CACHE = 'kl-disco';

export function cacheDisco(on: boolean): void {
  try {
    if (on) localStorage.setItem(DISCO_CACHE, 'on');
    else localStorage.removeItem(DISCO_CACHE);
  } catch {
    // Without storage the walk simply stops at the next reload.
  }
}

export function readCachedDisco(): boolean {
  try {
    return localStorage.getItem(DISCO_CACHE) === 'on';
  } catch {
    return false;
  }
}
