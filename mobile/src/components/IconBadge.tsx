import { cloneElement, isValidElement, type ReactElement, type ReactNode } from 'react';
import { Image, StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { useAppearance } from '../theme/AppearanceContext';

// A small square glyph button - the "+" that opens Connect, the gear that
// opens Settings, the bin that drops a connection: wherever a screen needs an
// icon-sized action rather than a labelled button.
//
// Square and radius-following, not a fixed pill (jdp, 2026-08-30: "soll der
// enfternen button ein quadratischer badge mit mülleimer icon sein"), and one
// size for every one of them - the family's rule is that a square icon badge
// has ONE size app-wide, whatever its role, so the eye never has to ask why
// two neighbours differ.
//
// No border. GlimStone separates surfaces by shade and never by a drawn line;
// this component carried a 1px border for as long as it existed, which is the
// one rule the app broke in the most places at once.

/** The badge's own square. One size app-wide, whatever the badge is for. */
const BADGE = 36;

/**
 * HOW MUCH OF THAT SQUARE THE GLYPH DRAWS: half of it.
 *
 * GlimStone 1.8.0 states the proportion as 16 in 32 and 20 in 40, so 18 in 36,
 * and it is a proportion rather than a size because a lone glyph has no text
 * beside it to be measured against. The 20px a glyph takes next to 14px text is
 * the answer to a different question - there, a mark and its label have to read
 * as one control - and carrying that number into a square is how a badge ends
 * up with its glyph filling two thirds of the frame, which reads as chunky.
 *
 * ONE constant, because this badge used to hold two numbers that had never been
 * compared: a drawn glyph arrived at 12 points of ink and a character at a
 * 16-point font, in identical 36-point boxes, so the "+" and the gear standing
 * side by side in the overview's top bar were visibly not one set. Two numbers
 * for one proportion is the defect; fixing the arithmetic of each separately
 * would have left it.
 */
const BADGE_INK = BADGE / 2;

/**
 * What to ask a glyph for so its INK lands on `ink` points.
 *
 * A glyph's `size` is the box it is given, and the drawn shape is deliberately
 * smaller than that (see GLYPH_EXTENT below), so a badge that wants 18 points of
 * ink cannot simply pass 18. Written as a function rather than a constant so the
 * relationship stays visible: change how much of its box a glyph fills and this
 * follows, instead of a second number drifting out of step with the first.
 */
export function boxForInk(ink: number): number {
  return (ink * GLYPH_BOX) / GLYPH_EXTENT;
}

/**
 * The characters this badge is still asked for, and the glyphs that answer them.
 *
 * Call sites pass `symbol="+"` or `symbol="▶"`, which is a perfectly good way to
 * say WHICH mark is wanted and a bad way to draw one: how much ink a character
 * puts inside its em box is the font's decision, differs per character and
 * differs per platform, so "+" and "■" at one font size are not one size on
 * screen. That is the same argument the glyph section below makes for not using
 * emoji, arriving one door further along.
 *
 * The table lives here rather than at the call sites because the badge is what
 * knows its own box. A caller names the meaning; the box decides how big it is
 * drawn, and every badge in the app then agrees without any of them being
 * edited.
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
  /** WHICH mark is wanted, named by its character ("+", "▶"). Resolved to a
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
        // surface2, the same step the web UI's IconBadge and the extension's
        // .iconBadge both stand on. One value, not "one shade above whatever
        // is behind me": these badges sit on the page ground in the top bar
        // and on a card inside a row, and a badge that changed shade between
        // the two would be two different badges.
        { borderRadius: radii.control, backgroundColor: c.surface2 },
        accent && { backgroundColor: accentColor },
      ]}
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={accessibilityLabel}
    >
      {/* The glyph on a filled badge takes the computed ink rather than the
          body text colour: the accent is user-chosen, and white on Sunflower
          is exactly the unreadable pairing contrastOn exists to rule out. On
          an UNfilled badge the glyph is accent-coloured ink on a pale surface,
          which is accentInk's whole job - see tokens.ts.

          It reaches a `symbol` glyph and not an `icon` one, and that asymmetry
          is on purpose rather than an oversight: a caller that hands over a
          finished element has already chosen the colour it wants there, and the
          bin in a package header is deliberately textSub rather than the
          accent. */}
      {drawGlyph(icon, symbol, accent ? accentContrast : accentInk)}
    </TouchableOpacity>
  );
}

