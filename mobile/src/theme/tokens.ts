import type { TextStyle } from 'react-native';

// GlimStone's palette, as this app's tokens.
//
// The values are copied from the shared reference
// (github.com/junkerderprovinz/glimstone, reference/tokens.css) rather than
// re-picked by eye, so "Sunflower" is the same yellow in every app on the
// language. The token names are the contract; these are the values behind them.
//
// React Native has no custom properties and no cascade, so the mechanism
// differs although the values do not: where the web sets `--carbon-bg` on :root
// and lets every component read it, this exposes one resolved object through a
// context (see AppearanceContext). A component asks for a token by name and
// never learns which theme, accent or rainbow position produced it.

export interface Palette {
  bg: string;
  sidebar: string;
  surface: string;
  surface2: string;
  surface3: string;
  hover: string;
  /**
   * Hover for something already filled with surface3, such as the neutral
   * button or a raised segment.
   *
   * One step further from the surface in each theme, lighter on dark and darker
   * on light, which is why it cannot be a brightness step: one brightness value
   * moves in one direction only. Nothing on this surface reads it yet, and it is
   * in the palette because a ramp that stops at surface3 sends every control
   * sitting on surface3 back down to `hover`, where it dims instead.
   */
  hoverRaised: string;
  border: string;

  text: string;
  textSub: string;
  textMuted: string;

  /**
   * The five state families, each in all three steps: `text` is the word, `bg`
   * the ground it sits on, `solid` the filled thing.
   *
   * A ground built by appending an alpha pair to a hex string is not a token:
   * it cannot differ between the two themes, nothing can find it, and changing
   * its strength becomes a search for a two-character literal.
   *
   * `info` is here although nothing on this surface shows an info state yet. A
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
  /** The quiet end of the warn family, for a notice that explains rather than
   *  warns: the ground of UnavailableNotice. */
  statusWarnBgSoft: string;
  statusInfoText: string;
  statusInfoBg: string;
  statusInfoSolid: string;
  statusNeutralText: string;
  statusNeutralBg: string;
  statusNeutralSolid: string;

  /**
   * The ground behind a floating window, as a token rather than a value typed
   * into whichever component floats one (GlimStone 1.11.0). A literal per
   * component ends as several scrims at several strengths with nothing able to
   * notice.
   *
   * Two values, one per theme, which a single literal cannot express: a light
   * page reaches the same separation with less ink, so the alpha that stops the
   * eye reading through on #161616 is heavier than #f4f4f4 needs.
   */
  scrim: string;

  /**
   * Brand marks at rest, on a neutral button (GlimStone's `--brand-*`). A
   * published brand colour is drawn for white or for its own fill, so on this
   * palette's surface2 each fails 3:1 in one of the two themes: the warm ones
   * are deepened for light and the dark ones lightened for dark. The true
   * colour is the pressed fill, in BRAND below.
   */
  brandCoffee: string;
  brandBitcoin: string;
  brandPaypal: string;
  brandGithub: string;
}

// Ground and surfaces are IBM Carbon's neutral greys rather than a warm
// near-black, which reads as brown next to any sibling app on this palette.
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
  statusWarnBgSoft: 'rgba(241, 194, 27, 0.06)',
  statusInfoText: '#FCC419',
  statusInfoBg: 'rgba(252, 196, 25, 0.13)',
  statusInfoSolid: '#FCC419',
  statusNeutralText: '#a8a8a8',
  statusNeutralBg: 'rgba(255, 255, 255, 0.05)',
  statusNeutralSolid: '#8d8d8d',

  // .65, the value tokens.css carries on a dark ground. At .60 the panel in
  // front and the page behind sit close enough in value that the eye keeps
  // reading the page.
  scrim: 'rgba(0, 0, 0, 0.65)',

  // On #393939 these measure 8.6, 5.0, 5.1 and 11.6 to 1, the extension's
  // values.
  brandCoffee: '#ffdd00',
  brandBitcoin: '#f7931a',
  brandPaypal: '#4fb5f0',
  brandGithub: '#ffffff',
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
  statusWarnBgSoft: 'rgba(142, 106, 0, 0.05)',
  statusInfoText: '#8E6A00',
  statusInfoBg: 'rgba(142, 106, 0, 0.12)',
  statusInfoSolid: '#A87D00',
  statusNeutralText: '#6f6f6f',
  statusNeutralBg: 'rgba(0, 0, 0, 0.045)',
  statusNeutralSolid: '#8d8d8d',

  // .55, not the dark theme's .65: black over a near-white page separates at a
  // lower alpha than black over a near-black one, and the darker value in light
  // mode looks like a power cut.
  scrim: 'rgba(0, 0, 0, 0.55)',

  // On #e8e8e8 these measure 4.0, 4.1, 9.7 and 14.6 to 1.
  brandCoffee: '#8a6d00',
  brandBitcoin: '#a85d00',
  brandPaypal: '#003087',
  brandGithub: '#181717',
};

