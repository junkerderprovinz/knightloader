import { useCallback, useRef, useState } from 'react';
import { Animated, Easing, FlatList, PanResponder, StyleSheet, View, type ViewStyle } from 'react-native';
// The cell wrapper's prop shape, taken from the list rather than re-declared,
// since a hand-written copy can drift from the version installed.
import type { CellRendererProps } from '@react-native/virtualized-lists';
import { settle, useMotion } from '../theme/MotionContext';

/**
 * Long-press to pick a row up, drag to move it, let go to drop.
 *
 * Built on PanResponder and Animated, both of which ship with React Native,
 * rather than on react-native-gesture-handler and react-native-reanimated.
 * Those two would give smoother gestures driven on the UI thread, at the price
 * of two native dependencies and a Babel plugin in an app whose Android build
 * has already cost a day to a linker problem. If this app needs
 * gesture-handler for something else, this is the first component to rewrite on
 * top of it.
 *
 * A long press arms the gesture, because a drag that starts on a plain touch
 * fights the list's own scrolling. Every row then wiggles, since the wiggle
 * says the list is editable rather than anything about the row under the
 * finger, the dragged row lifts and is drawn above its neighbours, and they
 * move as it passes rather than on release, so the gap is where the row would
 * land. The quietest motion level drops the wiggle and the lift's scale and
 * keeps the shadow and the gap.
 *
 * Three mechanics are worth knowing before editing this:
 *
 *   - The hold is timed off raw touch events rather than a Pressable. These
 *     rows are full of their own buttons, and a child that takes the responder
 *     on touch-down is a child a wrapping Pressable's onLongPress never hears
 *     about. See onTouchStart below.
 *   - The pan is claimed in the capture phase. Once armed, a child may still be
 *     holding the responder, and a plain `onMoveShouldSetPanResponder` asks
 *     politely for something somebody else has. The capture variant takes it,
 *     which also cancels the child's press.
 *   - Rows are different heights (a package header against a link), so each one
 *     reports its own layout and the drop target is computed against those
 *     boxes rather than one assumed row height.
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
   * How much this list is allowed to move. Read through MotionContext rather
   * than from AccessibilityInfo, because the motion axis has a user-facing
   * level as well as the system signal and the two are resolved together
   * there, with the system signal winning.
   *
   * At `off` the wiggle never starts and the lift's scale is 1, while the
   * shadow and the neighbours' gap stay at every level: they tell the eye which
   * row is in the hand and where it would land.
   */
  const { motion, n } = useMotion();
  // Both ride in refs as well, because the wiggle and the drag's end run from
  // callbacks that must not be rebuilt by the state changes the gesture itself
  // causes. beenden() in particular has to stay identical across a drag, since
  // two handlers race to call it.
  const bewegung = useRef(n);
  bewegung.current = n;
  const motionRef = useRef(motion);
  motionRef.current = motion;

  /**
   * The list is frozen for as long as a drag is armed.
   *
   * The task list keeps streaming while a finger is down, so the rows would be
   * rebuilt and re-sorted under the gesture. Everything downstream is indexed:
   * `drag.from` would point at a different package, `boxes` would hold the
   * measurements of the old order, and the neighbours would not move. From
   * outside that looks like a row lying on top of the others doing nothing.
   *
   * The live list is picked up again the moment the drag ends, and a reorder
   * writes the whole band, so a change that arrived during the drag lands one
   * render later.
   */
  const gefroren = useRef<DragRow[] | null>(null);
  if (drag === null) gefroren.current = null;
  const rows = gefroren.current ?? liveRows;
  // The gesture reads this, and it must not wait for a render to know where it
  // is. Assigned only while there is a drag: beenden() clears the ref itself
  // and a render already in flight still carries the old state, so an
  // unconditional assignment would put an ended drag straight back and leave
  // the row lifted with nothing able to move.
  const dragRef = useRef<{ from: number; to: number } | null>(null);
  if (drag !== null) dragRef.current = drag;

  const boxes = useRef<Record<number, { y: number; h: number }>>({});

  /**
   * Where each row is, measured on the cell rather than on the row.
   *
   * A layout event's `y` is relative to the parent. VirtualizedList wraps
   * whatever renderItem returns in a cell View of its own, and for a plain
   * vertical list that wrapper carries no style, so a row measured against it
   * reports y = 0 and `boxes` would hold heights and no positions.
   *
   * indexAt scores a candidate by |probe - (b.y + b.h/2)| with the probe at
   * b.y + b.h/2 + dy, so with every b.y = 0 the position cancels and the score
   * collapses to a comparison of row heights. Rows in one band are the same
   * height, so every candidate ties and `d < bestD` keeps the first: the target
   * snaps to the top of the band on the first move and never follows the
   * finger, and dragging the top row leaves to === from on every event.
   *
   * The cell is a direct child of the list's content view, which is the space
   * the gesture's dy is a delta in, so measuring here puts the ruler and the
   * finger in one coordinate system.
   *
   * useRef(...).current rather than an inline component: a fresh component type
   * on every render remounts every cell, including the one under the finger.
   */
  const Zelle = useRef(function DragCell({
    index,
    style,
    onLayout,
    children,
  }: CellRendererProps<DragRow>) {
    return (
      <View
        style={style}
        onLayout={(e) => {
          const { y, height } = e.nativeEvent.layout;
          boxes.current[index] = { y, h: height };
          // Passed on rather than swallowed: with no getItemLayout, this
          // handler is how the list keeps its own cell metrics.
          onLayout?.(e);
        }}
      >
        {children}
      </View>
    );
  }).current;

  const lift = useRef(new Animated.Value(0)).current;
  /** How far into the pick-up the dragged row is, 0 to 1. Its own value rather
   *  than a scale in points, so the level's liftScale can change under it
   *  without the spring having to be restarted. */
  const hebung = useRef(new Animated.Value(0)).current;
  const wiggle = useRef(new Animated.Value(0)).current;
  const wiggleLoop = useRef<Animated.CompositeAnimation | null>(null);

  const startWiggle = useCallback(() => {
    // Not started at all rather than started and muted: an infinite animation
    // at the quietest level gets a true stop. What the wiggle says is still
    // said by the row that lifts and by the neighbours stepping aside.
    const b = bewegung.current;
    if (b.wiggleDeg === 0 || b.wiggleDur === 0) return;
    wiggleLoop.current?.stop();
    wiggle.setValue(0);
    // A quarter out, a half back across, a quarter home: the same 1:2:1 this
    // loop has always run, with the total now coming off the level's table
    // instead of being three literals.
    const viertel = b.wiggleDur / 4;
    wiggleLoop.current = Animated.loop(
      Animated.sequence([
        Animated.timing(wiggle, { toValue: 1, duration: viertel, easing: Easing.linear, useNativeDriver: true }),
        Animated.timing(wiggle, { toValue: -1, duration: viertel * 2, easing: Easing.linear, useNativeDriver: true }),
        Animated.timing(wiggle, { toValue: 0, duration: viertel, easing: Easing.linear, useNativeDriver: true }),
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
   * Arm by key rather than by the index the touch started on.
   *
   * Four hundred milliseconds pass between the touch and the hold, and the list
   * streams the whole time, so the row at that index may be a different package
   * by the time the timer fires. The freeze above starts only once a drag
   * exists, which is one moment too late to cover this gap. A key that has gone
   * in the meantime arms nothing, since the row somebody pressed is no longer
   * there.
   */
  const arm = useCallback((key: string) => {
    touch.current = null;
    const liste = daten.current.rows;
    const index = liste.findIndex((r) => r.key === key);
    if (index < 0) return;
    // Frozen against the very list the index was just found in. Freezing in the
    // render that follows setDrag leaves a whole poll interval in which `from`
    // indexes one array while the boxes, the neighbours' offsets and the drop
    // measure against another. The two lines stay next to each other so nobody
    // can put a render between them.
    gefroren.current = liste;
    setDrag({ from: index, to: index });
    startWiggle();
    // The row rises on the level's own spring, which is where GlimStone
    // 1.17.0's springDamping is spent: a little overshoot at the top visible
    // level, a longer wobble at the hidden one. At `off` settle() writes the
    // value instead of animating it, and liftScale is 1 there, so the row does
    // not grow.
    hebung.setValue(0);
    settle(hebung, 1, motionRef.current);
  }, [hebung, startWiggle]);

  /**
   * End the drag, from wherever notices first.
   *
   * `dragRef` is cleared here rather than left to the next render, which is
   * what makes this safe to call twice. Two handlers fire for one lift, the
   * pan's release and the row's onTouchEnd, and React Native does not promise
   * which runs first. Waiting for the re-render would leave whichever ran
   * second looking at a live drag, so the two would have to be ordered by a
   * flag. Clearing it synchronously makes the second call a no-op.
   */
  const beenden = useCallback(() => {
    cancelArm();
    stopWiggle();
    // Both are written rather than sprung. setDrag(null) two lines down takes
    // `gezogen` away on the next render, and the row's translateY switches from
    // this value to the neighbours' plain offset in the same frame, so a spring
    // started here would animate a number nothing draws. The visible half of
    // the gesture is the pick-up in arm() above.
    //
    // Springing the drop means holding the row at its lifted offset until it
    // has travelled to its slot, which means not clearing the drag
    // synchronously, and the synchronous clear is what keeps the two handlers
    // from racing.
    lift.setValue(0);
    hebung.setValue(0);
    panning.current = false;
    dragRef.current = null;
    setDrag(null);
  }, [cancelArm, hebung, lift, stopWiggle]);
  // Through a ref, so the one long-lived PanResponder never captures a stale
  // copy of it.
  const beendenRef = useRef(beenden);
  beendenRef.current = beenden;

  /**
   * Which row a finger at this y belongs to, within one band: the one whose
   * centre is nearest.
   *
   * Asking whether the finger is inside a row's box reads badly twice over.
   * These rows are cards with margins, so between any two of them is a gap
   * inside no box at all, and inside a tall row such as a package header the
   * target only changes once the finger has crossed the whole of it, so the gap
   * lags the hand by most of a card.
   *
   * Nearest centre always answers, answers the same on both sides of a margin,
   * and moves the gap when the finger passes the halfway point.
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
   * One PanResponder for the whole list, created once, reading everything it
   * needs out of refs.
   *
   * A responder per row inside a useMemo keyed on `rows` is rebuilt on every
   * render, because `rows` is derived from the task list and is a new array
   * every time. The gesture in flight is then left holding handlers that belong
   * to no mounted view: the long press arms the drag, the drag re-renders the
   * list, and the moves go nowhere.
   *
   * A gesture handler must not be rebuilt by the state changes the gesture
   * itself causes, so anything a handler needs that changes during the gesture
   * goes in a ref rather than in a dependency array.
   */
  const daten = useRef({ rows, indexAt, onReorder });
  daten.current = { rows, indexAt, onReorder };

  const responder = useRef(
    PanResponder.create({
      // Never on a plain touch, which would take every scroll away from the
      // list. Only once a row is armed, and in the capture phase, because the
      // row's own button may be holding the responder by then and a polite ask
      // would be declined. Once armed, every touch is the drag's, including one
      // that starts on a badge inside a row; before that this stays out of the
      // way, so a tap reaches what it landed on and a swipe scrolls the list.
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
      /**
       * The line the whole gesture hangs on.
       *
       * VirtualizedList re-renders a cell only when `data` changes by reference
       * or when `extraData` does. `renderItem` below reads `drag` out of the
       * closure, a value the list knows nothing about, so without this a drag
       * changes nothing on screen: the neighbours do not step aside and the
       * lifted row does not lift.
       *
       * Rebuilding `rows` on every render hides that, because the reference
       * keeps changing and the cells are redrawn for the wrong reason. Freezing
       * the list during a drag makes the reference stable and switches that
       * accident off.
       */
      extraData={drag}
      // Measured on the cell rather than on the row; see Zelle above.
      CellRendererComponent={Zelle}
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
        // Guarded, because a row render that throws takes the whole list with
        // it. drag.from indexes the frozen list and should be in range, which
        // is not a reason to crash the screen if it ever is not.
        if (drag && !gezogen && rows[drag.from] && rows[index].band === rows[drag.from].band) {
          if (drag.from < drag.to && index > drag.from && index <= drag.to) versatz = -gezogeneHoehe;
          if (drag.from > drag.to && index >= drag.to && index < drag.from) versatz = gezogeneHoehe;
        }
        return (
          // No onLayout here: it would measure against the cell wrapper this
          // row exactly fills and report y = 0 for every row, overwriting the
          // box Zelle measured on the next re-layout.
          <Animated.View
            style={[
              gezogen ? styles.lifted : null,
              {
                transform: [
                  { translateY: gezogen ? lift : versatz },
                  // The lift's scale is the other half the quietest level
                  // drops, where liftScale is 1, so this reads the table rather
                  // than branching and rides the pick-up spring rather than
                  // appearing in one frame.
                  {
                    scale: gezogen
                      ? hebung.interpolate({ inputRange: [0, 1], outputRange: [1, n.liftScale] })
                      : 1,
                  },
                  {
                    rotate:
                      armed && !gezogen
                        ? wiggle.interpolate({
                            inputRange: [-1, 1],
                            outputRange: [`-${n.wiggleDeg}deg`, `${n.wiggleDeg}deg`],
                          })
                        : '0deg',
                  },
                ],
              },
            ]}
            {...responder.panHandlers}
            /* The long press is timed off the raw touch events rather than with
               a Pressable wrapped around the row. These rows are full of their
               own touchables, a fold chevron, a start badge, a bin, and a child
               that takes the responder on touch-down is a child the parent's
               onLongPress never hears about, so the gesture would work only on
               the parts of the card with no button on them.

               onTouchStart and onTouchEnd are not the responder system: React
               Native dispatches them by bubbling, so they reach this view for a
               touch anywhere inside it, whoever holds the responder. The timer
               starts on any touch on the row and the row's own buttons keep
               working. */
            onTouchStart={(e) => {
              // A new touch while a drag is still live means the last one never
              // ended: a row unmounted mid-gesture, a responder force-
              // terminated, anything. Rather than enumerate the ways, the next
              // touch cleans up, so the list cannot be left in a state where
              // nothing moves any more.
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
            /* Lifting ends it, armed or not: the drag lives as long as the
               touch that started it. A mode that outlived the finger would let
               the next touch anywhere in the list move the row armed minutes
               ago.

               Only when the pan never took over, though. Once it has, the drop
               is onPanResponderRelease's to perform, and both handlers fire for
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
  // Both: Android paints by elevation and iOS by zIndex, and a row that lifts
  // on one platform and slides under its neighbour on the other only shows up
  // on the device somebody else has.
  lifted: { zIndex: 10, elevation: 8, shadowColor: '#000', shadowOpacity: 0.3, shadowRadius: 8, shadowOffset: { width: 0, height: 4 } },
});