/**
 * Whatever the badge was given, drawn at the badge's own size.
 *
 * A caller hands over a finished element (`icon={<Trash color={...} />}`) or a
 * character (`symbol="+"`), and neither of them says how big the thing should
 * be - which is right, because the caller does not know what it is standing in.
 * So the size is applied HERE, in the component that owns the square, and every
 * call site in the app lands on one proportion without a single one of them
 * naming a number. Passing a size at each call site is the other way to do
 * this, and it is the way the two numbers got out of step in the first place.
 */
function drawGlyph(icon: ReactNode, symbol: string | undefined, color: string): ReactNode {
  const box = boxForInk(BADGE_INK);
  // `icon` still wins over `symbol` whenever it is there at all, which is the
  // precedence this component has always had; only glyph COMPONENTS are then
  // resized. Anything else - a host element, a fragment, something already
  // sized by its caller - is handed back untouched, because `size` on a view
  // that does not read it would be quietly ignored and look like the rule had
  // been applied.
  if (icon !== undefined && icon !== null) {
    return isValidElement(icon) && typeof icon.type === 'function'
      ? cloneElement(icon as ReactElement<{ size?: number }>, { size: box })
      : icon;
  }
  if (symbol) {
    const Glyph = SYMBOL_GLYPHS[symbol];
    if (Glyph) return <Glyph color={color} size={box} />;
    // The fallback, and it is a fallback rather than a mechanism: a character
    // nothing has been drawn for yet. Its font size comes off the same constant
    // so the two can never drift again, but an em box is NOT ink - the
    // character will sit short of the glyphs beside it by however much air its
    // font leaves around it, and the only real fix is to draw it.
    return <Text style={[styles.symbol, { color, fontSize: BADGE_INK, lineHeight: BADGE_INK * 1.15 }]}>{symbol}</Text>;
  }
  return null;
}

/* ---------------------------------------------------------------------------
   The glyphs.

   Drawn from plain Views. Not emoji (🗑 renders in colour, and differently on
   every platform) and not an icon library: react-native-svg is a NATIVE module,
   so pulling one in for a handful of shapes would mean a new prebuild and a new
   .apk story for the sake of twelve pixels.

   ONE OPTICAL SIZE FOR ALL OF THEM. A glyph is asked for at `size` and draws to
   GLYPH_EXTENT of it, whatever shape it is. That is a rule, not housekeeping:
   the same `size` used to mean 15 of 15 for the viewfinder, 13.6 for the
   clipboard and 10.1 for the plug, so three buttons in a column wore three
   visibly different icons (jdp, 2026-09-01: "Die buttons haben glyphen mit
   unterschiedlicher größe"). Nothing was wrong with any one of them; what was
   missing was a shared measure.

   Each glyph declares the extent of the shape it draws in its OWN units, and
   `unit()` scales those units so the result lands on GLYPH_EXTENT. So the
   numbers inside a glyph stay readable as proportions of that glyph - the bin
   is still "13 wide, lid 1.5 tall" - and the drawn size stops being an accident
   of how each one happened to be laid out.

   THE BOX BELOW IS THE DEFAULT, NOT THE RULE. A glyph standing beside a label
   takes it; a glyph alone in a square is sized by that square instead, through
   boxForInk at the top of this file, because the proportion a lone glyph owes
   is to its frame and there is no text next to it to match. The two cases are
   answered separately on purpose - one number serving both is how a glyph ends
   up correct beside a word and too small inside a badge.
   --------------------------------------------------------------------------- */

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
 * Back: a solid triangle pointing left (jdp, 2026-09-01: "es soll einfach ein
 * dreieck sein das nach links zeigt").
 *
 * It replaced the "‹" character, which is a typographic quotation mark borrowed
 * as an icon: it renders at the font's weight rather than the badge's and is
 * part of no icon set. Every glyph in this language is a filled solid shape,
 * and "‹" is a stroke.
 *
 * Two arrow shapes came between the two and neither survived contact - see the
 * comment inside for what each of them got wrong and why a triangle does not
 * have the same problem.
 */
export function Back({ color, size = GLYPH_BOX }: { color: string; size?: number }) {
  // The triangle stands 11 units tall in its own numbers below.
  const u = unit(size, 11);
  return (
    <View style={{ width: size, height: size, alignItems: 'center', justifyContent: 'center' }}>
      {/* Third cut, and this one is a triangle and nothing else (jdp,
          2026-09-01: "Der zurück glyph ist immer noch ein komischer pfeil. es
          soll einfach ein dreieck sein das nach links zeigt").

          The two before it were both arrows made of parts: a rotated square for
          the head, then two bars meeting in a V with a shaft laid on. Both read
          as an assembly at 15 points, because that is what they were - the eye
          sees the seams before it sees the arrow. A triangle has no seams.

          Drawn with the zero-size box and three borders, which is how a solid
          polygon is made without a polygon primitive: a View with no width or
          height, transparent top and bottom borders, and one coloured right
          border. The RESULT is a filled triangle, not a drawn line - worth
          saying because "no borders" is a rule in this project, and it is a
          rule about visible edges between surfaces, not about the layout engine
          used to fill a shape.

          Still no react-native-svg: it is a NATIVE module, and pulling one in
          for a handful of glyphs means a new prebuild and a new .apk story. */}
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
          // edge on its own. Nudged half a unit left so the POINT is centred
          // rather than the bounding box - a triangle centred by its box always
          // looks pushed towards its flat side.
          marginEnd: 1 * u,
        }}
      />
    </View>
  );
}

