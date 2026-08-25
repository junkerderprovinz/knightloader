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
  border: string;

  text: string;
  textSub: string;
  textMuted: string;

  statusOkText: string;
  statusOkSolid: string;
  statusFailText: string;
  statusFailSolid: string;
  statusWarnText: string;
  statusWarnSolid: string;
  statusNeutralText: string;
  statusNeutralSolid: string;
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
  border: '#393939',

  text: '#f4f4f4',
  textSub: '#c6c6c6',
  textMuted: '#8d8d8d',

  statusOkText: '#6fdc8c',
  statusOkSolid: '#6fdc8c',
  statusFailText: '#ff8389',
  statusFailSolid: '#ff8389',
  statusWarnText: '#f1c21b',
  statusWarnSolid: '#f1c21b',
  statusNeutralText: '#a8a8a8',
  statusNeutralSolid: '#8d8d8d',
};

// Carbon's light greys, mirroring the dark ramp step for step.
export const LIGHT: Palette = {
  bg: '#f4f4f4',
  sidebar: '#ffffff',
  surface: '#ffffff',
  surface2: '#e8e8e8',
  surface3: '#d1d1d1',
  hover: '#e0e0e0',
  border: '#d1d1d1',

  text: '#161616',
  textSub: '#525252',
  textMuted: '#6f6f6f',

  statusOkText: '#0e6027',
  statusOkSolid: '#198038',
  statusFailText: '#da1e28',
  statusFailSolid: '#da1e28',
  statusWarnText: '#8E6A00',
  statusWarnSolid: '#b28600',
  statusNeutralText: '#6f6f6f',
  statusNeutralSolid: '#8d8d8d',
};

// The light accent is the SAME hue darkened, never a different colour: yellow
// on white is unreadable at 11px, and Carbon's own answer to that is to darken
// rather than to switch. Applied only to the default; a user-chosen accent is
// used as given, because second-guessing a colour somebody picked on purpose is
// how a picker stops meaning anything.
export const LIGHT_DEFAULT_ACCENT = '#8E6A00';

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
