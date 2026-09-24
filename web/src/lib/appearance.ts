// Appearance is the set of looks the user owns: how rounded the interface is,
// and which colour marks activity, one accent or a palette handed out by
// position. Everything is applied to the document root, so components pick it
// up through the tokens they already read.
//
// No React here: this is the file a sibling app copies. The React binding is
// useRainbow.ts. The reference copy lives at
// https://github.com/junkerderprovinz/glimstone/blob/main/reference/appearance.ts

export type Shape = 'round' | 'soft' | 'square';

export const SHAPES: Shape[] = ['round', 'soft', 'square'];

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
  const s = SHAPES.includes(shape as Shape) ? (shape as Shape) : 'round';
  document.documentElement.setAttribute('data-shape', s);
  armShapeTransition();
}

let shapeTransitionArmed = false;

// armShapeTransition turns on the shape-morph transition (.glim-shape-armed)
// two frames after the first call, so the first paint never animates and every
// later shape change does.
function armShapeTransition(): void {
  if (shapeTransitionArmed) return;
  shapeTransitionArmed = true;
  requestAnimationFrame(() => {
    requestAnimationFrame(() => {
      document.documentElement.classList.add('glim-shape-armed');
    });
  });
}

/**
 * applyAccent overrides the accent tokens, or clears the override so the
 * theme's gold comes back. The contrast colour is computed, so a light accent
 * never ends up with white text on it.
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
 * wakes the readers. The custom properties are set even when the mode is off,
 * so a stylesheet can use `--rb-3` regardless; `data-rainbow` turns the look on.
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
  const mode: 'off' | 'on' | 'reactive' = !merged.on ? 'off' : merged.reactive ? 'reactive' : 'on';
  if (mode === 'off') root.removeAttribute('data-rainbow');
  else root.setAttribute('data-rainbow', mode);

  if (lastRainbowMode !== undefined && lastRainbowMode !== mode) {
    triggerColourWipe();
  }
  lastRainbowMode = mode;

  for (const fn of listeners) fn();
}

// triggerColourWipe puts .glim-wipe on the root for the length of
// --motion-wipe-dur, so every hued colour fades over one window instead of
// snapping. The duration is read from the live DOM, so it follows data-motion.
function triggerColourWipe(): void {
  const root = document.documentElement;
  root.classList.add('glim-wipe');
  if (wipeTimeout !== undefined) clearTimeout(wipeTimeout);
  const raw = getComputedStyle(root).getPropertyValue('--motion-wipe-dur').trim();
  const ms = parseFloat(raw);
  wipeTimeout = setTimeout(() => {
    root.classList.remove('glim-wipe');
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

/**
 * rainbowColor is the colour an item should use, or undefined when the mode is
 * off. Undefined rather than the accent keeps the accent in CSS, where a theme
 * change still reaches it.
 */
export function rainbowColor(i: number): string | undefined {
  return state.on ? rainbowAt(i) : undefined;
}

/**
 * hueVars are the inline custom properties an element with a palette position
 * sets on itself; the `.glim-hue` rules in index.css decide when the hue shows.
 * The class and these properties always travel together, which hueStyle() in
 * components/ui.tsx takes care of.
 */
export function hueVars(hex: string | undefined): Record<string, string> {
  if (!valid(hex)) return {};
  const { r, g, b } = parse(hex);
  return {
    '--item-hue': hex,
    '--item-hue-ink': contrastOn(hex),
    '--item-hue-soft': `rgba(${r}, ${g}, ${b}, 0.22)`,
    // A wash covers a whole row, so it stays below the soft tint. Much less
    // than 16% and the mode looks like it does nothing.
    '--item-hue-wash': `rgba(${r}, ${g}, ${b}, 0.16)`,
    // A small badge has no neighbouring rows to repeat its colour and reads as
    // grey at the row wash's strength.
    '--item-hue-badge': `rgba(${r}, ${g}, ${b}, 0.5)`,
    // The focus ring follows the position too, so no gold ring appears around
    // a teal tab.
    '--item-hue-ring': `rgba(${r}, ${g}, ${b}, 0.55)`,
  };
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

/** contrastOn is black or white, whichever is readable on the given colour. */
export function contrastOn(hex: string): string {
  if (!valid(hex)) return '#FFFFFF';
  const { r, g, b } = parse(hex);
  // Carbon's ink rather than a warm near-black, which smudges on yellow.
  return luminance(r, g, b) > 0.55 ? '#161616' : '#FFFFFF';
}

function valid(hex: string | undefined): hex is string {
  return !!hex && /^#[0-9a-fA-F]{6}$/.test(hex);
}

function parse(hex: string): { r: number; g: number; b: number } {
  const n = parseInt(hex.slice(1), 16);
  return { r: (n >> 16) & 255, g: (n >> 8) & 255, b: n & 255 };
}

// luminance is the relative luminance of an sRGB colour. The channels are
// linearised first, since raw values overstate blue and understate green.
function luminance(r: number, g: number, b: number): number {
  const lin = (c: number) => {
    const v = c / 255;
    return v <= 0.04045 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4);
  };
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
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
    const raw = localStorage.getItem(CACHE);
    if (raw) {
      const { shape, accent, rainbow } = JSON.parse(raw) as Cached;
      applyShape(shape);
      applyAccent(accent);
      applyRainbow(rainbow);
    } else {
      applyShape('round');
      applyRainbow(undefined);
    }
  } catch {
    applyShape('round');
    applyRainbow(undefined);
  }
  applyDisco(readCachedDisco(), rainbowState());
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

