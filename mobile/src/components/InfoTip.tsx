import { useEffect, useRef, useState } from 'react';
import { Animated, Easing, Modal, Pressable, StyleSheet, View, useWindowDimensions } from 'react-native';
import { useAppearance } from '../theme/AppearanceContext';
import { useMotion } from '../theme/MotionContext';
import { TYPE } from '../theme/tokens';
import { Text } from './Text';

/**
 * The "(i)" and the bubble it opens: the explanation of a control, kept off the
 * page (GlimStone rule 8). A sentence printed under a control is read once and
 * then costs its height for good, and a page of grey paragraphs hides the
 * controls it was meant to explain.
 *
 * The web's bubble opens on hover and focus; a phone has neither, so a tap on
 * the (i) opens it and a tap anywhere closes it, which is what a press does to
 * the web's bubble too. The (i) carries the explanation as its accessible
 * name, so a screen reader says it without opening anything.
 */

/** The gap to every screen edge and to the trigger, the reference's 8px. */
const MARGIN = 8;
/** The bubble's widest, as `.glim-bubble` has it. */
const MAX_WIDTH = 280;

interface Rect {
  x: number;
  y: number;
  w: number;
  h: number;
}

export function InfoTip({ text, color, size = 15 }: { text: string; color?: string; size?: number }) {
  const { c } = useAppearance();
  const trigger = useRef<View>(null);
  const [at, setAt] = useState<Rect | null>(null);
  return (
    <>
      <Pressable
        ref={trigger}
        onPress={() => trigger.current?.measureInWindow((x, y, w, h) => setAt({ x, y, w, h }))}
        // The glyph is 15 points and a thumb is not, so the target reaches past
        // it without the mark growing.
        hitSlop={12}
        accessibilityRole="button"
        accessibilityLabel={text}
        style={({ pressed }) => ({ opacity: pressed ? 1 : 0.8 })}
      >
        <InfoGlyph color={color ?? c.textMuted} size={size} />
      </Pressable>
      {at && <Bubble text={text} at={at} onClose={() => setAt(null)} />}
    </>
  );
}

/**
 * The bubble, in a Modal so no card or list can clip it, as the web renders
 * its one into <body>.
 *
 * It is measured before it is placed and placed with its measured width set,
 * so moving it cannot change how its text wraps: a box sized by its content
 * after being positioned can grow taller than the height it was placed by.
 * Clamped into the screen, below the trigger unless that would clip and there
 * is room above, with the arrow on the trigger's centre.
 */
function Bubble({ text, at, onClose }: { text: string; at: Rect; onClose: () => void }) {
  const { c, corners } = useAppearance();
  const { n } = useMotion();
  const { width: vw, height: vh } = useWindowDimensions();
  const root = useRef<View>(null);
  // Where the Modal's own origin sits in the window. The trigger was measured
  // in window coordinates, and whether the Modal starts under the status bar
  // differs between devices, so the difference is measured rather than assumed.
  const [origin, setOrigin] = useState<{ x: number; y: number } | null>(null);
  const [size, setSize] = useState<{ w: number; h: number } | null>(null);
  const fade = useRef(new Animated.Value(0)).current;
  const ready = origin !== null && size !== null;

  useEffect(() => {
    if (!ready) return;
    // The level's fade, short because the finger is already waiting; at `off`
    // it simply appears. Opacity rides a plain ease, never a spring, which
    // would finish it early.
    if (n.fade === 0) fade.setValue(1);
    else Animated.timing(fade, { toValue: 1, duration: n.fade, easing: Easing.out(Easing.ease), useNativeDriver: true }).start();
  }, [ready, n.fade, fade]);

  let place = { left: 0, top: 0, arrow: 0, above: false };
  if (ready) {
    const tx = at.x - origin.x;
    const ty = at.y - origin.y;
    const cx = tx + at.w / 2;
    const left = Math.max(MARGIN, Math.min(vw - MARGIN - size.w, cx - size.w / 2));
    const below = ty + at.h + MARGIN;
    const aboveTop = ty - MARGIN - size.h;
    const above = below + size.h > vh - MARGIN && aboveTop >= MARGIN;
    place = { left, top: above ? aboveTop : below, arrow: Math.max(10, Math.min(size.w - 10, cx - left)), above };
  }

  return (
    <Modal visible transparent animationType="none" statusBarTranslucent onRequestClose={onClose}>
      <Pressable
        ref={root}
        style={StyleSheet.absoluteFill}
        onPress={onClose}
        onLayout={() => root.current?.measureInWindow((x, y) => setOrigin({ x, y }))}
      >
        <Animated.View
          pointerEvents="none"
          onLayout={(e) => {
            const { width: w, height: h } = e.nativeEvent.layout;
            if (!size || size.w !== w || size.h !== h) setSize({ w, h });
          }}
          style={[
            styles.bubble,
            {
              backgroundColor: c.surface,
              ...corners.control,
              maxWidth: Math.min(MAX_WIDTH, vw - 2 * MARGIN),
              left: place.left,
              top: place.top,
              opacity: ready ? fade : 0,
            },
            size ? { width: size.w } : null,
          ]}
        >
          {ready && (
            <View
              style={[
                styles.arrow,
                place.above
                  ? { bottom: -6, borderTopWidth: 6, borderTopColor: c.surface }
                  : { top: -6, borderBottomWidth: 6, borderBottomColor: c.surface },
                { left: place.arrow - 6 },
              ]}
            />
          )}
          <Text style={[styles.text, { color: c.text }]}>{text}</Text>
        </Animated.View>
      </Pressable>
    </Modal>
  );
}

/**
 * The (i) itself: a ring, a dot and a bar, drawn as a bare outline in the
 * neutral ink. It is the one glyph in the set that is not filled, because a
 * filled mark there would read as a control announcing activity it does not
 * have.
 */
function InfoGlyph({ color, size }: { color: string; size: number }) {
  const u = size / 16;
  return (
    <View style={{ width: size, height: size, alignItems: 'center', justifyContent: 'center' }}>
      <View
        style={{
          position: 'absolute',
          width: 14 * u,
          height: 14 * u,
          borderRadius: 7 * u,
          borderWidth: 1.3 * u,
          borderColor: color,
        }}
      />
      <View style={{ width: 1.8 * u, height: 1.8 * u, borderRadius: 0.9 * u, backgroundColor: color, marginTop: -1.4 * u }} />
      <View style={{ width: 1.3 * u, height: 4.4 * u, borderRadius: 0.65 * u, backgroundColor: color, marginTop: 1.1 * u }} />
    </View>
  );
}

const styles = StyleSheet.create({
  bubble: {
    position: 'absolute',
    paddingVertical: 7,
    paddingHorizontal: 10,
    elevation: 6,
    shadowColor: '#000',
    shadowOpacity: 0.35,
    shadowRadius: 12,
    shadowOffset: { width: 0, height: 6 },
  },
  // A triangle from a zero-size box: two transparent sides and one coloured.
  arrow: {
    position: 'absolute',
    width: 0,
    height: 0,
    borderLeftWidth: 6,
    borderRightWidth: 6,
    borderLeftColor: 'transparent',
    borderRightColor: 'transparent',
  },
  text: { fontSize: TYPE.caption, lineHeight: 16 },
});
