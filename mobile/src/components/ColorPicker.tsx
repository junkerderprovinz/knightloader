import { useMemo, useRef, useState } from 'react';
import { Modal, PanResponder, Pressable, StyleSheet, Text, TextInput, View } from 'react-native';
import { useAppearance } from '../theme/AppearanceContext';
import { useT } from '../i18n/I18nContext';
import { TYPE } from '../theme/tokens';

/**
 * A colour picker: a saturation/value pad, a hue rail and a hex field.
 *
 * It exists because the app could show colours and not change them - the eight
 * accent presets were the whole of what a colour could be, and the rainbow
 * palette could not be touched at all (jdp, 2026-08-31: "alle farbfelder lassen
 * sich nicht bearbeiten", and again 2026-09-01: "wo sind die farbfelder für den
 * regenbogenmodus?").
 *
 * The maths is the extension's `colorpicker.js` and the shared reference behind
 * it (glimstone, reference/colorPicker.ts), transcribed rather than re-derived
 * by eye, so a colour mixed on a phone and the same colour mixed in a browser
 * are the same six digits.
 *
 * Drawn from plain Views and one PanResponder. Two things are deliberately not
 * used here:
 *
 *   - a gradient library. The pad is a grid of flat cells, which is what a
 *     gradient looks like once it is quantised anyway, and it costs no native
 *     module - the same call every glyph in IconBadge already makes.
 *   - react-native-gesture-handler. PanResponder is in React Native itself;
 *     the alternative is a native dependency, a new prebuild and a new .apk
 *     story, for one drag.
 */

export function hexToHsv(hex: string): { h: number; s: number; v: number } | null {
  const m = /^#?([0-9a-f]{6})$/i.exec(hex || '');
  if (!m) return null;
  const n = parseInt(m[1], 16);
  const r = ((n >> 16) & 255) / 255;
  const g = ((n >> 8) & 255) / 255;
  const b = (n & 255) / 255;
  const mx = Math.max(r, g, b);
  const mn = Math.min(r, g, b);
  const d = mx - mn;
  let h = 0;
  if (d) {
    if (mx === r) h = 60 * (((g - b) / d) % 6);
    else if (mx === g) h = 60 * ((b - r) / d + 2);
    else h = 60 * ((r - g) / d + 4);
  }
  if (h < 0) h += 360;
  return { h, s: mx ? d / mx : 0, v: mx };
}

export function hsvToHex(h: number, s: number, v: number): string {
  const c = v * s;
  const x = c * (1 - Math.abs(((h / 60) % 2) - 1));
  const m = v - c;
  let r = 0;
  let g = 0;
  let b = 0;
  if (h < 60) {
    r = c;
    g = x;
  } else if (h < 120) {
    r = x;
    g = c;
  } else if (h < 180) {
    g = c;
    b = x;
  } else if (h < 240) {
    g = x;
    b = c;
  } else if (h < 300) {
    r = x;
    b = c;
  } else {
    r = c;
    b = x;
  }
  const f = (u: number) =>
    Math.round((u + m) * 255)
      .toString(16)
      .padStart(2, '0');
  return `#${f(r)}${f(g)}${f(b)}`;
}

