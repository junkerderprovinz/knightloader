import type { TextStyle } from 'react-native';

// GlimStone's palette, as this app's tokens.
//
// The values are copied verbatim from the shared reference
// (github.com/junkerderprovinz/glimstone, reference/tokens.css) rather than
// re-picked by eye, because the whole point of a shared design language is
// that "Sunflower" is the same yellow in every app that claims to use it.
// The token NAMES are the contract; these are the values behind them here.
//
// React Native has no CSS custom properties and no cascade, so the mechanism
// differs even though the values do not: where the web sets `--carbon-bg` on
// :root and lets every component read it, this exposes one resolved object
// through a context (see AppearanceContext). A component asks for a token by
// name and never learns which theme, accent or rainbow position produced it -
// which is the same guarantee the CSS version gives, arrived at differently.

export interface Palette {
  bg: string;
  sidebar: string;
  surface: string;
  surface2: string;
  surface3: string;
  hover: string;
  /**
   * Hover for something ALREADY filled with surface3 - the neutral button, a
   * raised segment.
   *
   * One step further from the surface in each theme, which means lighter on
   * dark and darker on light, and that is why it cannot be a brightness step:
   * a single brightness value can only move one of those two directions. It
   * carries no consumer on this surface today, and it is in the palette for
   * the same reason `hover` is - the names are the contract, and the ramp
   * stopping at surface3 is exactly how every control sitting ON surface3
   * reached back down to `hover` and dimmed itself instead.
   */
  hoverRaised: string;
  border: string;

  text: string;
  textSub: string;
  textMuted: string;

  /**
   * The five state families, each in all THREE steps.
   *
   * `text` is the word, `bg` the ground it sits on, `solid` the filled thing -
   * and the middle step is the one this file used to leave out, which is how
   * StatusBadge ended up building its own ground by appending "26" to a hex
   * string. An alpha step written as string concatenation is not a token: it
   * cannot differ between the two themes, nothing can find it, and the day the
   * badge wants a different strength it is a search for a two-character
   * literal.
   *
   * `info` is here for the same reason the other four are, even though nothing
   * on this surface shows an info state yet: the NAMES are the contract, and a
   * family missing from the palette is a family the next screen invents a
   * colour for.
   */
  statusOkText: string;
  statusOkBg: string;
  statusOkSolid: string;
  statusFailText: string;
  statusFailBg: string;
  statusFailSolid: string;
  statusWarnText: string;
  statusWarnBg: string;
  statusWarnSolid: string;
  statusInfoText: string;
  statusInfoBg: string;
  statusInfoSolid: string;
  statusNeutralText: string;
  statusNeutralBg: string;
  statusNeutralSolid: string;

  /**
   * The ground behind a floating window, as a TOKEN and not a value typed into
   * whichever component happens to float one (GlimStone 1.11.0).
   *
   * It was `#00000088` inside ColorPicker - one number, in one file, in one
   * theme's worth of thinking. The cost of that shape is not the value being
   * slightly wrong, it is that "make the scrim darker" becomes a hunt: one
   * adopting app was found carrying four scrims at two strengths, the odd one
   * weaker than the rest for as long as it had existed, with nothing able to
   * notice.
   *
   * TWO values, one per theme, and that is the half a single literal cannot
   * express at all: a light page reaches the same separation with less ink, so
   * the same alpha that stops the eye reading through on #161616 is heavier
   * than it needs to be on #f4f4f4.
   */
  scrim: string;
}

// Ground and surfaces are IBM Carbon's neutral greys, deliberately not a warm
// near-black: that reads as brown next to any sibling app on this palette.
export const DARK: Palette = {
  bg: '#161616',
  sidebar: '#262626',
  surface: '#262626',
  surface2: '#393939',
  surface3: '#525252',
  hover: '#353535',
  hoverRaised: '#6f6f6f',
  border: '#393939',

  text: '#f4f4f4',
  textSub: '#c6c6c6',
  textMuted: '#8d8d8d',

  statusOkText: '#6fdc8c',
  statusOkBg: 'rgba(111, 220, 140, 0.13)',
  statusOkSolid: '#6fdc8c',
  statusFailText: '#ff8389',
  statusFailBg: 'rgba(255, 131, 137, 0.14)',
  statusFailSolid: '#ff8389',
  statusWarnText: '#f1c21b',
  statusWarnBg: 'rgba(241, 194, 27, 0.12)',
  statusWarnSolid: '#f1c21b',
  statusInfoText: '#FCC419',
  statusInfoBg: 'rgba(252, 196, 25, 0.13)',
  statusInfoSolid: '#FCC419',
  statusNeutralText: '#a8a8a8',
  statusNeutralBg: 'rgba(255, 255, 255, 0.05)',
  statusNeutralSolid: '#8d8d8d',

  // .65, the value tokens.css carries on a dark ground. At .60 the panel in
  // front and the page behind sit close enough in value that the eye keeps
  // reading the page, which is the one thing a scrim is for.
  scrim: 'rgba(0, 0, 0, 0.65)',
};