export type Brand = 'coffee' | 'bitcoin' | 'paypal' | 'github';

/**
 * Each brand's true colour and the ink measured on it. The same in both themes,
 * because it is spent as a fill, the one ground the colour was drawn for.
 */
export const BRAND: Record<Brand, { fill: string; ink: string }> = {
  // Buy Me a Coffee's own near-black, the ink their button uses. 14.3:1.
  coffee: { fill: '#ffdd00', ink: '#0d0c22' },
  bitcoin: { fill: '#f7931a', ink: '#161616' },
  paypal: { fill: '#003087', ink: '#ffffff' },
  github: { fill: '#181717', ink: '#ffffff' },
};

/**
 * inkFor is the accent darkened until it can be read on a light ground.
 *
 * A colour on a fill needs no darkening, because the ink on top is computed
 * (see contrastOn). A colour used as ink does, since yellow text on white is
 * unreadable at 11px whichever yellow it is. So the accent stays the accent and
 * this is the second token: the same hue, 55% of the way to black. Dark mode
 * has nothing to solve and gets the accent as given.
 *
 * The 55% is GlimStone 1.5.0's `color-mix(in srgb, var(--accent)
 * var(--ink-mix), black)`, and this function exists because React Native has no
 * color-mix. Darkening iteratively until the result clears 4.5:1 against white
 * sounds better and is worse: it agrees with the CSS on Sunflower and diverges
 * on every other preset (Blue stops at #1D7CC5 where the CSS lands on #105486),
 * so the app and its own web interface would show two different blues for one
 * setting.
 *
 * Measured against white, the palest ground in the palette: Sunflower lands at
 * 4.95:1 and all eight accent presets clear 4.5:1.
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

// The type scale: a fixed reference table rather than an engine, since nobody
// sets their own type scale. `caption` covers three treatments of one size (a
// plain caption, an uppercase label with letter-spacing, the info-bubble text);
// the treatment carries the distinction, not the size.
export const TYPE = {
  heading: 20,
  body: 14,
  dense: 12,
  caption: 11,
} as const;

/**
 * Digits that do not jitter while they count.
 *
 * The rule is wherever they change or stack, and a download list is both: a
 * byte count rewritten every second, in a row repeated down the page. With
 * proportional figures a "1" is narrower than a "7", so the footer shuffles
 * sideways on every refresh. `.glim-num` stops the same fidget on the other two
 * surfaces.
 *
 * A shared style rather than a literal at each call site, for the reason TYPE
 * is a table: the next place that counts something finds it written down.
 *
 * `tabular-nums` is a Latin-digit feature with no defined effect on the Eastern
 * Arabic-Indic or Devanagari digits some locales render, so a locale showing
 * its own digits needs a fixed-width box for the column instead.
 */
export const NUM: TextStyle = { fontVariant: ['tabular-nums'] };

/**
 * The two button heights, and there is no third.
 *
 * `BTN_H` is what an ordinary control measures and also the side of every
 * square icon badge, whose glyph draws to half of it: the reference states the
 * pairing as 16 in 32 and 20 in 40. `BTN_H_KEY` is the step up, for a control
 * somebody reaches for with a thumb rather than a pointer.
 *
 * Two, because two reads as a difference and three reads as a ladder somebody
 * has to pick a rung from, at which point every button becomes an argument.
 *
 * rem values from tokens.css at the usual 16px root, like RADII below.
 */
export const BTN_H = 32;
export const BTN_H_KEY = 40;

/** Radii for one shape. One set for everything, with no exception list and no
 *  further mechanism behind it. */
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