/** Accepts "2f6feb" or "#2F6FEB", answers "#rrggbb" lowercase, or null. */
export function normalizeHex(value: string): string | null {
  const trimmed = String(value ?? '')
    .trim()
    .replace(/^#/, '');
  return /^[0-9a-f]{6}$/i.test(trimmed) ? `#${trimmed.toLowerCase()}` : null;
}

const PAD = 15; // cells across and down the saturation/value pad
const HUES = 24; // steps along the hue rail
const CLAMP = (n: number) => (n < 0 ? 0 : n > 1 ? 1 : n);

export default function ColorPicker({
  visible,
  initial,
  onPick,
  onClose,
}: {
  visible: boolean;
  initial: string;
  /** Called as the colour moves, so the page behind updates live. */
  onPick: (hex: string) => void;
  onClose: () => void;
}) {
  const { c, radii, accent, accentContrast } = useAppearance();
  const { t } = useT();
  const start = hexToHsv(initial) ?? { h: 45, s: 1, v: 1 };
  const [hsv, setHsv] = useState(start);
  // Read by the pan handlers, which are built once: a handler closing over
  // `hsv` would hold the value from the render that created it, and the drag
  // would snap back to where it began on every frame. Same trap DragList
  // documents at length.
  const live = useRef(hsv);
  live.current = hsv;
  const size = useRef(1);

  const responder = useMemo(
    () =>
      PanResponder.create({
        onStartShouldSetPanResponder: () => true,
        onMoveShouldSetPanResponder: () => true,
        onPanResponderGrant: (e) => move(e.nativeEvent.locationX, e.nativeEvent.locationY),
        onPanResponderMove: (e) => move(e.nativeEvent.locationX, e.nativeEvent.locationY),
      }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  );

  function move(x: number, y: number) {
    const w = size.current || 1;
    const next = { h: live.current.h, s: CLAMP(x / w), v: CLAMP(1 - y / w) };
    live.current = next;
    setHsv(next);
    onPick(hsvToHex(next.h, next.s, next.v));
  }

  const current = hsvToHex(hsv.h, hsv.s, hsv.v);

  return (
    <Modal visible={visible} transparent animationType="fade" onRequestClose={onClose}>
      {/* Tapping the ground closes it, which is what a popover does. The panel
          itself swallows the press so a drag inside never dismisses. */}
      <Pressable style={styles.scrim} onPress={onClose}>
        <Pressable
          style={[styles.panel, { backgroundColor: c.surface, borderRadius: radii.card }]}
          onPress={() => {}}
        >
          {/* The pad: saturation left to right, value bottom to top, at the
              hue chosen on the rail below. A grid of flat cells - see the file
              comment for why that is not a compromise. */}
          <View
            style={[styles.pad, { borderRadius: radii.control }]}
            onLayout={(e) => {
              size.current = e.nativeEvent.layout.width;
            }}
            {...responder.panHandlers}
          >
            {Array.from({ length: PAD }, (_, row) => (
              <View key={row} style={styles.padRow}>
                {Array.from({ length: PAD }, (_, col) => (
                  <View
                    key={col}
                    style={{
                      flex: 1,
                      backgroundColor: hsvToHex(hsv.h, (col + 0.5) / PAD, 1 - (row + 0.5) / PAD),
                    }}
                  />
                ))}
              </View>
            ))}
            {/* Where you are. Two nested views, not a border: this language
                separates surfaces by shade and never by a drawn line, and the
                Swatch beside it draws its own selection ring exactly this way.
                The outer ink flips with the value under it so the marker stays
                visible in a white corner and a black one alike. */}
            <View
              pointerEvents="none"
              style={[
                styles.dot,
                {
                  left: `${hsv.s * 100}%`,
                  top: `${(1 - hsv.v) * 100}%`,
                  backgroundColor: hsv.v > 0.6 ? '#161616' : '#FFFFFF',
                },
              ]}
            >
              <View style={[styles.dotFill, { backgroundColor: current }]} />
            </View>
          </View>

          {/* The hue rail. Tapped rather than dragged: it is 24 wide targets in
              a row, and a tap lands on the one you meant. */}
          <View style={[styles.rail, { borderRadius: radii.control }]}>
            {Array.from({ length: HUES }, (_, i) => {
              const h = (i * 360) / HUES;
              return (
                <Pressable
                  key={i}
                  accessibilityLabel={`${Math.round(h)}°`}
                  style={{ flex: 1, backgroundColor: hsvToHex(h, 1, 1) }}
                  onPress={() => {
                    const next = { ...live.current, h };
                    live.current = next;
                    setHsv(next);
                    onPick(hsvToHex(next.h, next.s, next.v));
                  }}
                />
              );
            })}
          </View>

          <View style={styles.foot}>
            <View style={[styles.preview, { backgroundColor: current, borderRadius: radii.pill }]} />
            <TextInput
              style={[styles.hex, { backgroundColor: c.surface2, color: c.text, borderRadius: radii.control }]}
              value={current.toUpperCase()}
              autoCapitalize="characters"
              autoCorrect={false}
              maxLength={7}
              accessibilityLabel="Hex"
              onChangeText={(text) => {
                const n = normalizeHex(text);
                if (!n) return;
                const parsed = hexToHsv(n);
                if (!parsed) return;
                live.current = parsed;
                setHsv(parsed);
                onPick(n);
              }}
            />
            <Pressable
              style={[styles.done, { backgroundColor: accent, borderRadius: radii.control }]}
              onPress={onClose}
            >
              <Text style={[styles.doneText, { color: accentContrast }]}>{t('settings.pickerDone')}</Text>
            </Pressable>
          </View>
        </Pressable>
      </Pressable>
    </Modal>
  );
}

const styles = StyleSheet.create({
  scrim: { flex: 1, backgroundColor: '#00000088', alignItems: 'center', justifyContent: 'center', padding: 24 },
  panel: { width: '100%', maxWidth: 320, padding: 16, gap: 12 },
  pad: { width: '100%', aspectRatio: 1, overflow: 'hidden' },
  padRow: { flex: 1, flexDirection: 'row' },
  dot: {
    position: 'absolute',
    width: 16,
    height: 16,
    marginLeft: -8,
    marginTop: -8,
    borderRadius: 8,
    alignItems: 'center',
    justifyContent: 'center',
  },
  dotFill: { width: 11, height: 11, borderRadius: 5.5 },
  rail: { flexDirection: 'row', height: 22, overflow: 'hidden' },
  foot: { flexDirection: 'row', alignItems: 'center', gap: 10 },
  preview: { width: 32, height: 32 },
  hex: { flex: 1, height: 36, paddingHorizontal: 10, fontSize: TYPE.dense, fontVariant: ['tabular-nums'] },
  done: { height: 36, paddingHorizontal: 14, alignItems: 'center', justifyContent: 'center' },
  doneText: { fontSize: TYPE.dense, fontWeight: '600' },
});
