// Appearance is the set of looks the user owns: how rounded the interface is,
// and what colour it uses for activity — one accent, or a palette handed out by
// position. All of it is applied to the document root, so every component picks
// it up through the same tokens it already reads and nothing has to be told
// about the change.
//
// This file stays free of React on purpose: it is the piece a sibling app
// copies, and a design language should not arrive with a framework attached.
// The React binding is the small hook in useRainbow.ts.
//
// This is KnightLoader's own implementation. The canonical reference copy
// lives at https://github.com/junkerderprovinz/glimstone/blob/main/reference/appearance.ts

export type Shape = 'round' | 'soft' | 'square';

export const SHAPES: Shape[] = ['round', 'soft', 'square'];

/**
 * The built-in accent. Empty in settings means this.
 *
 * It is the sibling apps' own default rather than a colour of our own: these
 * share a design language, and a family whose members open in different colours
 * is a family only on paper.
 */
export const DEFAULT_ACCENT = '#FCC419';

/**
 * ACCENTS are the presets offered in the picker — the same eight the siblings
 * offer (the original five plus Orange/Teal/Pink, confirmed live off the real
 * BombVault test container — the same eight hues as RAINBOW below, just in
 * the presets' own order rather than the palette's position order), so a
 * person who set "Blue" in one app finds the same blue here. A free colour
 * field sits beside them, so this list is a shortcut rather than a
 * restriction.
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
 * RAINBOW is the default palette: a full turn of the wheel, but tuned to the
 * same warm, slightly dusty register as the accent presets, so switching the
 * mode on changes how much colour there is and not which family it belongs to.
 * The length is fixed — colours are handed out by position, so a palette that
 * could grow would re-colour every existing row the moment one was added.
 */
export const RAINBOW: string[] = [
  '#FF8389', // red 30
  '#FF832B', // orange 40
  '#FCC419', // sunflower — the default accent, so one row always matches it
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

// Module-level so it only ever arms once per page load, no matter how many
// times applyShape() itself gets called (the cached boot apply, the live
// -settings apply once fetchSettings() resolves, every future edit from the
// picker) — see armShapeTransition() below.
let shapeTransitionArmed = false;

/**
 * Arms the shape-morph transition (index.css's .glim-shape-armed) two
 * animation frames after the first call. GlimStone's own "Round 2"
 * motion-engine note: scoped to AFTER mount deliberately, not just relying
 * on a transition being a harmless no-op on a freshly painted element's own
 * first frame (true, but not the point being guarded against — the
 * mechanism's correctness shouldn't quietly depend on a timing coincidence a
 * future change could break). The class stays off for the app's own very
 * first paint, which needs no transition at all, only the correct end
 * state, and turns every FUTURE shape change into one.
 */
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
 * The resolved on/off/reactive mode as of the last applyRainbow() call, or
 * undefined before the first one. Tracked purely so a genuine mode CHANGE
 * (fires the colour-wipe below) can be told apart from the initial boot
 * apply and from a no-op re-apply of the same resolved state — neither of
 * those should wipe. GlimStone, "The motion engine" > "Round 2" >
 * "Colour-wipe".
 */
let lastRainbowMode: 'off' | 'on' | 'reactive' | undefined;
let wipeTimeout: ReturnType<typeof setTimeout> | undefined;

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
  const mode: 'off' | 'on' | 'reactive' = !merged.on ? 'off' : merged.reactive ? 'reactive' : 'on';
  if (mode === 'off') root.removeAttribute('data-rainbow');
  else root.setAttribute('data-rainbow', mode);

  if (lastRainbowMode !== undefined && lastRainbowMode !== mode) {
    triggerColourWipe();
  }
  lastRainbowMode = mode;

  for (const fn of listeners) fn();
}

/**
 * Colour-wipe: a genuine on/off/reactive change wipes every hued element's
 * colour over one fixed window instead of each snapping independently. A
 * temporary .glim-wipe class on the root arms a `transition` on the colour
 * properties (index.css) for the --motion-wipe-dur token's own duration,
 * then comes back off. Reads the duration from the live DOM rather than
 * duplicating it as a number here, so it tracks whatever data-motion has
 * already resolved it to (0 at "off", shorter at "subtle") without this
 * module needing to know the axis' own numbers. GlimStone, "The motion
 * engine" > "Round 2" > "Colour-wipe".
 */
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
 * rainbowAt is the colour at a position, rotation applied. It answers even when
 * the mode is off, because the settings page has to show the palette it is
 * editing.
 */
