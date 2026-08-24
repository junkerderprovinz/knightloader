import { StyleSheet, Text, TouchableOpacity } from 'react-native';
import { colors } from '../theme';

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
  return (
    <TouchableOpacity
      style={[styles.badge, accent && styles.badgeAccent]}
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={accessibilityLabel}
    >
      <Text style={[styles.symbol, accent && styles.symbolAccent]}>{symbol}</Text>
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
  badge: {
    width: 36,
    height: 36,
    borderRadius: 18,
    backgroundColor: colors.surface,
    borderWidth: 1,
    borderColor: colors.border,
    alignItems: 'center',
    justifyContent: 'center',
  },
  badgeAccent: {
    backgroundColor: colors.accent,
    borderColor: colors.accent,
  },
  symbol: { color: colors.accent, fontSize: 16, fontWeight: '700', lineHeight: 18 },
  symbolAccent: { color: colors.text },
});
