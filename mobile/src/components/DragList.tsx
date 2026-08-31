import { useCallback, useRef, useState } from 'react';
import {
  Animated,
  Easing,
  FlatList,
  PanResponder,
  Pressable,
  StyleSheet,
  View,
  type ViewStyle,
} from 'react-native';

/**
 * Long-press to pick a row up, drag to move it, let go to drop.
 *
 * Built on PanResponder and Animated, both of which ship with React Native,
 * rather than on the usual pairing of react-native-gesture-handler and
 * react-native-reanimated. That is a deliberate trade and worth naming: those
 * two would give smoother gestures driven on the UI thread, and they are two
 * NEW NATIVE dependencies plus a Babel plugin in an app whose Android build has
 * already cost a day to a linker problem once. A reorder gesture is not worth
 * putting the build at risk for. If this app ever needs gesture-handler for
 * something else, this is the first component to rewrite on top of it.
 *
 * What the gesture does, in jdp's own words (2026-08-31): "bei langem drücken
 * sollen sie anfangen zu zittern und man soll den ordner optisch anheben und
 * die anderen sollen sich sofort verschieben wenn man drüberhovert."
 *
 *   - **Long press arms it.** A drag that starts on a plain touch fights the
 *     list's own scrolling, and every attempt to tell the two apart by distance
 *     or direction gets one of them wrong.
 *   - **Everything wiggles while it is armed.** The wiggle is a MODE
 *     indicator - it says "this list is editable now" about the list, not about
 *     the row under the finger - which is why every row does it.
 *   - **The dragged row lifts:** scaled, raised, drawn above its neighbours.
 *     `elevation` and `zIndex` both, because Android reads one and iOS the
 *     other.
 *   - **The others move as it passes**, not on release, so the gap is always
 *     where the row would land.
 *
 * Two mechanics are worth knowing before editing this:
 *
 *   - **The pan is claimed in the CAPTURE phase.** By the time a drag starts,
 *     the row's own Pressable already holds the responder (that is how the long
 *     press was detected at all), and a plain `onMoveShouldSetPanResponder`
 *     asks politely for something a child is holding. The capture variant takes
 *     it. This is also why the long press lives on a real Pressable rather than
 *     a transparent overlay: an overlay claiming touches would swallow every
 *     badge inside the row, which is exactly what these rows are full of.
 *   - **Rows are different heights** (a package header against a link), so each
 *     one reports its own layout and the drop target is computed against those
 *     real boxes rather than one assumed row height.
 */
export interface DragRow {
  key: string;
  /** Rows only reorder within their own band. A link cannot become a package
   *  header's sibling, and a package cannot slide into another package's files. */
  band: string;
  render: (dragging: boolean, armed: boolean) => React.ReactNode;
}

