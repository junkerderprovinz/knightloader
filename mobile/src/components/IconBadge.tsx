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

/**
 * A waste bin, drawn from plain views.
 *
 * Not an emoji (🗑 renders in colour, and differently on every platform) and
 * not an icon library: react-native-svg is a NATIVE module, so pulling one in
 * for a single glyph would mean a new prebuild and a new .apk story for the
 * sake of twelve pixels. Views compose it exactly and take the colour they
 * are given, which is all this needs to do.
 */
export function Trash({ color, size = 15 }: { color: string; size?: number }) {
  const u = size / 15; // every number below is in fifteenths of the glyph box
  return (
    <View style={{ width: size, height: size, alignItems: 'center', justifyContent: 'center' }}>
      {/* handle */}
      <View style={{ width: 5 * u, height: 1.5 * u, backgroundColor: color, marginBottom: 0.5 * u }} />
      {/* lid */}
      <View style={{ width: 13 * u, height: 1.5 * u, backgroundColor: color }} />
      {/* The body is BUILT from its bars rather than filled and cut: React
          Native has no way to punch a hole in a view, and a "slot" drawn in
          the background colour is a lie the moment the badge sits on a
          different surface. Three uprights and a base, which is the same
          shape a stroked bin glyph draws anyway. */}
      <View style={{ width: 10 * u, height: 9.5 * u, marginTop: 1 * u }}>
        <View style={{ flexDirection: 'row', justifyContent: 'space-between', height: 7 * u }}>
          <View style={{ width: 2 * u, backgroundColor: color }} />
          <View style={{ width: 2 * u, backgroundColor: color }} />
          <View style={{ width: 2 * u, backgroundColor: color }} />
        </View>
        <View style={{ width: 10 * u, height: 1.5 * u, marginTop: 1 * u, backgroundColor: color }} />
      </View>
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
