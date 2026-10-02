import { cloneElement, isValidElement, type ReactElement, type ReactNode } from 'react';
import { Image, StyleSheet, TouchableOpacity, View, type ImageStyle, type StyleProp } from 'react-native';
import { useAppearance } from '../theme/AppearanceContext';
import { usePress } from '../theme/MotionContext';
import { BTN_H } from '../theme/tokens';
import { GLYPH_BOX, GLYPH_INK, IconAdd, IconPlay, IconStop, type GlyphProps } from './glyphs';
import { Text } from './Text';

// A small square glyph button: the "+" that opens Connect, the gear that opens
// Settings, the bin that drops a connection, wherever a screen needs an
// icon-sized action rather than a labelled button.
//
// Square and radius-following rather than a fixed pill, and one size for all of
// them. A square icon badge has one size app-wide, whatever its role, so the
// eye never has to ask why two neighbours differ.
//
// No border: GlimStone separates surfaces by shade and never by a drawn line.

/**
 * The badge's own square, one size app-wide and product-wide.
 *
 * It comes off the shared height token (see theme/tokens.ts), where the
 * reference puts it: an icon-only control is a square at the ordinary button
 * height. A dense row that needs to stay dense absorbs the size in its own
 * padding, rather than shrinking the badge, which is how a product ends up with
 * three badge sizes on one card.
 */
const BADGE = BTN_H;

/**
 * How much of that square the glyph draws: half of it.
 *
 * GlimStone states the proportion as 16 in 32 and 20 in 40, so a square on the
 * house height lands on the first of those rather than extrapolating a third
 * pair. A proportion rather than a size, because a lone glyph has no text
 * beside it to be measured against. Beside a label a glyph is the label's size
 * instead (GLYPH_BOX), since there the mark and its words have to read as one
 * control and neither may outweigh the other.
 *
 * One constant for both the drawn glyphs and the character fallback, or the two
 * arrive at different sizes in identical boxes.
 */
const BADGE_INK = BADGE / 2;

/**
 * What to ask a glyph for so its ink lands on `ink` points. A glyph's `size` is
 * its box, and its ink fills GLYPH_INK of that.
 */
export function boxForInk(ink: number): number {
  return ink / GLYPH_INK;
}

/**
 * The characters this badge is still asked for, and the glyphs that answer them.
 *
 * Call sites pass `symbol="+"` or `symbol="▶"`, which names the mark well and
 * draws it badly: how much ink a character puts inside its em box is the font's
 * decision, differs per character and per platform, so "+" and "■" at one font
 * size are not one size on screen.
 *
 * The table lives here rather than at the call sites because the badge knows
 * its own box. A caller names the meaning and the box decides how big it is
 * drawn, so every badge in the app agrees without any of them being edited.
 */
const SYMBOL_GLYPHS: Record<string, (p: GlyphProps) => ReactNode> = {
  '+': IconAdd,
  '▶': IconPlay,
  '■': IconStop,
};

export default function IconBadge({
  symbol,
  icon,
  onPress,
  accessibilityLabel,
  accent,
}: {
  /** Which mark is wanted, named by its character ("+", "▶"). Resolved to a
   *  drawn glyph through SYMBOL_GLYPHS; a character with no glyph behind it
   *  yet is printed as text. Ignored when `icon` is given. */
  symbol?: string;
  /** A drawn glyph, such as IconTrash. Its `size` is set here, so a call site
   *  neither states one nor needs to. */
  icon?: ReactNode;
  onPress: () => void;
  accessibilityLabel: string;
  /** Filled with the accent color for a primary action (e.g. "+"); plain
   *  surface for a secondary one (e.g. the settings gear). */
  accent?: boolean;
}) {
  const { c, accent: accentColor, accentContrast, accentInk, corners } = useAppearance();
  const press = usePress();

  return (
    <TouchableOpacity
      style={[
        styles.badge,
        // surface2, the step the web UI's IconBadge and the extension's
        // .iconBadge both stand on. One value rather than one shade above
        // whatever is behind it: these badges sit on the page ground in the top
        // bar and on a card inside a row, and a badge that changed shade
        // between the two would be two different badges.
        { ...corners.pill, backgroundColor: c.surface2 },
        accent && { backgroundColor: accentColor },
        { transform: [{ scale: press.scale }] },
      ]}
      onPress={onPress}
      onPressIn={press.onPressIn}
      onPressOut={press.onPressOut}
      accessibilityRole="button"
      accessibilityLabel={accessibilityLabel}
    >
      {/* The glyph on a filled badge takes the computed ink rather than the
          body text colour, because the accent is user-chosen and white on
          Sunflower is the unreadable pairing contrastOn rules out. On an
          unfilled badge the glyph is accent-coloured ink on a pale surface,
          which is what accentInk is for (see tokens.ts).

          It reaches a `symbol` glyph and not an `icon` one: a caller that hands
          over a finished element has already chosen the colour it wants there,
          such as the textSub bin in a package header. */}
      {drawGlyph(icon, symbol, accent ? accentContrast : accentInk)}
    </TouchableOpacity>
  );
}

