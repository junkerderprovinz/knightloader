import type { ReactNode } from 'react';
import { StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { useAppearance } from '../theme/AppearanceContext';
import { TYPE } from '../theme/tokens';
import { useT } from '../i18n/I18nContext';

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

/**
 * The way back for a row of swatches: an icon, not a text link (rule 13), and
 * the same circle as the swatches it stands beside.
 *
 * Sized by the row like they are, so nine things across one line stay nine
 * equal things - the extension's own row has looked exactly like this since
 * GlimStone 1.6.0, and the app was the surface still missing it entirely.
 */
export function SwatchReset({ onPress, label }: { onPress: () => void; label: string }) {
  const { c, radii } = useAppearance();
  return (
    <TouchableOpacity accessibilityLabel={label} onPress={onPress} style={styles.swatchRing}>
      <View style={[styles.swatchGap, { borderRadius: radii.pill, backgroundColor: c.surface2 }]}>
        {/* A counter-clockwise arrow, drawn as an open ring with a head, in the
            same filled register as every other glyph. */}
        <View style={[styles.resetRing, { borderColor: c.textSub, borderRadius: radii.pill }]} />
        <View style={[styles.resetHead, { borderBottomColor: c.textSub }]} />
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
  // Sized by the ROW, not by a number here (jdp, 2026-09-01: "Die farbfelder
  // bitte enger zusammen rücken, dass sie in einer zeile platz haben").
  //
  // Eight swatches at a fixed 32 plus a reset plus a label do not fit across a
  // phone, so the row wrapped - and picking a smaller fixed number just moves
  // the wrap to a narrower phone. `flex: 1` with a square aspect divides
  // whatever the row has between nine equal things, and `maxWidth` keeps them
  // from growing into saucers on a tablet. The ring and the gap follow as
  // percentages so the selection ring stays proportional at every size.
  swatchRing: { flex: 1, maxWidth: 32, aspectRatio: 1, alignItems: 'center', justifyContent: 'center' },
  swatchGap: { width: '88%', height: '88%', alignItems: 'center', justifyContent: 'center' },
  swatchFill: { width: '86%', height: '86%' },
  // The reset glyph: three quarters of a ring, plus a head on the open end.
  resetRing: { width: '58%', height: '58%', borderWidth: 1.5, borderRightColor: 'transparent' },
  statusBadge: { paddingHorizontal: 7, paddingVertical: 2, flexShrink: 0 },
  statusText: { fontSize: 11, fontWeight: '600', letterSpacing: 0.2 },
  resetHead: {
    position: 'absolute',
    right: '18%',
    top: '20%',
    width: 0,
    height: 0,
    borderStyle: 'solid',
    borderLeftWidth: 2.4,
    borderRightWidth: 2.4,
    borderBottomWidth: 4,
    borderLeftColor: 'transparent',
    borderRightColor: 'transparent',
  },
});

/**
 * A status badge: the word, on a ground in that status's own colour.
 *
 * It replaces a 10-point coloured dot (jdp, 2026-09-01: "der statuspunkt soll
 * ein kleiner badge mit online/offlin text sein und auch dem status entsprechend
 * eingefärbt sein"). A dot asks the reader to know the colour code, and to
 * anybody who cannot tell this green from this red it says nothing at all -
 * which is the whole of what a status indicator is for. The word carries the
 * meaning and the colour carries the urgency, the pairing every other status in
 * this family already uses.
 *
 * The ground is the status colour at low opacity rather than the solid, and the
 * ink is the solid: a fully filled badge in the fail colour on a card reads as
 * an alarm, and "offline" on an instance somebody has not switched on is not an
 * alarm. The extension's own `.glim-status` follows the identical rule, so the
 * two surfaces draw one object.
 */
export function StatusBadge({ status }: { status: 'checking' | 'online' | 'offline' }) {
  const { c, radii } = useAppearance();
  const { t } = useT();
  const ink =
    status === 'online' ? c.statusOkSolid : status === 'checking' ? c.statusWarnSolid : c.statusFailSolid;
  const label =
    status === 'online' ? t('instance.online') : status === 'checking' ? t('instance.checking') : t('instance.offline');
  return (
    <View style={[styles.statusBadge, { backgroundColor: ink + '26', borderRadius: radii.pill }]}>
      <Text style={[styles.statusText, { color: ink }]}>{label}</Text>
    </View>
  );
}
