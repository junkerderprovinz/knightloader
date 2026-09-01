import type { ReactNode } from 'react';
import { StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { useAppearance } from '../theme/AppearanceContext';
import { TYPE } from '../theme/tokens';

/**
 * The GlimStone controls, as React Native.
 *
 * This file exists because the settings screen was drawn with borders and
 * loose grey captions while the product it connects to draws notch badges,
 * well selectors and filled switches (jdp, 2026-08-29: "In den einstellungen
 * sehen die buttons und alles ganz anders aus wie in KL selbst. Das soll auch
 * in der App exakt gleich aussehen."). The shapes and numbers here are the
 * web implementation's, not approximations: notch 22px tall, 12px uppercase
 * label with 1.2 tracking, half overlapped over its card's top edge; the well
 * is a padded groove one surface deeper whose CHOSEN segment is the only
 * badge; the switch is a 36x20 pill track with a 16px knob.
 *
 * No borders anywhere, which is the rule the old screen broke most: GlimStone
 * separates surfaces by shade, never by drawn lines.
 */

/** One card with its notch title badge half over the top edge. `hue` is the
 *  card's rainbow position; without the mode it resolves to the accent, which
 *  is exactly how the web's SectionTitle behaves. */
export function NotchCard({ title, hue, children }: { title: string; hue?: number; children: ReactNode }) {
  const { c, accent, accentContrast, radii, hueAt, rainbow } = useAppearance();
  const fill = (rainbow.on && hue !== undefined ? hueAt(hue) : undefined) ?? accent;
  return (
    <View style={[styles.cardWrap]}>
      <View style={[styles.card, { backgroundColor: c.surface, borderRadius: radii.card }]}>{children}</View>
      <View style={[styles.notch, { backgroundColor: fill, borderRadius: radii.pill }]}>
        <Text style={[styles.notchText, { color: contrastFor(fill, accentContrast) }]} numberOfLines={1}>
          {title}
        </Text>
      </View>
    </View>
  );
}

/** The one horizontal selector: a groove one surface deeper, equal segments,
 *  and only the chosen segment is a badge. Never per-segment borders. */
export function WellSelector<T extends string>({
  options,
  value,
  onPick,
}: {
  options: { value: T; label: string }[];
  value: T;
  onPick: (v: T) => void;
}) {
  const { c, accent, accentContrast, radii, hueAt } = useAppearance();
  return (
    <View style={[styles.well, { backgroundColor: c.surface2, borderRadius: radii.control }]}>
      {options.map((o, i) => {
        const on = o.value === value;
        // Each SEGMENT owns a palette position (jdp, 2026-09-01: "die design und
        // ecken selektoren sind nicht im rainbowmode in der app").
        //
        // The design language names a segmented control's segments as one of
        // the things that may own a position, for the same reason a tab strip
        // does: they are members of one set, all equal. NotchCard and
        // GlimToggle in this very file already read hueAt; this control was the
        // one that did not, so the Theme and Corners rows stayed flat gold on a
        // screen where every card around them had gone plural. Positions are
        // this control's OWN 0-based sequence, not the page's.
        const fill = (hueAt(i) ?? accent) as string;
        return (
          <TouchableOpacity
            key={o.value}
            onPress={() => onPick(o.value)}
            style={[styles.segment, { borderRadius: radii.control }, on && { backgroundColor: fill }]}
          >
            {/* Computed against the fill it actually landed on, never the flat
                accent's contrast: a palette position can be far lighter or
                darker than the accent, and reusing accentContrast is how white
                text ends up on a pale mint segment. */}
            <Text style={[styles.segmentText, { color: on ? contrastFor(fill, accentContrast) : c.textSub }]}>
              {o.label}
            </Text>
          </TouchableOpacity>
        );
      })}
    </View>
  );
}

/**
 * The switch: a track, a knob, filled when on. The same object the web UI and
 * the browser extension draw, so a person who flipped one there recognises
 * this one.
 *
 * Both radii come from the shape engine (jdp, 2026-08-30: "die toggles folgen
 * nicht der form"). They were hard 999s, which is the one number that ignores
 * the setting entirely: on Eckig every other control in the app squared off
 * and the switches stayed pills. The web's own Toggle reads
 * `var(--radius-pill)` for exactly this reason - "pill" is a token the shape
 * engine sets, not a synonym for "fully round".
 *
 * `hue` is this switch's position in a set of switches sharing one card, the
 * same 0-based sequence the cards themselves carry. Without it three switches
 * in one card all light up in the single accent and stop reading as three
 * distinct rows.
 */
export function GlimToggle({
  value,
  onChange,
  hue,
}: {
  value: boolean;
  onChange: (v: boolean) => void;
  hue?: number;
}) {
  const { c, accent, radii, hueAt } = useAppearance();
  const on = (hue !== undefined ? hueAt(hue) : undefined) ?? accent;
  return (
    <TouchableOpacity
      accessibilityRole="switch"
      accessibilityState={{ checked: value }}
      onPress={() => onChange(!value)}
      style={[styles.track, { borderRadius: radii.pill, backgroundColor: value ? on : c.surface3 }]}
    >
      {/* The knob is the page's own ground sitting on the track, not a fixed
          white: it reads dark in dark mode and light in light mode. */}
      <View
        style={[
          styles.knob,
          { borderRadius: radii.pill, backgroundColor: c.bg, alignSelf: value ? 'flex-end' : 'flex-start' },
        ]}
      />
    </TouchableOpacity>
  );
}

/** A row inside a card: label left, control flush right - the ToggleRow shape
 *  the whole family uses. */
export function GlimRow({ label, sub, control }: { label: string; sub?: string; control?: ReactNode }) {
  const { c } = useAppearance();
  return (
    <View style={styles.rowOuter}>
      <View style={styles.rowText}>
        <Text style={[styles.rowLabel, { color: c.text }]}>{label}</Text>
        {sub ? <Text style={[styles.rowSub, { color: c.textMuted }]}>{sub}</Text> : null}
      </View>
      {control}
    </View>
  );
}

/** A colour swatch. The current one is marked by a RING - an inset gap in the
 *  card colour, then the ink - drawn as nested views rather than a border,
 *  because a border is a line and this language has none. */
export function Swatch({ hex, selected, onPress, label }: { hex: string; selected: boolean; onPress: () => void; label: string }) {
  const { c, radii } = useAppearance();
  return (
    <TouchableOpacity accessibilityLabel={label} onPress={onPress} style={[styles.swatchRing, { borderRadius: radii.pill, backgroundColor: selected ? c.text : 'transparent' }]}>
      <View style={[styles.swatchGap, { borderRadius: radii.pill, backgroundColor: selected ? c.surface : 'transparent' }]}>
        <View style={[styles.swatchFill, { borderRadius: radii.pill, backgroundColor: hex }]} />
      </View>
    </TouchableOpacity>
  );
}

/** Ink on a fill, mirroring the web's luminance rule closely enough for the
 *  eight palette colours; the resolved accent contrast is the fallback for
 *  the plain-accent case. */
function contrastFor(hex: string, fallback: string): string {
  const m = /^#([0-9a-f]{6})$/i.exec(hex);
  if (!m) return fallback;
  const n = parseInt(m[1], 16);
  const lin = (v: number) => {
    const x = v / 255;
    return x <= 0.04045 ? x / 12.92 : Math.pow((x + 0.055) / 1.055, 2.4);
  };
  const lum = 0.2126 * lin((n >> 16) & 255) + 0.7152 * lin((n >> 8) & 255) + 0.0722 * lin(n & 255);
  return lum > 0.55 ? '#161616' : '#FFFFFF';
}

const styles = StyleSheet.create({
  // Room for the notch above: the badge is 22 tall and overlaps by 11.
  cardWrap: { marginTop: 24 },
  card: { paddingTop: 24, paddingBottom: 14, paddingHorizontal: 16, gap: 4 },
  notch: {
    position: 'absolute',
    top: -11,
    left: 16,
    height: 22,
    paddingHorizontal: 12,
    justifyContent: 'center',
    elevation: 3,
    shadowColor: '#000',
    shadowOpacity: 0.25,
    shadowRadius: 3,
    shadowOffset: { width: 0, height: 1 },
  },
  notchText: { fontSize: TYPE.dense, fontWeight: '500', textTransform: 'uppercase', letterSpacing: 1.2 },
  well: { flexDirection: 'row', padding: 3, gap: 2, alignSelf: 'flex-start' },
  segment: { minWidth: 84, paddingVertical: 7, paddingHorizontal: 14, alignItems: 'center' },
  segmentText: { fontSize: TYPE.dense, fontWeight: '500' },
  // No borderRadius here: it comes from the shape engine at render time, and
  // a value baked into the stylesheet cannot follow a setting.
  track: { width: 36, height: 20, padding: 2, justifyContent: 'center' },
  knob: { width: 16, height: 16 },
  rowOuter: { flexDirection: 'row', alignItems: 'center', gap: 12, paddingVertical: 8 },
  rowText: { flex: 1, minWidth: 0, gap: 2 },
  rowLabel: { fontSize: 15 },
  rowSub: { fontSize: TYPE.caption, lineHeight: 16 },
  swatchRing: { width: 32, height: 32, alignItems: 'center', justifyContent: 'center' },
  swatchGap: { width: 28, height: 28, alignItems: 'center', justifyContent: 'center' },
  swatchFill: { width: 24, height: 24 },
});