/**
 * Plus: add something. Two crossed bars.
 *
 * Shorter and thicker than the "+" it replaces, which is the house rule for
 * this mark and for the X (GlimStone's glyph reference, rule 7): a cross drawn
 * to the full grid is long and thin, which manages to look oversized and weak
 * at once. Ten units of arm against 2.8 of bar reads as a deliberate mark.
 *
 * Drawn rather than typed for the reason this whole section exists: a character
 * puts whatever ink its font decides inside its em box, so "+" beside a drawn
 * gear was never going to be one size however carefully the font size was
 * chosen.
 */
export function Plus({ color, size = GLYPH_BOX }: { color: string; size?: number }) {
  // The arms span 10 units of this glyph's own grid.
  const u = unit(size, 10);
  // Absolutely placed with no insets, so the parent's own centring puts them
  // both in the middle - the same construction the gear's teeth use.
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
 * The same numbers and the same construction - a zero-size box with two
 * transparent borders and one coloured one - deliberately, because these two
 * are the same shape in this app and two separate drawings of one shape drift
 * apart the moment either is touched. Only the coloured edge and the nudge
 * change sides.
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
          // Nudged so the POINT is centred rather than the bounding box, which
          // is what makes a triangle look pushed towards its flat side.
          marginStart: 1 * u,
        }}
      />
    </View>
  );
}

