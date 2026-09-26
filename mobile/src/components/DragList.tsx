import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Animated, Easing, PanResponder, StyleSheet, View, type ViewStyle } from 'react-native';
// The cell wrapper's prop shape, taken from the list rather than re-declared,
// since a hand-written copy can drift from the version installed.
import type { CellRendererProps } from '@react-native/virtualized-lists';
import { settle, useMotion } from '../theme/MotionContext';
import { Arrive, MovingList } from './Moving';
import {
  bandFolgt,
  blockEnde,
  landeziel,
  ordneBand,
  reiheNach,
  schrittVon,
  spielraum,
  versatz,
  type Kasten,
} from './dragOrder';

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
 * slide aside as it passes rather than on release, so the gap is where the row
 * would land. On release the row slides into that gap and stays above its
 * neighbours until it gets there. The quietest motion level drops the wiggle
 * and the lift's scale and keeps the shadow and the gap, whose rows then step
 * aside rather than slide.
 *
 * Four mechanics are worth knowing before editing this:
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
 *   - A drop is not over when the finger lifts. The drag stays live until the
 *     row and every neighbour have arrived, and the new order then goes in over
 *     two commits. See landen(), and the effect on `gelandet` for why two.
 */
export interface DragRow {
  key: string;
  /** Rows only reorder within their own band. A link cannot become a package
   *  header's sibling, and a package cannot slide into another package's files. */
  band: string;
  /** The key of the row this one hangs under, set on a link below its open
   *  package header. The link then travels with the header, both when the
   *  header is carried and when it steps aside for another. */
  parent?: string;
  render: (dragging: boolean, armed: boolean) => React.ReactNode;
}

/** A neighbour's own offset, shared with the rows hanging under it so they
 *  move as one. `lauf` counts the springs started on it: a spring replaced
 *  mid-flight reports in after its successor has started, and without the
 *  count would mark the row as arrived while it still moves. */
interface Nachbar {
  wert: Animated.Value;
  ziel: number;
  lauf: number;
  unterwegs: boolean;
}

/** How long a dropped order is held after the write succeeds: long enough for
 *  a relay's round trip, short enough that an order the server settled
 *  differently is not contradicted for long. */
const HALTEN_MS = 3000;