// Disco, the colour engine's easter egg (GlimStone 2.1.0), steps the rainbow's
// seed once a second, so every hued element moves to the next colour together.
// It animates nothing: a seed change re-renders the colour engine's readers,
// which is a repaint and no transform.

/** One colour step a second, well under the 3Hz flicker threshold named in
 *  photosensitivity guidance. */
export const DISCO_TICK_MS = 1000;

/** Turn-ons needed to unlock, matching STORM_TAPS. */
export const DISCO_UNLOCK_TURN_ONS = 5;

/** How long a run of turn-ons may pause before it counts as a new run, so
 *  somebody comparing rainbow on and off over a minute does not unlock disco. */
export const DISCO_UNLOCK_WINDOW_MS = 3000;

let discoTimer: ReturnType<typeof setInterval> | null = null;

/** stopDisco stops the walk, and does nothing when none is running. The caller
 *  decides whether the palette the last tick left behind stays. */
export function stopDisco(): void {
  if (discoTimer !== null) {
    clearInterval(discoTimer);
    discoTimer = null;
  }
}

/**
 * applyDisco starts or stops the walk and stamps `data-disco` on the root. Call
 * it at boot and after every applyRainbow of a stored state; each call stops
 * the previous interval first. `stored` is the rainbow state as saved.
 *
 * The tick sets `rotate: true`, because rainbowAt ignores the seed without it.
 * The tick applies and never persists, so the stored seed and rotation switch
 * stay as chosen and stopping re-applies `stored`. With rainbow off the walk
 * does not run, and it starts again when rainbow comes back.
 *
 * A hue reaches an element as an inline style computed during render, so an
 * element only changes colour when its component renders again. useRainbow.ts
 * feeds the state from above the routes for that reason.
 */
export function applyDisco(on: boolean, stored: RainbowState): void {
  const wasWalking = discoTimer !== null;
  stopDisco();

  const root = document.documentElement;
  if (on) root.setAttribute('data-disco', 'on');
  else root.removeAttribute('data-disco');

  if (!on || !rainbowState().on) {
    if (wasWalking) applyRainbow(stored);
    return;
  }

  const palette = rainbowState().palette.length || 1;
  discoTimer = setInterval(() => {
    const live = rainbowState();
    applyRainbow({ ...live, rotate: true, seed: (live.seed + 1) % palette });
  }, DISCO_TICK_MS);
}

/**
 * discoTap counts the unlock gesture: five turn-ons of rainbow mode, each
 * within DISCO_UNLOCK_WINDOW_MS of the last. Returns true on the fifth.
 *
 * Counting turn-ons rather than clicks leaves rainbow on, the only state where
 * disco has colours to walk. As with stormTap, the count lives in the caller
 * and is never persisted.
 */
export function discoTap(
  state: { taps: number; last: number },
  turnedOn: boolean,
  clock: { now: number },
): boolean {
  if (!turnedOn) return false;
  const gap = clock.now - state.last;
  state.last = clock.now;
  state.taps = state.taps > 0 && gap <= DISCO_UNLOCK_WINDOW_MS ? state.taps + 1 : 1;
  if (state.taps < DISCO_UNLOCK_TURN_ONS) return false;
  state.taps = 0;
  return true;
}

// The switch is stored per browser like the motion level, since the server's
// settings know nothing of it. Having found it is never stored: see Look.tsx.
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