/**
 * Stop: halt the queue. A filled square.
 *
 * Its silhouette is the whole of what tells it from Play, which is the rule for
 * a pair of state glyphs in one badge: the colour is spent on the badge saying
 * which state is active, so the shape carries the difference. One round shape
 * against one angular one is the usual advice and a triangle against a square
 * is the same idea - never two members of one family, which is how a "play"
 * triangle beside a "pause" pair of bars ends up reading as two states of one
 * control instead of two different things.
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
  // sized by shrinking the FRAME rather than by scaling numbers inside it - the
  // same GLYPH_EXTENT either way, reached the only way an edge-anchored shape
  // can reach it. It was the widest of the family before: a full 15 of 15 where
  // the plug beside it drew 10.1.
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
      {/* A SOLID body (jdp, 2026-08-30: "Löschenicon soll ein gefülltes icon
          sein"). It was three uprights and a base, which is what a STROKED bin
          glyph draws - and a line-drawn glyph among filled badges is the one
          thing the icon rule rules out, the same call the gear just had. One
          filled block with rounded lower corners now; the slots are gone
          rather than faked, because React Native cannot punch a hole and a
          slot painted in the background colour is a lie the moment the badge
          sits on a different surface. A bin at 15px reads from its silhouette
          anyway. */}
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
 * The settings badge carried the text glyph "⚙" (U+2699), which every system
 * font draws as a thin OUTLINE - and a line-drawn glyph sitting among filled
 * badges and filled switches is the one thing the design language's icon rule
 * forbids outright (jdp, 2026-08-30: "Das Einstellungssybol soll ausgefüllt
 * sein"). The browser extension had exactly this bug and fixed it by swapping
 * in a filled path; there is no path to swap in here, so the shape is
 * composed: a filled disc, six teeth as rotated bars around it, and the hole
 * punched by a disc in the colour BEHIND the glyph.
 *
 * `hole` has to be passed rather than guessed: React Native cannot cut a shape
 * out of another, so the centre is painted, and painting it the wrong colour
 * is exactly the kind of lie that only shows up on the one surface it was not
 * tested against. The badge knows what it is standing on, so it says.
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
  // the thing inside it cannot be changed independently of each other.
  symbol: { fontWeight: '700' },
});

/** Coffee: a cup with a handle. The About card's thank-you, and the one glyph
 *  here that is a joke and a control at the same time. */
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
        {/* The handle: a ring with its inner disc punched by the badge's own
            ground would be a lie on a different surface, so it is drawn as an
            open square-ish bracket instead - three filled bars. */}
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
 * The ONE glyph in this file that is a bitmap rather than a shape built out of
 * Views, and the reason is the same one the file's own header gives for having
 * no icon library at all: react-native-svg is a native module, and this app's
 * Android build has already cost a day to a linker problem once. A logo,
 * though, is recognised or it is not - an approximation drawn out of rounded
 * rectangles would be worse than no logo - so this is a 96px mark tinted with
 * the same colour every other glyph takes, which is the one thing an Image can
 * do that keeps it in the theme.
 *
 * jdp, 2026-09-01: "der Githubutton soll das github logo als glyph bekomen und
 * nut GitHub heißen".
 */
export function Github({ color, size = GLYPH_BOX }: { color: string; size?: number }) {
  // A bitmap is sized by shrinking its FRAME, like the viewfinder's corner
  // marks and for the same reason: there are no units inside it to scale.
  //
  // Measured rather than assumed, which is the rule for any glyph arriving from
  // outside: the asset is 96x96 and its alpha reaches 96 by 94, so the mark
  // fills its canvas edge to edge. `contain` therefore drew it to the FULL box
  // while every hand-drawn glyph beside it drew to GLYPH_EXTENT of one - a
  // quarter bigger, which in the About card put GitHub's mark visibly above the
  // envelope next to it with both asking for the same size. Nothing here was
  // wrong except the one glyph that never went through `unit()` because it had
  // no units to go through it with.
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

/** Mail: an envelope, drawn as a body with the flap laid over its top. */
export function Mail({ color, size = GLYPH_BOX }: { color: string; size?: number }) {
  // The envelope is wider than it is tall, so the WIDTH is what fills the box.
  const u = unit(size, 13);
  return (
    <View style={{ width: size, height: size, alignItems: 'center', justifyContent: 'center' }}>
      <View style={{ width: 13 * u, height: 9.5 * u, backgroundColor: color, borderRadius: 1.4 * u, overflow: 'hidden' }}>
        {/* The flap: a square rotated 45 degrees, cropped by the body's own
            overflow so only the V of it shows. Filled in the badge's ground
            would be a lie on another surface, so it is the CUT that draws it -
            two bars meeting at the point, in the surface behind. */}
        <View style={{ position: 'absolute', top: -6.6 * u, left: 1.4 * u, width: 10.2 * u, height: 10.2 * u, transform: [{ rotate: '45deg' }] }} />
      </View>
    </View>
  );
}