export default function DragList({
  rows,
  onReorder,
  style,
  contentContainerStyle,
  header,
  empty,
}: {
  rows: DragRow[];
  /** The new order of keys within one band, once a drop actually moved
   *  something. Not called for a drag that ends where it started. */
  onReorder: (keys: string[], band: string) => void;
  style?: ViewStyle;
  contentContainerStyle?: ViewStyle | ViewStyle[];
  header?: React.ReactNode;
  empty?: React.ReactNode;
}) {
  const [drag, setDrag] = useState<{ from: number; to: number } | null>(null);
  // The gesture reads this, and a gesture must not wait for a render to know
  // where it is.
  const dragRef = useRef<{ from: number; to: number } | null>(null);
  dragRef.current = drag;

  const boxes = useRef<Record<number, { y: number; h: number }>>({});
  const lift = useRef(new Animated.Value(0)).current;
  const wiggle = useRef(new Animated.Value(0)).current;
  const wiggleLoop = useRef<Animated.CompositeAnimation | null>(null);

  const startWiggle = useCallback(() => {
    wiggleLoop.current?.stop();
    wiggle.setValue(0);
    wiggleLoop.current = Animated.loop(
      Animated.sequence([
        Animated.timing(wiggle, { toValue: 1, duration: 90, easing: Easing.linear, useNativeDriver: true }),
        Animated.timing(wiggle, { toValue: -1, duration: 180, easing: Easing.linear, useNativeDriver: true }),
        Animated.timing(wiggle, { toValue: 0, duration: 90, easing: Easing.linear, useNativeDriver: true }),
      ]),
    );
    wiggleLoop.current.start();
  }, [wiggle]);

  const stopWiggle = useCallback(() => {
    wiggleLoop.current?.stop();
    wiggleLoop.current = null;
    wiggle.setValue(0);
  }, [wiggle]);

  const beenden = useCallback(() => {
    stopWiggle();
    lift.setValue(0);
    setDrag(null);
  }, [lift, stopWiggle]);
  // Through a ref, so the one long-lived PanResponder never captures a stale
  // copy of it.
  const beendenRef = useRef(beenden);
  beendenRef.current = beenden;

  /** Which row a finger at this y is over, within one band. */
  const indexAt = useCallback(
    (y: number, band: string, from: number) => {
      let best = from;
      for (let i = 0; i < rows.length; i++) {
        if (rows[i].band !== band) continue;
        const b = boxes.current[i];
        if (!b) continue;
        if (y >= b.y && y <= b.y + b.h) return i;
        // Past the end of the band the last row wins, so dragging below
        // everything drops at the bottom rather than doing nothing.
        if (y > b.y) best = i;
      }
      return best;
    },
    [rows],
  );

  /**
   * ONE PanResponder for the whole list, created once, reading everything it
   * needs out of refs.
   *
   * The first cut built one responder PER ROW inside a useMemo keyed on `rows`.
   * `rows` is derived from the task list on every render, so it is a new array
   * every time - the memo recomputed, every `panHandlers` object was replaced,
   * and the gesture in flight was left holding handlers that no longer belonged
   * to any mounted view. The long press armed the drag, the drag re-rendered the
   * list, and the moves went nowhere: exactly "wenn ich lange tippe kann ich es
   * nicht verschieben" (jdp, 2026-08-31).
   *
   * The general shape is worth keeping: **a gesture handler must not be rebuilt
   * by the state changes the gesture itself causes.** Anything a handler needs
   * that changes during the gesture goes in a ref, not in a dependency array.
   */
  const daten = useRef({ rows, indexAt, onReorder });
  daten.current = { rows, indexAt, onReorder };

  const responder = useRef(
    PanResponder.create({
      // Never on a plain touch: that would take every scroll away from the
      // list. Only once a row is armed, and in the CAPTURE phase, because the
      // row's own Pressable is holding the responder by then and a polite ask
      // would be declined.
      onStartShouldSetPanResponderCapture: () => false,
      onMoveShouldSetPanResponderCapture: () => dragRef.current !== null,
      onPanResponderGrant: () => lift.setValue(0),
      onPanResponderMove: (_e, g) => {
        const d = dragRef.current;
        if (!d) return;
        lift.setValue(g.dy);
        const b = boxes.current[d.from];
        if (!b) return;
        const { rows: r, indexAt: finde } = daten.current;
        const zeile = r[d.from];
        if (!zeile) return;
        const to = finde(b.y + b.h / 2 + g.dy, zeile.band, d.from);
        if (to !== d.to) setDrag({ from: d.from, to });
      },
      onPanResponderRelease: () => {
        const d = dragRef.current;
        const { rows: r, onReorder: melde } = daten.current;
        beendenRef.current();
        if (!d || d.to === d.from) return;
        const zeile = r[d.from];
        const ziel = r[d.to];
        if (!zeile || !ziel || zeile.band !== ziel.band) return;
        const keys = r.filter((x) => x.band === zeile.band).map((x) => x.key);
        const von = keys.indexOf(zeile.key);
        const nach = keys.indexOf(ziel.key);
        if (von < 0 || nach < 0) return;
        const neu = keys.slice();
        neu.splice(nach, 0, ...neu.splice(von, 1));
        melde(neu, zeile.band);
      },
      onPanResponderTerminate: () => beendenRef.current(),
    }),
  ).current;

  const gezogeneHoehe = drag ? (boxes.current[drag.from]?.h ?? 0) : 0;

  return (
    <FlatList
      style={style}
      data={rows}
      keyExtractor={(r) => r.key}
      // A list that scrolls under a finger dragging a row is a list fighting the
      // gesture.
      scrollEnabled={drag === null}
      contentContainerStyle={contentContainerStyle}
      ListHeaderComponent={header ? <>{header}</> : null}
      ListEmptyComponent={empty ? <>{empty}</> : null}
      renderItem={({ item, index }) => {
        const gezogen = drag?.from === index;
        const armed = drag !== null;
        let versatz = 0;
        if (drag && !gezogen && rows[index].band === rows[drag.from].band) {
          if (drag.from < drag.to && index > drag.from && index <= drag.to) versatz = -gezogeneHoehe;
          if (drag.from > drag.to && index >= drag.to && index < drag.from) versatz = gezogeneHoehe;
        }
        return (
          <Animated.View
            onLayout={(e) => {
              const { y, height } = e.nativeEvent.layout;
              boxes.current[index] = { y, h: height };
            }}
            style={[
              gezogen ? styles.lifted : null,
              {
                transform: [
                  { translateY: gezogen ? lift : versatz },
                  { scale: gezogen ? 1.03 : 1 },
                  {
                    rotate:
                      armed && !gezogen
                        ? wiggle.interpolate({ inputRange: [-1, 1], outputRange: ['-0.7deg', '0.7deg'] })
                        : '0deg',
                  },
                ],
              },
            ]}
            {...responder.panHandlers}
          >
            {/* delayLongPress rather than a timer of our own: the platform
                already cancels it on movement and on lift, which is three edge
                cases nobody has to reimplement. A tap passes straight through
                to whatever the row renders. */}
            <Pressable
              delayLongPress={400}
              onLongPress={() => {
                setDrag({ from: index, to: index });
                startWiggle();
              }}
            >
              {item.render(gezogen, armed)}
            </Pressable>
          </Animated.View>
        );
      }}
    />
  );
}

const styles = StyleSheet.create({
  // Both, deliberately: Android paints by elevation, iOS by zIndex, and a row
  // that lifts on one platform and slides under its neighbour on the other is
  // the kind of thing that only shows up on the device somebody else has.
  lifted: { zIndex: 10, elevation: 8, shadowColor: '#000', shadowOpacity: 0.3, shadowRadius: 8, shadowOffset: { width: 0, height: 4 } },
});