/**
 * Whatever the badge was given, drawn at the badge's own size.
 *
 * A caller hands over a finished element (`icon={<IconTrash color={...} />}`)
 * or a character (`symbol="+"`), and neither says how big the thing should be,
 * because the caller does not know what it is standing in. The size is applied
 * here, in the component that owns the square, so every call site lands on one
 * proportion without naming a number.
 */
function drawGlyph(icon: ReactNode, symbol: string | undefined, color: string): ReactNode {
  const box = boxForInk(BADGE_INK);
  // `icon` wins over `symbol` whenever it is there, and only glyph components
  // are resized. Anything else, a host element, a fragment or something already
  // sized by its caller, is handed back untouched: `size` on a view that does
  // not read it would be ignored and look as if the rule had been applied.
  if (icon !== undefined && icon !== null) {
    return isValidElement(icon) && typeof icon.type === 'function'
      ? cloneElement(icon as ReactElement<{ size?: number }>, { size: box })
      : icon;
  }
  if (symbol) {
    const Glyph = SYMBOL_GLYPHS[symbol];
    if (Glyph) return <Glyph color={color} size={box} />;
    // A character nothing has been drawn for yet. Its font size comes off the
    // same constant so the two cannot drift, but an em box is not ink: the
    // character sits short of the glyphs beside it by whatever air its font
    // leaves around it, and the fix is to draw it.
    return <Text style={[styles.symbol, { color, fontSize: BADGE_INK, lineHeight: BADGE_INK * 1.15 }]}>{symbol}</Text>;
  }
  return null;
}

/**
 * Scan: the four corner marks of a viewfinder. Nothing in the middle, because
 * what goes in the middle is the thing being scanned. GlimStone has no glyph
 * for scanning, so it is drawn here from Views, to the share of its box the
 * standard glyphs fill.
 */
export function Scan({ color, size = GLYPH_BOX }: { color: string; size?: number }) {
  // The corner marks are pinned to the edges of their frame, so this one is
  // sized by shrinking the frame rather than by scaling numbers inside it.
  const frame = size * GLYPH_INK;
  const u = frame / 15;
  const arm = { position: 'absolute' as const, backgroundColor: color };
  const ecke = (oben: boolean, links: boolean) => (
    <>
      <View
        style={{
          ...arm,
          width: 5 * u,
          height: 1.8 * u,
          borderRadius: 0.9 * u,
          [oben ? 'top' : 'bottom']: 0,
          [links ? 'left' : 'right']: 0,
        }}
      />
      <View
        style={{
          ...arm,
          width: 1.8 * u,
          height: 5 * u,
          borderRadius: 0.9 * u,
          [oben ? 'top' : 'bottom']: 0,
          [links ? 'left' : 'right']: 0,
        }}
      />
    </>
  );
  return (
    // Outer box keeps the asked-for size so this glyph aligns with the others
    // in a row; the inner frame is what the marks are pinned to.
    <View style={{ width: size, height: size, alignItems: 'center', justifyContent: 'center' }}>
      <View style={{ width: frame, height: frame }}>
        {ecke(true, true)}
        {ecke(true, false)}
        {ecke(false, true)}
        {ecke(false, false)}
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  badge: {
    width: BADGE,
    height: BADGE,
    alignItems: 'center',
    justifyContent: 'center',
  },
  // No fontSize here: it comes off BADGE_INK at render time, so the square and
  // the thing inside it cannot change independently.
  symbol: { fontWeight: '700' },
  fill: { width: '100%', height: '100%' },
  // A size of its own although the insets would give one: a bundled image
  // brings its pixel size as a default, and that wins over the insets.
  layer: { position: 'absolute', top: 0, start: 0, width: '100%', height: '100%' },
});

