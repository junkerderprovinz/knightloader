import { StyleSheet, Text, TouchableOpacity } from 'react-native';
import { useAppearance } from '../theme/AppearanceContext';

// A small round glyph button - the "+" that opens Connect, the gear that
// opens Settings, wherever a screen needs an icon-sized action rather than
// a labelled button. Text glyphs rather than an icon font/SVG set: nothing
// else in this app pulls one in yet (QRScanner's "QR" label, DownloadsScreen's
// "‹" back chevron are the same plain-Text approach), so this stays
// consistent instead of introducing a new dependency for two symbols.
export default function IconBadge({
  symbol,
  onPress,
  accessibilityLabel,
  accent,
}: {
  symbol: string;
  onPress: () => void;
  accessibilityLabel: string;
  /** Filled with the accent color for a primary action (e.g. "+"); plain
   *  surface for a secondary one (e.g. the settings gear). */
  accent?: boolean;
}) {
  const { c, accent: accentColor, accentContrast, radii } = useAppearance();

  return (
    <TouchableOpacity
      style={[
        styles.badge,
        { borderRadius: radii.pill, backgroundColor: c.surface, borderColor: c.border },
        accent && { backgroundColor: accentColor, borderColor: accentColor },
      ]}
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={accessibilityLabel}
    >
      {/* The glyph on a filled badge takes the computed ink rather than the
          body text colour: the accent is user-chosen, and white on Sunflower
          is exactly the unreadable pairing contrastOn exists to rule out. */}
      <Text style={[styles.symbol, { color: accent ? accentContrast : accentColor }]}>{symbol}</Text>
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
  badge: {
    width: 36,
    height: 36,
    borderWidth: 1,
    alignItems: 'center',
    justifyContent: 'center',
  },
  symbol: { fontSize: 16, fontWeight: '700', lineHeight: 18 },
});