export default function DragList({
  rows: liveRows,
  onReorder,
  style,
  contentContainerStyle,
  header,
  empty,
  lineKey,
}: {
  rows: DragRow[];
  /** Called with the band's new order and the dragged row's key, only when a
   *  drop moved something. The list shows the new order until the rows it is
   *  given agree, the returned promise rejects, or a few seconds after it
   *  resolves. Returning nothing turns the drop down and the row slides back. */
  onReorder: (keys: string[], band: string, moved: string) => Promise<void> | undefined;
  style?: ViewStyle;
  contentContainerStyle?: ViewStyle | ViewStyle[];
  header?: React.ReactNode;
  empty?: React.ReactNode;
  /** Which set of rows is shown; see MovingList. */
  lineKey?: string;
}) {
  /** `gelandet` marks the one render between the drop arriving and the new
   *  order going in; see the effect that reads it. */
  const [drag, setDrag] = useState<{ from: number; to: number; gelandet?: boolean } | null>(null);

  /**
   * How much this list is allowed to move. Read through MotionContext rather
   * than from AccessibilityInfo, because the motion axis has a user-facing
   * level as well as the system signal and the two are resolved together
   * there, with the system signal winning over every level a picker offers.
   *
   * At `off` the wiggle never starts and the lift's scale is 1, while the
   * shadow and the neighbours' gap stay at every level: they tell the eye which
   * row is in the hand and where it would land.
   */
  const { motion, n } = useMotion();
  // Both ride in refs as well, because the wiggle and the drag's end run from
  // callbacks that must not be rebuilt by the state changes the gesture itself
  // causes.
  const bewegung = useRef(n);
  bewegung.current = n;
  const motionRef = useRef(motion);
  motionRef.current = motion;

  /**
   * The order the last drop wrote, shown until the live list agrees. The
   * server's copy arrives a round trip later at best, and until then the row
   * that just landed would jump back and then move again, which reads as a
   * drop that did not take. Laid over the live rows rather than the frozen
   * ones, so the figures inside them keep moving.
   */
  const [abgelegt, setAbgelegt] = useState<{ band: string; keys: string[] } | null>(null);
  const bestaetigt = abgelegt !== null && bandFolgt(liveRows, abgelegt.band, abgelegt.keys);
  useEffect(() => {
    if (bestaetigt) setAbgelegt(null);
  }, [bestaetigt]);

  /**
   * The list is frozen for as long as a drag is armed.
   *
   * The task list keeps streaming while a finger is down, so the rows would be
   * rebuilt and re-sorted under the gesture. Everything downstream is indexed:
   * `drag.from` would point at a different package, `boxes` would hold the
   * measurements of the old order, and the neighbours would not move. From
   * outside that looks like a row lying on top of the others doing nothing.
   *
   * The live list is picked up again when the drag ends, with the dropped band
   * held in its new order until the live list has it too.
   */
  const gefroren = useRef<DragRow[] | null>(null);
  if (drag === null) gefroren.current = null;
  const rows =
    gefroren.current ?? (abgelegt && !bestaetigt ? ordneBand(liveRows, abgelegt.band, abgelegt.keys) : liveRows);
  // The gesture reads this, and it must not wait for a render to know where it
  // is. Assigned only while there is a drag: beenden() clears the ref itself
  // and a render already in flight still carries the old state, so an
  // unconditional assignment would put an ended drag straight back and leave
  // the row lifted with nothing able to move.
  const dragRef = useRef<{ from: number; to: number } | null>(null);
  if (drag !== null) dragRef.current = drag;

  const boxes = useRef<Record<number, Kasten>>({});

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

  /**
   * Built once per level rather than in the row. Animated keys a view's native
   * props on the nodes in its style, so a fresh interpolation on every render
   * puts the view's defaults back on the way, and during a drag every move of
   * the gap is a render: the neighbours would flicker back to rest mid-slide.
   *
   * At the quietest level liftScale is 1, so the scale reads the table rather
   * than branching.
   */
  const skala = useMemo(
    () => hebung.interpolate({ inputRange: [0, 1], outputRange: [1, n.liftScale] }),
    [hebung, n.liftScale],
  );
  const drehung = useMemo(
    () => wiggle.interpolate({ inputRange: [-1, 1], outputRange: [`-${n.wiggleDeg}deg`, `${n.wiggleDeg}deg`] }),
    [wiggle, n.wiggleDeg],
  );

  /** The other rows of the dragged row's band, made fresh in arm(). */
  const nachbarn = useRef(new Map<string, Nachbar>());

  /**
   * The drop in flight: where the row is going, and how many of its two
   * springs, the slide and the scale, are still running. landen() does nothing
   * while one is set, so both handlers that see a lift can call it.
   */
  const landung = useRef<{ d: { from: number; to: number }; ziel: number; offen: number } | null>(null);

  const startWiggle = useCallback(() => {
    // Not started at all rather than started and muted: an infinite animation
    // at the quietest level gets a true stop. What the wiggle says is still
    // said by the row that lifts and by the neighbours stepping aside.
    const b = bewegung.current;
    if (b.wiggleDeg === 0 || b.wiggleDur === 0) return;
    wiggleLoop.current?.stop();
    // GlimStone's glim-wiggle: from one side to the other in wiggleDur and back,
    // eased at both ends, as the web plays it alternating.
    wiggle.setValue(-1);
    const swing = (toValue: number) =>
      Animated.timing(wiggle, { toValue, duration: b.wiggleDur, easing: Easing.inOut(Easing.ease), useNativeDriver: true });
    wiggleLoop.current = Animated.loop(Animated.sequence([swing(1), swing(-1)]));
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
  /** Whether the pan responder actually took the gesture over. Once it has,
   *  its release and its termination end the drag; see onTouchEnd. */
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
    // `lift` still holds the offset the last drop landed on, drawn right up to
    // the render that ended it. The neighbours get fresh values for the same
    // reason.
    lift.setValue(0);
    const band = liste[index].band;
    const neu = new Map<string, Nachbar>();
    liste.forEach((r, i) => {
      if (i === index || r.band !== band) return;
      const nb: Nachbar = { wert: new Animated.Value(0), ziel: 0, lauf: 0, unterwegs: false };
      const ende = blockEnde(liste, i);
      for (let j = i; j < ende; j++) neu.set(liste[j].key, nb);
    });
    nachbarn.current = neu;
    startWiggle();
    // The row rises on the level's own spring: a little overshoot at the top
    // visible level, a longer wobble at the hidden one. At `off` settle()
    // writes the value instead of animating it, and liftScale is 1 there, so
    // the row does not grow.
    hebung.setValue(0);
    settle(hebung, 1, motionRef.current);
  }, [hebung, lift, startWiggle]);

  /** Every spring of a drop calls this as it stops, so the last one through
   *  finishes the drop. */
  const pruefeLandung = useCallback(() => {
    const l = landung.current;
    if (!l || l.offen > 0) return;
    for (const nb of nachbarn.current.values()) if (nb.unterwegs) return;
    // Animated tells the JS side of a natively driven value where it stopped
    // only after this callback, and the render asked for below has to see it.
    lift.setValue(l.ziel);
    hebung.setValue(0);
    for (const nb of nachbarn.current.values()) nb.wert.setValue(nb.ziel);
    setDrag({ ...l.d, gelandet: true });
  }, [hebung, lift]);

  /**
   * Sends each neighbour to where the gap at `to` puts it. A row whose place
   * did not change keeps the spring it has, because a new one would start from
   * a standstill: the JS side never learns the speed of a natively driven one.
   */
  const verteile = useCallback((from: number, to: number) => {
    const r = daten.current.rows;
    const schritt = schrittVon(boxes.current, r, from);
    r.forEach((zeile, i) => {
      // Band rows only. The rows hanging under one share its entry, and their
      // own index would give it a different offset.
      if (zeile.band !== r[from].band) return;
      const nb = nachbarn.current.get(zeile.key);
      const ziel = versatz(i, from, to, schritt);
      if (!nb || nb.ziel === ziel) return;
      nb.ziel = ziel;
      nb.unterwegs = true;
      const lauf = ++nb.lauf;
      settle(nb.wert, ziel, motionRef.current, {
        rest: 0.5,
        done: () => {
          if (nb.lauf !== lauf) return;
          nb.unterwegs = false;
          pruefeLandung();
        },
      });
    });
  }, [pruefeLandung]);

  /**
   * End the drag, from the finished drop or from a new touch that finds a drag
   * still live. `dragRef` is cleared here rather than left to the next render,
   * so a second call finds nothing to end.
   *
   * No offset is zeroed here: the rows keep theirs until the render this asks
   * for takes them away, and an offset zeroed first shows the old order for a
   * frame.
   */
  const beenden = useCallback(() => {
    cancelArm();
    stopWiggle();
    landung.current = null;
    lift.stopAnimation();
    hebung.stopAnimation();
    for (const nb of nachbarn.current.values()) nb.wert.stopAnimation();
    panning.current = false;
    dragRef.current = null;
    setDrag(null);
  }, [cancelArm, hebung, lift, stopWiggle]);

  /**
   * The drop's last step, one commit after everything arrived. The native
   * driver moves the rows without telling React, so React's copy of an offset
   * can still be zero while the screen shows the row a slot away.
   * pruefeLandung() commits the true offsets; this render then zeroes them in
   * the same commit that puts the new order in, so layout and offset reach the
   * screen together. In one step, a row whose copy was already zero would have
   * nothing to update and would sit a slot away.
   */
  useEffect(() => {
    if (drag?.gelandet) beenden();
  }, [drag, beenden]);

  /**
   * Let go of the row: it slides into the gap and the order is written if it
   * moved. `zurueck` sends it home and writes nothing, for a gesture the system
   * took away rather than one the finger finished.
   *
   * Two handlers see a lift, the pan's release and the row's onTouchEnd, and
   * React Native does not promise which runs first; `landung` makes the second
   * call a no-op. The drag stays live until the row arrives, so its shadow and
   * its place above the neighbours last the whole slide.
   */
  const landen = useCallback((zurueck: boolean) => {
    const d = dragRef.current;
    if (!d || landung.current) return;
    cancelArm();
    stopWiggle();
    // Asked before the row sets off, so a drop the caller turns down slides
    // home rather than into a gap nothing will fill.
    const { rows: r, onReorder: melde } = daten.current;
    const reihe = zurueck ? null : reiheNach(r, d.from, d.to);
    const schreiben = reihe ? melde(reihe.keys, reihe.band, r[d.from].key) : undefined;
    const to = schreiben ? d.to : d.from;
    const ziel = landeziel(boxes.current, r, d.from, to);
    const l = { d: { from: d.from, to }, ziel, offen: 2 };
    landung.current = l;

    const angekommen = () => {
      if (landung.current !== l) return;
      l.offen -= 1;
      pruefeLandung();
    };
    verteile(d.from, to);
    const m = motionRef.current;
    // Arrived within half a point for the slide and a hundredth of the pick-up
    // for the scale, both too small to see.
    settle(hebung, 0, m, { rest: 0.01, done: angekommen });
    settle(lift, ziel, m, { rest: 0.5, done: angekommen });

    if (!reihe || !schreiben) return;
    setAbgelegt(reihe);
    const loslassen = () => setAbgelegt((a) => (a === reihe ? null : a));
    schreiben.then(() => {
      setTimeout(loslassen, HALTEN_MS);
    }, loslassen);
  }, [cancelArm, hebung, lift, pruefeLandung, stopWiggle, verteile]);

  // Through a ref, so the one long-lived PanResponder never captures a stale
  // copy of them.
  const griffe = useRef({ beenden, landen, verteile });
  griffe.current = { beenden, landen, verteile };

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
      // Not while a drop is landing either: a touch then is onTouchStart's to
      // clean up, not a second drag of a row already on its way.
      onStartShouldSetPanResponderCapture: () => dragRef.current !== null && !landung.current,
      onMoveShouldSetPanResponderCapture: () => dragRef.current !== null && !landung.current,
      // The list must not be able to take the gesture back mid-drag.
      onPanResponderTerminationRequest: () => false,
      onPanResponderGrant: () => {
        panning.current = true;
        lift.setValue(0);
      },
      onPanResponderMove: (_e, g) => {
        const d = dragRef.current;
        if (!d) return;
        const { rows: r, indexAt: finde } = daten.current;
        const [oben, unten] = spielraum(boxes.current, r, d.from);
        const dy = Math.min(unten, Math.max(oben, g.dy));
        lift.setValue(dy);
        const b = boxes.current[d.from];
        const zeile = r[d.from];
        if (!b || !zeile) return;
        const to = finde(b.y + b.h / 2 + dy, zeile.band, d.from);
        if (to === d.to) return;
        // Into the ref as well as into state: the neighbours start moving at
        // once, and a release before the next render has to land in the gap
        // they are moving to.
        dragRef.current = { from: d.from, to };
        setDrag(dragRef.current);
        griffe.current.verteile(d.from, to);
      },
      onPanResponderRelease: () => {
        panning.current = false;
        griffe.current.landen(false);
      },
      // Taken away rather than let go: the row goes home and nothing is
      // written, as an escape does it on the web.
      onPanResponderTerminate: () => {
        panning.current = false;
        griffe.current.landen(true);
      },
    }),
  ).current;

  // The rows hanging under the dragged row are carried with it.
  const tragEnde = drag ? blockEnde(rows, drag.from) : 0;

  return (
    <MovingList
      style={style}
      lineKey={lineKey}
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
        const gezogen = drag !== null && index >= drag.from && index < tragEnde;
        const armed = drag !== null;
        const nachbar = drag ? nachbarn.current.get(item.key) : undefined;
        return (
          // The lift sits on the outermost view of the row, the arrival's, since
          // a stacking order only counts among siblings and the neighbours'
          // rows are siblings of that view, not of the one inside it.
          //
          // No onLayout here: it would measure against the cell wrapper this
          // row exactly fills and report y = 0 for every row, overwriting the
          // box Zelle measured on the next re-layout.
          <Arrive style={gezogen ? styles.lifted : null}>
            <Animated.View
              style={{
                transform: [
                  // A plain zero once the drag is over, not the values left at
                  // the offsets they landed on; see the effect on `gelandet`.
                  { translateY: gezogen ? lift : (nachbar?.wert ?? 0) },
                  { scale: gezogen ? skala : 1 },
                  { rotate: armed && !gezogen ? drehung : '0deg' },
                ],
              }}
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
                // ended or is still landing: a row unmounted mid-gesture, a
                // responder force-terminated, a tap during the slide. Rather than
                // enumerate the ways, the next touch cleans up, so the list cannot
                // be left in a state where nothing moves any more.
                if (dragRef.current) {
                  griffe.current.beenden();
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

                 Only when the pan never took over, though. Once it has, its
                 release and its termination tell a drop from a gesture taken
                 away, which a raw touch end cannot, and landen() already ignores
                 whichever of two calls for one lift comes second. */
              onTouchEnd={() => {
                cancelArm();
                if (!panning.current) griffe.current.landen(false);
              }}
              onTouchCancel={() => {
                cancelArm();
                if (!panning.current) griffe.current.landen(true);
              }}
            >
              {item.render(gezogen, armed)}
            </Animated.View>
          </Arrive>
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