// Carbon's light greys, mirroring the dark ramp step for step.
export const LIGHT: Palette = {
  bg: '#f4f4f4',
  sidebar: '#ffffff',
  surface: '#ffffff',
  surface2: '#e8e8e8',
  surface3: '#d1d1d1',
  hover: '#e0e0e0',
  hoverRaised: '#c6c6c6',
  border: '#d1d1d1',

  text: '#161616',
  textSub: '#525252',
  textMuted: '#6f6f6f',

  statusOkText: '#0e6027',
  statusOkBg: 'rgba(14, 96, 39, 0.11)',
  statusOkSolid: '#198038',
  statusFailText: '#da1e28',
  statusFailBg: 'rgba(218, 30, 40, 0.10)',
  statusFailSolid: '#da1e28',
  statusWarnText: '#8E6A00',
  statusWarnBg: 'rgba(142, 106, 0, 0.10)',
  statusWarnSolid: '#b28600',
  statusInfoText: '#8E6A00',
  statusInfoBg: 'rgba(142, 106, 0, 0.12)',
  statusInfoSolid: '#A87D00',
  statusNeutralText: '#6f6f6f',
  statusNeutralBg: 'rgba(0, 0, 0, 0.045)',
  statusNeutralSolid: '#8d8d8d',

  // .55, not the dark theme's .65: black over a near-white page separates at a
  // lower alpha than black over a near-black one, and carrying the darker value
  // into light mode is how a scrim starts looking like a power cut.
  scrim: 'rgba(0, 0, 0, 0.55)',
};

/**
 * inkFor is the accent darkened until it can be READ on a light ground.
 *
 * The light theme used to swap the whole accent for a fixed dark yellow
 * (#8E6A00, Carbon yellow 60), which made every filled thing on the page -
 * notch badges, switches, the primary button - olive-brown in light mode.
 * jdp, 2026-08-30: "Die gelbe akzentfarbe ist im hellen modus ganz dunkel."
 * He is right, and the mistake was using ONE token for two jobs. A colour on
 * a fill needs no darkening at all (the ink on top is computed, see
 * contrastOn); a colour used AS ink does, because yellow text on white is
 * unreadable at 11px whichever yellow it is.
 *
 * So the accent stays the accent, and this is the second token: the same hue,
 * 55% of the way to black. In dark mode there is nothing to solve and the
 * accent is returned as given.
 *
 * The 55% is not a number picked here. It is GlimStone 1.5.0's own
 * `color-mix(in srgb, var(--accent) var(--ink-mix), black)`, and this function
 * exists only because React Native has no color-mix to read it from. A first
 * cut darkened iteratively until the result cleared 4.5:1 against white,
 * which sounds better and is worse: it agrees with the CSS on Sunflower and
 * diverges on every other preset (Blue stops at #1D7CC5 where the CSS lands on
 * #105486), so the app and its own web interface would show two different
 * blues for one setting. One design language means one rule, even where the
 * rule is arithmetically the cruder of the two.
 *
 * Measured against white, the palest ground in the palette: Sunflower lands at
 * 4.95:1 and all five accent presets clear 4.5:1. Sunflower also comes out at
 * #8B6C0E, within a shade of the #8E6A00 the single old token used to be.
 */
export function inkFor(hex: string): string {
  const m = /^#([0-9a-fA-F]{6})$/.exec(hex);
  if (!m) return hex;
  const n = parseInt(m[1], 16);
  const mix = (c: number) => Math.round(c * 0.55);
  const r = mix((n >> 16) & 255);
  const g = mix((n >> 8) & 255);
  const b = mix(n & 255);
  return `#${((r << 16) | (g << 8) | b).toString(16).padStart(6, '0').toUpperCase()}`;
}

// The type scale: a fixed reference table, not an engine - nobody sets their
// own type scale. `caption` covers three treatments of one size (a plain
// caption, an uppercase label with letter-spacing, the info-bubble text); the
// treatment carries the distinction, not the size.
export const TYPE = {
  heading: 20,
  body: 14,
  dense: 12,
  caption: 11,
} as const;

/**
 * Digits that do not jitter while they count.
 *
 * The rule is "wherever they change or stack", and a download list is both at
 * once: a byte count rewritten every second, in a row repeated down the page.
 * With proportional figures a "1" is narrower than a "7", so the whole footer
 * shuffles sideways on every refresh - the exact fidget `.glim-num` exists to
 * stop on the other two surfaces, which have carried it from the start.
 *
 * A shared style rather than a literal at each call site, for the same reason
 * TYPE is a table: the next place that counts something finds it already
 * written down.
 *
 * It is a Latin-digit feature and nothing more - `tabular-nums` has no defined
 * effect on the Eastern Arabic-Indic or Devanagari digits some locales render,
 * so where a locale shows its own digits the column has to come from a
 * fixed-width box instead.
 */
export const NUM: TextStyle = { fontVariant: ['tabular-nums'] };

/**
 * The two button heights, and there is no third.
 *
 * `BTN_H` is what an ordinary control measures, and it is also the side of
 * every square icon badge in the app - the glyph inside then draws to half of
 * it, which is the pairing the reference states as 16 in 32 and 20 in 40.
 * `BTN_H_KEY` is the one step up, for a control somebody reaches for with a
 * thumb rather than a pointer.
 *
 * Two, because two reads as a deliberate difference and three reads as a
 * ladder somebody has to pick a rung from - at which point every button
 * becomes an argument. This app had picked its own two numbers (36 for the
 * badge, 44 for the labelled button) while the web measured the same badge at
 * 32 and the extension at 30: three answers for one object across one product,
 * none of them written down anywhere that could notice.
 *
 * rem values from tokens.css at the usual 16px root, like RADII below.
 */
export const BTN_H = 32;
export const BTN_H_KEY = 40;

/** Radii for one shape. One set for everything, no exception list - that is the
 *  whole shape engine, with no further mechanism behind it. */
export interface Radii {
  card: number;
  control: number;
  pill: number;
}

// rem values from tokens.css at the usual 16px root.
export const RADII: Record<string, Radii> = {
  round: { card: 16, control: 10, pill: 9999 },
  soft: { card: 8, control: 5, pill: 5 },
  square: { card: 0, control: 0, pill: 0 },
};
