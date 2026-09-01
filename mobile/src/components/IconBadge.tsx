import type { ReactNode } from 'react';
import { StyleSheet, Text, TouchableOpacity, View } from 'react-native';
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
export default function IconBadge({
  symbol,
  icon,
  onPress,
  accessibilityLabel,
  accent,
}: {
  /** A text glyph ("+", "⚙"). Ignored when `icon` is given. */
  symbol?: string;
  /** A drawn glyph, for anything the font cannot say plainly - see Trash. */
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
          an UNfilled badge the glyph is accent-coloured TEXT on a pale
          surface, which is accentInk's whole job - see tokens.ts. */}
      {icon ?? <Text style={[styles.symbol, { color: accent ? accentContrast : accentInk }]}>{symbol}</Text>}
    </TouchableOpacity>
  );
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
    width: 36,
    height: 36,
    alignItems: 'center',
    justifyContent: 'center',
  },
  symbol: { fontSize: 16, fontWeight: '700', lineHeight: 18 },
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

/** Bug: a body with legs. What "report a problem" looks like everywhere else,
 *  so nobody has to learn a second vocabulary for it. */
export function Bug({ color, size = GLYPH_BOX }: { color: string; size?: number }) {
  // Head (3) plus body (8) less their overlap (0.6) = 10.4 units tall.
  const u = unit(size, 10.4);
  const bein = { position: 'absolute' as const, width: 3 * u, height: 1.3 * u, backgroundColor: color, borderRadius: 0.65 * u };
  return (
    <View style={{ width: size, height: size, alignItems: 'center', justifyContent: 'center' }}>
      <View style={{ alignItems: 'center' }}>
        <View style={{ width: 4.4 * u, height: 3 * u, backgroundColor: color, borderRadius: 2.2 * u, marginBottom: -0.6 * u }} />
        <View style={{ width: 7 * u, height: 8 * u, backgroundColor: color, borderRadius: 3.5 * u }} />
        {/* Three legs a side, mirrored. Absolute so they hang off the body
            rather than widening the box the body is centred in. */}
        {[0, 1, 2].map((i) => (
          <View key={`l${i}`} style={[bein, { left: -2.4 * u, top: (3.4 + i * 2.4) * u }]} />
        ))}
        {[0, 1, 2].map((i) => (
          <View key={`r${i}`} style={[bein, { right: -2.4 * u, top: (3.4 + i * 2.4) * u }]} />
        ))}
      </View>
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