/**
 * A white bitmap tinted with the colour it is handed, which is what keeps an
 * Image in the theme. It draws the brand marks GlimStone's glyph list has no
 * entry for, each trimmed to its ink so its longer side fills the canvas.
 */
function Tinted({ source, color, style }: { source: number; color: string; style: StyleProp<ImageStyle> }) {
  return <Image source={source} style={style} tintColor={color} resizeMode="contain" accessibilityIgnoresInvertColors />;
}

/* The marks on the About card's README buttons. Each fills the mark box the
 * button gives it, so the button and not the mark decides how big it is. */

/** PayPal's double P, from Simple Icons (CC0), as on the other two surfaces. */
export function PayPal({ color }: { color: string }) {
  return <Tinted source={require('../../assets/paypal-mark.png')} color={color} style={styles.fill} />;
}

/**
 * Bitcoin's letterform with the disc cut away, for the button that opens the
 * crypto window: it reads as "crypto" to somebody who has never held any, and
 * at this size the disc would read as a dot rather than a letter.
 */
export function BitcoinLetter({ color }: { color: string }) {
  return <Tinted source={require('../../assets/coin-btcletter.png')} color={color} style={styles.fill} />;
}

/**
 * Buy Me a Coffee's own README button, which stands for the button's mark and
 * its words: the cup in `cup` and the lettering in `words`, on the button's own
 * canvas. The lettering is how the brand is known, and "Buy me a coffee" in the
 * house font does not fit the button. A bitmap takes one tint, so the cup and
 * the lettering are two layers.
 */
export function CoffeeArt({ cup, words }: { cup: string; words: string }) {
  return (
    <>
      <Tinted source={require('../../assets/coffee-cup.png')} color={cup} style={styles.layer} />
      <Tinted source={require('../../assets/coffee-lettering.png')} color={words} style={styles.layer} />
    </>
  );
}

/* The Apps cards' marks, rendered from the ones the web interface's Apps page
 * draws: Windows, Apple and Unraid from Dashboard Icons, Tux and the browsers'
 * one-ink marks from Simple Icons (CC0), the ZIP from Font Awesome Free (CC BY
 * 4.0). Docker wears GlimStone's IconContainers. */

export function WindowsMark({ color }: { color: string }) {
  return <Tinted source={require('../../assets/windows-mark.png')} color={color} style={styles.fill} />;
}

export function AppleMark({ color }: { color: string }) {
  return <Tinted source={require('../../assets/apple-mark.png')} color={color} style={styles.fill} />;
}

/** Unraid in its own colours. Its button waits for the Community Applications
 *  listing, so it is never pressed and needs no one-ink version. */
export function UnraidMark() {
  return <Image source={require('../../assets/unraid-mark.png')} style={styles.fill} resizeMode="contain" />;
}

export function ZipMark({ color }: { color: string }) {
  return <Tinted source={require('../../assets/zip-mark.png')} color={color} style={styles.fill} />;
}

/** Tux takes the ink and keeps his yellow beak and feet, which on the pressed
 *  button's yellow read as cut out of him. */
export function LinuxMark({ color }: { color: string }) {
  return (
    <>
      <Tinted source={require('../../assets/linux-mark.png')} color={color} style={styles.layer} />
      <Image source={require('../../assets/linux-beak.png')} style={styles.layer} resizeMode="contain" />
    </>
  );
}

/** Chrome in its own colours, and in one ink while pressed, where its
 *  gradients would flatten to a blot. */
export function ChromeMark({ lit, color }: { lit: boolean; color: string }) {
  if (lit) return <Tinted source={require('../../assets/chrome-mark-lit.png')} color={color} style={styles.fill} />;
  return <Image source={require('../../assets/chrome-mark.png')} style={styles.fill} resizeMode="contain" />;
}

/** Firefox in its own colours. Its button has no listing to open yet, so it is
 *  never pressed and needs no one-ink version. */
export function FirefoxMark() {
  return <Image source={require('../../assets/firefox-mark.png')} style={styles.fill} resizeMode="contain" />;
}
