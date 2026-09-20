import { cloneElement, isValidElement, type ReactElement, type ReactNode } from 'react';
import { Image, StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { useAppearance } from '../theme/AppearanceContext';
import { BTN_H } from '../theme/tokens';

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
 * beside it to be measured against. The 20px a glyph takes next to 14px text
 * answers a different question, where a mark and its label have to read as one
 * control, and carrying that number into a square fills two thirds of the
 * frame, which reads as chunky.
 *
 * One constant for both the drawn glyphs and the character fallback, or the two
 * arrive at different sizes in identical boxes.
 */
const BADGE_INK = BADGE / 2;

/**
 * What to ask a glyph for so its ink lands on `ink` points.
 *
 * A glyph's `size` is the box it is given and the drawn shape is smaller than
 * that (see GLYPH_EXTENT below), so a badge that wants 18 points of ink cannot
 * pass 18. A function rather than a constant, so changing how much of its box a
 * glyph fills carries through here instead of leaving a second number behind.
 */
export function boxForInk(ink: number): number {
  return (ink * GLYPH_BOX) / GLYPH_EXTENT;
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
const SYMBOL_GLYPHS: Record<string, (p: { color: string; size?: number }) => ReactNode> = {
  '+': Plus,
  '▶': Play,
  '■': Stop,
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
  /** A drawn glyph, for anything the font cannot say plainly - see Trash. Its
   *  `size` is set here, so a call site neither states one nor needs to. */
  icon?: ReactNode;
  onPress: () => void;
  accessibilityLabel: string;
  /** Filled with the accent color for a primary action (e.g. "+"); plain
   *  surface for a secondary one (e.g. the settings gear). */
  accent?: boolean;
}) {
  const { c, accent: accentColor, accentContrast, accentInk, radii } = useAppearance();

  return (
    <TouchableOpacity
      style={[
        styles.badge,
        // surface2, the step the web UI's IconBadge and the extension's
        // .iconBadge both stand on. One value rather than one shade above
        // whatever is behind it: these badges sit on the page ground in the top
        // bar and on a card inside a row, and a badge that changed shade
        // between the two would be two different badges.
        { borderRadius: radii.control, backgroundColor: c.surface2 },
        accent && { backgroundColor: accentColor },
      ]}
      onPress={onPress}
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
 * A caller hands over a finished element (`icon={<Trash color={...} />}`) or a
 * character (`symbol="+"`), and neither says how big the thing should be,
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

/* The glyphs, drawn from plain Views.
 *
 * Not emoji, which render in colour and differently on every platform, and not
 * an icon library: react-native-svg is a native module, so pulling one in for a
 * handful of shapes would mean a new prebuild and a new .apk story.
 *
 * One optical size for all of them. A glyph is asked for at `size` and draws to
 * GLYPH_EXTENT of it whatever shape it is, so three buttons in a column do not
 * wear three visibly different icons. Each glyph declares the extent of the
 * shape it draws in its own units and `unit()` scales those units to land on
 * GLYPH_EXTENT, which keeps the numbers inside a glyph readable as proportions
 * of that glyph: the bin is still 13 wide with a lid 1.5 tall.
 *
 * The box below is the default rather than the rule. A glyph beside a label
 * takes it; a glyph alone in a square is sized by that square through
 * boxForInk, because the proportion a lone glyph owes is to its frame. One
 * number serving both leaves a glyph correct beside a word and too small inside
 * a badge. */

/** The box a glyph is given, in points, when nothing else is said. */
const GLYPH_BOX = 15;

/**
 * How much of that box the drawn shape fills, on its longest side. Below the
 * box so a round shape and a square one look equally big beside each other, and
 * so a glyph never touches the edge of the badge it sits in.
 */
const GLYPH_EXTENT = 12;

/**
 * The drawing unit for a glyph whose own longest side is `natural` units.
 *
 * `unit(size, 15)` for a shape laid out across a full 15-unit grid, `unit(size,
 * 11)` for one that only ever reaches 11 - both come out drawn at the same
 * height. Callers still pass a `size` in points and get a glyph that fits it.
 */
function unit(size: number, natural: number): number {
  return (size * GLYPH_EXTENT) / (GLYPH_BOX * natural);
}

/**
 * Back: a solid triangle pointing left.
 *
 * Not the "‹" character, a typographic quotation mark that renders at the
 * font's weight rather than the badge's and belongs to no icon set. Every glyph
 * in this language is a filled solid shape.
 */
export function Back({ color, size = GLYPH_BOX }: { color: string; size?: number }) {
  // The triangle stands 11 units tall in its own numbers below.
  const u = unit(size, 11);
  return (
    <View style={{ width: size, height: size, alignItems: 'center', justifyContent: 'center' }}>
      {/* An arrow assembled from parts, a rotated square for the head or two
          bars meeting in a V with a shaft laid on, reads as an assembly at 15
          points, because the eye sees the seams before it sees the arrow. A
          triangle has no seams.

          Drawn with a zero-size box and three borders, which is how a solid
          polygon is made without a polygon primitive: no width or height,
          transparent top and bottom borders, one coloured right border. The
          result is a filled triangle rather than a drawn line, so the project's
          rule against borders, which is about visible edges between surfaces,
          still holds. */}
      <View
        style={{
          width: 0,
          height: 0,
          borderStyle: 'solid',
          borderTopWidth: 5.5 * u,
          borderBottomWidth: 5.5 * u,
          borderRightWidth: 8 * u,
          borderTopColor: 'transparent',
          borderBottomColor: 'transparent',
          borderRightColor: color,
          // The shape is 8 wide against a 15 box, so it sits 3.5 from either
          // edge on its own. Nudged left so the point is centred rather than
          // the bounding box, which would look pushed towards the flat side.
          marginEnd: 1 * u,
        }}
      />
    </View>
  );
}

/**
 * Plus: add something. Two crossed bars.
 *
 * Shorter and thicker than the "+" character, which is the house rule for this
 * mark and for the X (GlimStone's glyph reference, rule 7): a cross drawn to
 * the full grid is long and thin, and looks oversized and weak at once. Ten
 * units of arm against 2.8 of bar reads as a mark.
 */
export function Plus({ color, size = GLYPH_BOX }: { color: string; size?: number }) {
  // The arms span 10 units of this glyph's own grid.
  const u = unit(size, 10);
  // Absolutely placed with no insets, so the parent's centring puts them both
  // in the middle, the construction the gear's teeth use.
  const bar = { position: 'absolute' as const, backgroundColor: color, borderRadius: 1.4 * u };
  return (
    <View style={{ width: size, height: size, alignItems: 'center', justifyContent: 'center' }}>
      <View style={[bar, { width: 10 * u, height: 2.8 * u }]} />
      <View style={[bar, { width: 2.8 * u, height: 10 * u }]} />
    </View>
  );
}

/**
 * Play: start the queue. The Back triangle, pointing the other way.
 *
 * The same numbers and the same construction, a zero-size box with two
 * transparent borders and one coloured one, because two separate drawings of
 * one shape drift apart the moment either is touched. Only the coloured edge
 * and the nudge change sides.
 */
export function Play({ color, size = GLYPH_BOX }: { color: string; size?: number }) {
  // The triangle stands 11 units tall, as Back's does.
  const u = unit(size, 11);
  return (
    <View style={{ width: size, height: size, alignItems: 'center', justifyContent: 'center' }}>
      <View
        style={{
          width: 0,
          height: 0,
          borderStyle: 'solid',
          borderTopWidth: 5.5 * u,
          borderBottomWidth: 5.5 * u,
          borderLeftWidth: 8 * u,
          borderTopColor: 'transparent',
          borderBottomColor: 'transparent',
          borderLeftColor: color,
          // Nudged so the point is centred rather than the bounding box, which
          // would look pushed towards the flat side.
          marginStart: 1 * u,
        }}
      />
    </View>
  );
}

/**
 * Stop: halt the queue. A filled square, the shared assortment's own answer for
 * "stop, abort".
 *
 * One of these is on screen at a time. The queue badge shows the offer that is
 * not already true, a triangle while the queue is halted and a square while it
 * runs, so the change is what tells them apart and the accessible label says
 * which in words.
 *
 * The language's pair rule does not apply here. It governs a two-option pair
 * drawn side by side with only the active one filled, where the silhouettes
 * carry the whole difference and a round shape against an angular one is the
 * pairing that survives 16 points; a play triangle beside a stop square is
 * named there as the failure of that test. If this control is ever rebuilt as
 * such a pair, the second segment needs a different silhouette, a power ring
 * against this triangle, rather than this square moved into it.
 */
export function Stop({ color, size = GLYPH_BOX }: { color: string; size?: number }) {
  // A square is as tall as it is wide, so its own grid is its extent.
  const u = unit(size, 10);
  return (
    <View style={{ width: size, height: size, alignItems: 'center', justifyContent: 'center' }}>
      <View style={{ width: 10 * u, height: 10 * u, backgroundColor: color, borderRadius: 1.2 * u }} />
    </View>
  );
}

/** Paste: a clipboard with its clip. Two filled rectangles and a bar, which is
 *  all a clipboard is at this size. */
export function Paste({ color, size = GLYPH_BOX }: { color: string; size?: number }) {
  // Clip (2.6) plus board (12) less their overlap (1) = 13.6 units tall.
  const u = unit(size, 13.6);
  return (
    <View style={{ width: size, height: size, alignItems: 'center', justifyContent: 'center' }}>
      {/* The clip, overlapping the board's top edge. */}
      <View
        style={{
          width: 6 * u,
          height: 2.6 * u,
          backgroundColor: color,
          borderRadius: 0.8 * u,
          marginBottom: -1 * u,
          zIndex: 1,
        }}
      />
      <View
        style={{
          width: 11 * u,
          height: 12 * u,
          backgroundColor: color,
          borderRadius: 1.6 * u,
        }}
      />
    </View>
  );
}

/** Connect: a plug, drawn as a body with two pins. The one glyph that says
 *  "join something" without needing a word beside it. */
export function Connect({ color, size = GLYPH_BOX }: { color: string; size?: number }) {
  // Pins (3.5) plus body (7) less their overlap (0.4) = 10.1 units tall.
  const u = unit(size, 10.1);
  return (
    <View style={{ width: size, height: size, alignItems: 'center', justifyContent: 'center' }}>
      <View style={{ flexDirection: 'row', gap: 2 * u, marginBottom: -0.4 * u }}>
        <View style={{ width: 2 * u, height: 3.5 * u, backgroundColor: color, borderRadius: 1 * u }} />
        <View style={{ width: 2 * u, height: 3.5 * u, backgroundColor: color, borderRadius: 1 * u }} />
      </View>
      <View
        style={{
          width: 9 * u,
          height: 7 * u,
          backgroundColor: color,
          borderBottomLeftRadius: 4.5 * u,
          borderBottomRightRadius: 4.5 * u,
          borderTopLeftRadius: 1 * u,
          borderTopRightRadius: 1 * u,
        }}
      />
    </View>
  );
}

/** Scan: the four corner marks of a viewfinder. Nothing in the middle, because
 *  what goes in the middle is the thing being scanned. */
export function Scan({ color, size = GLYPH_BOX }: { color: string; size?: number }) {
  // The corner marks are pinned to the edges of their frame, so this one is
  // sized by shrinking the frame rather than by scaling numbers inside it. The
  // same GLYPH_EXTENT either way, reached the only way an edge-anchored shape
  // can reach it.
  const frame = (size * GLYPH_EXTENT) / GLYPH_BOX;
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

/** A waste bin: handle, lid and a solid body. */
export function Trash({ color, size = GLYPH_BOX }: { color: string; size?: number }) {
  // Handle (1.5+0.5), lid (1.5), gap (1) and body (9.5) = 14 units tall.
  const u = unit(size, 14);
  return (
    <View style={{ width: size, height: size, alignItems: 'center', justifyContent: 'center' }}>
      {/* handle */}
      <View style={{ width: 5 * u, height: 1.5 * u, backgroundColor: color, marginBottom: 0.5 * u }} />
      {/* lid */}
      <View style={{ width: 13 * u, height: 1.5 * u, backgroundColor: color }} />
      {/* A solid body: three uprights and a base is what a stroked bin glyph
          draws, and a line-drawn glyph among filled badges is what the icon
          rule rules out. The slots are gone rather than faked, because React
          Native cannot punch a hole and a slot painted in the background colour
          is wrong the moment the badge sits on a different surface. A bin at
          15px reads from its silhouette. */}
      <View
        style={{
          width: 10 * u,
          height: 9.5 * u,
          marginTop: 1 * u,
          backgroundColor: color,
          borderBottomLeftRadius: 1.5 * u,
          borderBottomRightRadius: 1.5 * u,
        }}
      />
    </View>
  );
}

/**
 * A gear, filled, drawn from plain views.
 *
 * Not the text glyph "⚙" (U+2699), which every system font draws as a thin
 * outline, and a line-drawn glyph among filled badges and filled switches is
 * what the design language's icon rule forbids. There is no filled path to swap
 * in on this surface, so the shape is composed: a filled disc, six teeth as
 * rotated bars around it, and the hole painted by a disc in the colour behind
 * the glyph.
 *
 * `hole` is passed rather than guessed. React Native cannot cut one shape out
 * of another, so the centre is painted, and the wrong colour there only shows
 * up on the surface it was not tested against. The badge knows what it stands
 * on, so it says.
 */
export function Gear({ color, hole, size = GLYPH_BOX }: { color: string; hole: string; size?: number }) {
  // The teeth span the full 17 units of this glyph's own grid.
  const u = unit(size, 17);
  const zahn = { position: 'absolute' as const, width: 3.4 * u, height: 17 * u, backgroundColor: color };
  return (
    <View style={{ width: size, height: size, alignItems: 'center', justifyContent: 'center' }}>
      {[0, 60, 120].map((deg) => (
        <View key={deg} style={[zahn, { borderRadius: 1 * u, transform: [{ rotate: `${deg}deg` }] }]} />
      ))}
      <View
        style={{ position: 'absolute', width: 13 * u, height: 13 * u, borderRadius: 6.5 * u, backgroundColor: color }}
      />
      <View
        style={{ position: 'absolute', width: 5 * u, height: 5 * u, borderRadius: 2.5 * u, backgroundColor: hole }}
      />
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
});

/** Coffee: a cup with a handle, for the About card's thank-you. */
export function Coffee({ color, size = GLYPH_BOX }: { color: string; size?: number }) {
  // Cup (9) plus saucer (1.5) plus the gap between them (0.5) = 11 units tall.
  const u = unit(size, 11);
  return (
    <View style={{ width: size, height: size, alignItems: 'center', justifyContent: 'center' }}>
      <View style={{ flexDirection: 'row', alignItems: 'flex-start' }}>
        <View
          style={{
            width: 8 * u,
            height: 9 * u,
            backgroundColor: color,
            borderBottomLeftRadius: 3 * u,
            borderBottomRightRadius: 3 * u,
            borderTopLeftRadius: 0.8 * u,
            borderTopRightRadius: 0.8 * u,
          }}
        />
        {/* The handle. A ring with its inner disc painted in the badge's ground
            would be wrong on a different surface, so it is three filled bars
            forming an open bracket. */}
        <View style={{ width: 3.2 * u, height: 5 * u, marginTop: 1.2 * u, marginStart: -0.4 * u }}>
          <View style={{ height: 1.4 * u, backgroundColor: color, borderTopRightRadius: 0.7 * u }} />
          <View style={{ flex: 1, alignItems: 'flex-end' }}>
            <View style={{ width: 1.4 * u, flex: 1, backgroundColor: color }} />
          </View>
          <View style={{ height: 1.4 * u, backgroundColor: color, borderBottomRightRadius: 0.7 * u }} />
        </View>
      </View>
      <View style={{ width: 11 * u, height: 1.5 * u, marginTop: 0.5 * u, backgroundColor: color, borderRadius: 0.75 * u }} />
    </View>
  );
}

/**
 * GitHub's own mark, for the button that goes there.
 *
 * The one glyph here that is a bitmap rather than a shape built out of Views. A
 * logo is recognised or it is not, and an approximation drawn out of rounded
 * rectangles would be worse than no logo, so this is a 96px mark tinted with
 * the colour every other glyph takes, which is what keeps an Image in the
 * theme.
 */
export function Github({ color, size = GLYPH_BOX }: { color: string; size?: number }) {
  // A bitmap is sized by shrinking its frame, like the viewfinder's corner
  // marks, because there are no units inside it to scale. The asset is 96x96
  // with its alpha reaching 96 by 94, so the mark fills its canvas edge to
  // edge and `contain` would draw it to the full box while every hand-drawn
  // glyph beside it draws to GLYPH_EXTENT of one.
  const frame = (size * GLYPH_EXTENT) / GLYPH_BOX;
  return (
    <View style={{ width: size, height: size, alignItems: 'center', justifyContent: 'center' }}>
      <Image
        source={require('../../assets/github-mark.png')}
        style={{ width: frame, height: frame }}
        tintColor={color}
        resizeMode="contain"
        accessibilityIgnoresInvertColors
      />
    </View>
  );
}

/**
 * Mail: an envelope - a filled body with the flap's V laid over its top.
 *
 * `hole` is the colour behind the glyph, and the flap exists only when it is
 * given. React Native cannot cut one shape out of another, so the V is painted
 * rather than punched, and a guessed colour there is invisible on the surface
 * it was tested against and wrong everywhere else. The rotated square needs a
 * fill of its own: `overflow: hidden` on the body crops nothing out of a view
 * that draws nothing, and the envelope comes out a plain rounded rectangle.
 */
export function Mail({ color, hole, size = GLYPH_BOX }: { color: string; hole?: string; size?: number }) {
  // The envelope is wider than it is tall, so the width fills the box.
  const u = unit(size, 13);
  return (
    <View style={{ width: size, height: size, alignItems: 'center', justifyContent: 'center' }}>
      <View style={{ width: 13 * u, height: 9.5 * u, backgroundColor: color, borderRadius: 1.4 * u, overflow: 'hidden' }}>
        {/* A square rotated 45 degrees and hung above the body's top edge, so
            the body's own overflow keeps only its lower corner: the two edges
            of that corner are the flap, and the ground colour between them is
            what draws them. */}
        {hole ? (
          <View
            style={{
              position: 'absolute',
              top: -6.6 * u,
              left: 1.4 * u,
              width: 10.2 * u,
              height: 10.2 * u,
              backgroundColor: hole,
              transform: [{ rotate: '45deg' }],
            }}
          />
        ) : null}
      </View>
    </View>
  );
}

/**
 * Folder: a tab and a body, which is all a folder is at this size.
 *
 * The empty package list's mark. A muted glyph at reduced opacity says what
 * would be there, and the words underneath say why it is not.
 */
export function Folder({ color, size = GLYPH_BOX }: { color: string; size?: number }) {
  // Wider than it is tall (13 across against 2 + 9 less their 0.6 overlap), so
  // the width fills the box.
  const u = unit(size, 13);
  return (
    <View style={{ width: size, height: size, alignItems: 'center', justifyContent: 'center' }}>
      <View style={{ width: 13 * u }}>
        <View
          style={{
            width: 6 * u,
            height: 2 * u,
            marginBottom: -0.6 * u,
            backgroundColor: color,
            borderTopLeftRadius: 0.8 * u,
            borderTopRightRadius: 0.8 * u,
          }}
        />
        <View style={{ width: 13 * u, height: 9 * u, backgroundColor: color, borderRadius: 1.4 * u }} />
      </View>
    </View>
  );
}