export function rainbowAt(i: number): string {
  const p = state.palette;
  const off = state.rotate ? state.seed : 0;
  const n = ((Math.trunc(i) % p.length) + p.length) % p.length;
  const color = p[(n + off) % p.length];
  if (color === undefined) {
    // Unreachable in practice: usablePalette() never lets state.palette go
    // empty, but the index is computed via modulo, which TS can't verify.
    throw new Error('rainbowAt: palette is empty');
  }
  return color;
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
 *
 * The class and these properties always travel together: `.glim-hue` with no
 * `--item-hue` under it would resolve the accent to nothing. Components get
 * both from one call — `hueStyle()` in components/ui.tsx.
 */
export function hueVars(hex: string | undefined): Record<string, string> {
  if (!valid(hex)) return {};
  const { r, g, b } = parse(hex);
  return {
    '--item-hue': hex,
    '--item-hue-ink': contrastOn(hex),
    '--item-hue-soft': `rgba(${r}, ${g}, ${b}, 0.22)`,
    // The wash covers a whole row, so it sits below the soft tint - but not as
    // far below as an earlier 7% figure: real feedback across sibling apps
    // said the mode "does nothing" at that strength even though the mechanism
    // was wiring correctly (the values genuinely differed row to row). 16% is
    // the floor - still short of 22%'s "colour chart" territory, but no longer
    // indistinguishable from the ground colour at a glance.
    '--item-hue-wash': `rgba(${r}, ${g}, ${b}, 0.16)`,
    // A compact circular badge (an icon toggle, an undo/redo/zoom action) has
    // no neighbouring row to reinforce the colour by repetition the way a list
    // does, and reads as barely-tinted grey at the wash's own 16% once shrunk
    // to badge size. This tier is deliberately separate from the wash above
    // rather than just raising it - a list row's own 16% is calibrated for a
    // different reason (dense/at-scale is exactly where subtlety matters) and
    // must stay put.
    '--item-hue-badge': `rgba(${r}, ${g}, ${b}, 0.5)`,
    // The focus ring follows the position too. A gold ring around a teal tab is
    // the one place the single accent leaks back into the plural mode, and it
    // is the most visible one, because it only ever appears on the element the
    // keyboard is standing on.
    '--item-hue-ring': `rgba(${r}, ${g}, ${b}, 0.55)`,
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
  // Carbon's own ink, not a warm near-black: on a yellow accent a brown-tinted
  // black reads as a smudge, and it was the last hard-coded warm value left.
  return luminance(r, g, b) > 0.55 ? '#161616' : '#FFFFFF';
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

// ---------------------------------------------------------------------------
// Motion intensity
//
// The third user-owned axis alongside Shape and Accent/Rainbow above —
// GlimStone's docs/design-language.md, "The user-owned axes" > "Motion
// intensity" and "The motion engine" > "Round 2". data-motion on <html>
// resolves the duration/distance/amplitude tokens index.css's keyframes
// read; this module only ever sets the attribute and mirrors it to
// localStorage — the same "single mechanism, nothing downstream has to know
// which setting produced the value" shape Shape and Accent above already
// use.
//
// Unlike Shape/Accent/Rainbow, this axis is NOT part of the server's
// settings — it has nothing to round-trip (GlimStone's own "Persistence"
// note under Motion intensity: "the same 'single-operator tool, no second
// viewer who needs to agree on the current setting' reasoning Shape and
// Accent already give... for staying client-side"). It gets its own
// localStorage key rather than folding into CACHE's combined shape/accent
// /rainbow blob above, for exactly that reason: it is never written by a
// settings PATCH and never arrives in fetchSettings()'s response, so it has
// no reason to travel through the same cache entry as three fields that do.
//
// Wiring: applyMotionIntensity/cacheMotionIntensity/readCachedMotionIntensity
// are consumed by a settings-page row this module does not own (Look.tsx)
// and by app/Layout.tsx's own boot-time apply, so the axis is live from
// first paint everywhere, not only once that settings row mounts.
// ---------------------------------------------------------------------------

export type MotionIntensity = 'off' | 'subtle' | 'full';

const MOTION_INTENSITIES: MotionIntensity[] = ['off', 'subtle', 'full'];

/**
 * The richest experience, not a compatibility fallback: this axis is
 * additive polish a user dials DOWN, never one they have to opt into (unlike
 * Theme's "system" default above, which exists because nothing else already
 * reads prefers-color-scheme unconditionally — prefers-reduced-motion, by
 * contrast, already gates every entrance in index.css regardless of this
 * setting, so a "system" option here would just re-derive a signal the app
 * honours everywhere already).
 */
export const DEFAULT_MOTION: MotionIntensity = 'full';

/** applyMotionIntensity sets the attribute the motion tokens key off. */
export function applyMotionIntensity(m: MotionIntensity): void {
  document.documentElement.dataset.motion = m;
}

const MOTION_CACHE = 'kl-motion';

/**
 * Mirrors the chosen intensity into localStorage so the next load can apply
 * it before first paint — the same reason cacheAppearance above exists.
 */
export function cacheMotionIntensity(m: MotionIntensity): void {
  try {
    localStorage.setItem(MOTION_CACHE, m);
  } catch {
    // A browser with storage disabled simply pays one flash per load.
  }
}

/**
 * Applied at boot (see app/Layout.tsx). Falls back to DEFAULT_MOTION on
 * anything unexpected — no localStorage, a value this build doesn't
 * recognise, or storage access throwing outright — the same defensive shape
 * applyCachedAppearance above already uses for shape/accent/rainbow.
 */
export function readCachedMotionIntensity(): MotionIntensity {
  try {
    const raw = localStorage.getItem(MOTION_CACHE);
    return MOTION_INTENSITIES.includes(raw as MotionIntensity) ? (raw as MotionIntensity) : DEFAULT_MOTION;
  } catch {
    return DEFAULT_MOTION;
  }
}
