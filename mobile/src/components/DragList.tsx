import { useCallback, useRef, useState } from 'react';
import {
  Animated,
  Easing,
  FlatList,
  PanResponder,
  StyleSheet,
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
 *   - **The hold is timed off raw touch events, not a Pressable.** These rows
 *     are full of their own buttons, and a child that takes the responder on
 *     touch-down is a child a wrapping Pressable's onLongPress never hears
 *     about - so the gesture only worked where no button happened to be. See
 *     onTouchStart below.
 *   - **The pan is claimed in the CAPTURE phase.** Once armed, a child may
 *     still be holding the responder, and a plain `onMoveShouldSetPanResponder`
 *     asks politely for something somebody else has. The capture variant takes
 *     it - which also cancels the child's press, so a drag that starts on a
 *     badge never also presses it.
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
  rows: liveRows,
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

  /**
   * The list is FROZEN for as long as a drag is armed.
   *
   * This is the fix for the one that survived every other one, and it is only
   * visible on video: the task list keeps streaming while a finger is down, so
   * the rows are rebuilt and re-sorted under the gesture. In jdp's recording
   * (2026-09-01, 17:44) the top row changes identity between second 3 and second
   * 5 - "The.Jungle.Book" becomes "Avanti ragazzi di Buda" - with the finger
   * still on it. Everything downstream is indexed: `drag.from` points at a row
   * that is now a different package, `boxes` holds the measurements of the old
   * order, and the neighbours never move because the arithmetic is about rows
   * that have moved on. What it looks like from outside is a row lying on top of
   * the others doing nothing, which is exactly what he reported.
   *
   * Freezing is the honest answer rather than making the indices cleverer: while
   * somebody is moving a row, the order they are looking at IS the subject of
   * the gesture, and letting a poll rewrite it mid-move is the bug however well
   * the code follows it. The live list is picked up again the moment the drag
   * ends, and a reorder writes the whole band anyway, so nothing is lost - a
   * change that arrived during the drag lands one render later.
   */
  const gefroren = useRef<DragRow[] | null>(null);
  if (drag === null) gefroren.current = null;
  else if (gefroren.current === null) gefroren.current = liveRows;
  const rows = gefroren.current ?? liveRows;
  // The gesture reads this, and a gesture must not wait for a render to know
  // where it is.
  //
  // Assigned only while there IS a drag: beenden() clears the ref itself, and a
  // render already in flight still carries the old state - so an unconditional
  // assignment here would put an ended drag straight back and leave the row
  // lifted with nothing able to move.
  const dragRef = useRef<{ from: number; to: number } | null>(null);
  if (drag !== null) dragRef.current = drag;

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

  /** The touch that might become a hold: where it began, and the timer that
   *  turns it into one. */
  const touch = useRef<{ y: number; key: string; timer: ReturnType<typeof setTimeout> } | null>(null);
  /** Whether the pan responder actually took the gesture over. It decides who
   *  ends the drag on a lift - see onTouchEnd. */
  const panning = useRef(false);

  const cancelArm = useCallback(() => {
    if (!touch.current) return;
    clearTimeout(touch.current.timer);
    touch.current = null;
  }, []);

  /**
   * Arm by KEY, not by the index the touch started on.
   *
   * Four hundred milliseconds pass between the touch and the hold, and the list
   * streams the whole time - so the row at that index may be a different package
   * by the time the timer fires, and the drag would pick up something the finger
   * was never on. The freeze above starts only once a drag exists, which is
   * exactly one moment too late to cover this gap; looking the key up here
   * closes it. A key that has gone in the meantime arms nothing at all, which is
   * the right answer: the row somebody pressed is no longer there.
   */
  const arm = useCallback((key: string) => {
    touch.current = null;
    const index = daten.current.rows.findIndex((r) => r.key === key);
    if (index < 0) return;
    setDrag({ from: index, to: index });
    startWiggle();
  }, [startWiggle]);

  /**
   * End the drag, from wherever notices first.
   *
   * `dragRef` is cleared HERE rather than left to the next render, and that is
   * what makes this safe to call twice. Two handlers fire for one lift - the
   * pan's own release and the row's onTouchEnd - and React Native does not
   * promise which runs first. Waiting for the re-render to clear the ref meant
   * whichever ran second still saw a live drag, so the two had to be ordered by
   * a flag; getting that ordering wrong is how the row ended up "über anderen
   * Einträgen liegen" with nothing able to move afterwards (jdp, 2026-09-01).
   * Clearing it synchronously makes the second call a no-op instead of a race.
   */
  const beenden = useCallback(() => {
    cancelArm();
    stopWiggle();
    lift.setValue(0);
    panning.current = false;
    dragRef.current = null;
    setDrag(null);
  }, [cancelArm, lift, stopWiggle]);
  // Through a ref, so the one long-lived PanResponder never captures a stale
  // copy of it.
  const beendenRef = useRef(beenden);
  beendenRef.current = beenden;

  /**
   * Which row a finger at this y belongs to, within one band: the one whose
   * CENTRE is nearest.
   *
   * It used to ask "is the finger inside this row's box", and fall back to the
   * last row that began above it. Two things that reads badly, and together
   * they are most of "das verschieben funktioniert gar nicht gut" (jdp,
   * 2026-09-01): these rows are cards with margins, so between any two of them
   * there is a gap that is inside no box at all and the answer came from the
   * fallback; and inside a TALL row - a package header - the target only
   * changed once the finger had crossed the whole of it, so the gap lagged the
   * hand by most of a card.
   *
   * Nearest centre has neither problem. It always answers, it answers the same
   * thing on both sides of a margin, and the gap moves when the finger passes
   * the halfway point, which is where a hand expects it to move.
   */
  const indexAt = useCallback(
    (y: number, band: string, from: number) => {
      let best = from;
      let bestD = Infinity;
      for (let i = 0; i < rows.length; i++) {
        if (rows[i].band !== band) continue;
        const b = boxes.current[i];
        if (!b) continue;
        const d = Math.abs(y - (b.y + b.h / 2));
        if (d < bestD) {
          bestD = d;
          best = i;
        }
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
      // row's own button may be holding the responder by then and a polite ask
      // would be declined.
      // Once armed, every touch is the drag's - including one that starts on a
      // badge inside a row. Before it is armed this stays out of the way
      // entirely, so a tap reaches whatever it landed on and a swipe scrolls
      // the list.
      onStartShouldSetPanResponderCapture: () => dragRef.current !== null,
      onMoveShouldSetPanResponderCapture: () => dragRef.current !== null,
      // The list must not be able to take the gesture back mid-drag.
      onPanResponderTerminationRequest: () => false,
      onPanResponderGrant: () => {
        panning.current = true;
        lift.setValue(0);
      },
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
        panning.current = false;
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
      onPanResponderTerminate: () => {
        panning.current = false;
        beendenRef.current();
      },
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
            /* The long press is timed here, off the raw touch events, and NOT
               with a Pressable wrapped around the row (jdp, 2026-09-01: "man
               muss den ordner an einer leren stelle antippen und halten damit
               es geht").

               He is describing exactly what a wrapping Pressable does. These
               rows are full of their own touchables - a fold chevron, a start
               badge, a bin - and a child that takes the responder on touch-down
               is a child the parent's onLongPress never hears about. So the
               gesture worked on the parts of the card that happened to have no
               button on them, which is not a rule anybody could guess.

               onTouchStart/onTouchEnd are not the responder system: React
               Native dispatches them by bubbling, so they reach this view for a
               touch anywhere inside it, whoever ends up holding the responder.
               That is the whole fix - the timer starts on any touch on the row,
               and the row's own buttons keep working untouched. */
            onTouchStart={(e) => {
              // The rip-cord. A new touch while a drag is still live means the
              // last one never ended - a row unmounted mid-gesture, a responder
              // force-terminated, anything. Rather than work out every way that
              // can happen, the next touch cleans up after it, so the list can
              // never be left in a state where nothing moves any more (jdp,
              // 2026-09-01: "es lassen sich dann plötzlich keine einträge mehr
              // verschieben").
              if (dragRef.current) {
                beendenRef.current();
                return;
              }
              const y = e.nativeEvent.pageY;
              touch.current = { y, key: item.key, timer: setTimeout(() => arm(item.key), 400) };
            }}
            onTouchMove={(e) => {
              // Moved before the timer fired: that was a scroll, not a hold.
              // 10 points rather than 0, because a finger resting on glass is
              // never completely still.
              const s = touch.current;
              if (s && Math.abs(e.nativeEvent.pageY - s.y) > 10) cancelArm();
            }}
            /* Lifting ends it, armed or not: the drag lives exactly as long as
               the touch that started it. A mode that outlives the finger would
               mean the next touch anywhere in the list moves the row that was
               armed minutes ago, which is a worse surprise than having to hold
               again. */
            /* Lifting ends it, armed or not: the drag lives exactly as long as
               the touch that started it. A mode that outlives the finger would
               mean the next touch anywhere in the list moves the row that was
               armed minutes ago, which is a worse surprise than having to hold
               again.

               Only when the pan never took over, though. Once it has, the drop
               is onPanResponderRelease's to perform - and both handlers fire for
               the same lift, so ending here as well would be a race over which
               one sees the drag first. */
            onTouchEnd={() => {
              cancelArm();
              if (!panning.current && dragRef.current) beendenRef.current();
            }}
            onTouchCancel={() => {
              cancelArm();
              if (!panning.current && dragRef.current) beendenRef.current();
            }}
          >
            {item.render(gezogen, armed)}
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
