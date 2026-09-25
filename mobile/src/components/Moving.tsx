import { NavigationContext } from '@react-navigation/native';
import { createContext, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import {
  Animated,
  FlatList,
  ScrollView,
  StyleSheet,
  View,
  type FlatListProps,
  type NativeScrollEvent,
  type NativeSyntheticEvent,
  type ScrollViewProps,
  type StyleProp,
  type ViewStyle,
} from 'react-native';
import { useMotion } from '../theme/MotionContext';
import { arrivalDelay, arrivalSide, edgeReach, springOf } from '../theme/motionNative';

/**
 * How a page moves, as "On a phone" in GlimStone's design language has it: its
 * cards arrive, and a flung page runs on past its edge and springs back.
 *
 * Every card of a page takes the next place in a line as it mounts and springs
 * up into it after its place's delay, swinging in from alternating sides at the
 * top levels. A screen stays mounted while another is pushed over it, so the
 * whole line plays again each time it comes back into view. A card that mounts
 * after its list was scrolled came in by scrolling and appears without flying
 * in, and it still joins the next replay.
 *
 * A card that mounts after the page, such as a row of a folder just opened or
 * of a list whose data came late, counts its first delay among the cards that
 * mounted with it, so it does not wait out the stagger of the whole page. Its
 * place in the line decides every replay.
 */
type Slot = { place: number; inBatch: number };
type Line = { take: () => Slot; round: number; scrolled: { current: boolean } };

const Arrival = createContext<Line | null>(null);

/**
 * The line of one page. `round` counts the times its screen came into view,
 * starting at -1 for a page mounted under a screen that is not focused. A page
 * that mounts while its screen is focused starts at 0 at once: the focus event
 * it would wait for has already fired.
 *
 * `lineKey` names the set of rows the page shows. When it changes, as when the
 * download list switches to the collector, the new rows fly in however far the
 * old ones were scrolled, and they number from the front of the line again.
 */
function useLine(lineKey?: string): Line {
  const nav = useContext(NavigationContext);
  const [round, setRound] = useState(() => (!nav || nav.isFocused() ? 0 : -1));
  // Only a return starts a replay. The focus event that follows a screen's own
  // mount would otherwise start every card a second time.
  const away = useRef(round < 0);
  useEffect(() => {
    if (!nav) return;
    const offBlur = nav.addListener('blur', () => {
      away.current = true;
    });
    const offFocus = nav.addListener('focus', () => {
      if (!away.current) return;
      away.current = false;
      setRound((r) => r + 1);
    });
    return () => {
      offBlur();
      offFocus();
    };
  }, [nav]);

  const next = useRef(0);
  const scrolled = useRef(false);
  // The place the current batch started at. The cards one commit mounts are
  // taken in one synchronous render, so the batch closes with that task.
  const batch = useRef<number | null>(null);
  const shown = useRef(lineKey);
  if (shown.current !== lineKey) {
    shown.current = lineKey;
    next.current = 0;
    batch.current = null;
    scrolled.current = false;
  }
  return useMemo(() => {
    const take = (): Slot => {
      const place = next.current++;
      if (batch.current === null) {
        batch.current = place;
        setTimeout(() => {
          batch.current = null;
        }, 0);
      }
      return { place, inBatch: place - batch.current };
    };
    return { take, round, scrolled };
  }, [round]);
}

/** The moving style of one card on a page, or null outside one. */
function useArrival() {
  const line = useContext(Arrival);
  const { n } = useMotion();
  const slot = useRef<Slot | null>(null);
  const late = useRef(false);
  if (line && slot.current === null) {
    slot.current = line.take();
    late.current = line.scrolled.current;
  }
  const v = useRef(new Animated.Value(line && n.travel > 0 && !late.current ? 0 : 1)).current;
  const round = line?.round;
  const born = useRef(round);

  useEffect(() => {
    if (round === undefined || n.travel === 0 || (late.current && round === born.current)) {
      v.setValue(1);
      return;
    }
    v.setValue(0);
    if (round < 0) return;
    // A round comes from a line, and the line handed out the slot.
    const { place, inBatch } = slot.current!;
    const run = Animated.spring(v, {
      toValue: 1,
      delay: arrivalDelay(round === born.current ? inBatch : place, n),
      useNativeDriver: true,
      ...springOf(n.bounce),
    });
    run.start();
    return () => run.stop();
  }, [round, n, v]);

  // Built once per level: Animated keys a view's native props on the nodes in
  // its style, and a fresh interpolation on every render puts the view back at
  // rest mid-flight, which a list row that re-renders during a drag would show.
  //
  // Translation only, because a card holds controls that move on their own, and
  // the opacity is full a third of the way in so the overshoot never fades.
  const side = arrivalSide(slot.current?.place ?? 0);
  const style = useMemo(
    () => ({
      opacity: v.interpolate({ inputRange: [0, 0.35, 1], outputRange: [0, 1, 1], extrapolate: 'clamp' }),
      transform: [
        { translateY: v.interpolate({ inputRange: [0, 1], outputRange: [n.travel, 0] }) },
        { translateX: v.interpolate({ inputRange: [0, 1], outputRange: [side * n.sway, 0] }) },
      ],
    }),
    [v, n.travel, n.sway, side],
  );
  return line ? style : null;
}

/**
 * What a scrolling page shares: the line for its cards and the spring at either
 * end, where the whole page runs on by edgeReach() and swings back on `bounce`.
 * The page moves rather than its content, since a virtualised list has no one
 * content box to move. Where the level has no edge the platform's own
 * overscroll stays; where it has one that effect is off, so the two never play
 * at once.
 */
function useScrollMotion(lineKey?: string) {
  const { n } = useMotion();
  const line = useLine(lineKey);
  const shift = useRef(new Animated.Value(0)).current;
  const live = useRef(n);
  live.current = n;
  const { scrolled } = line;

  const handlers = useMemo(() => {
    let resting: 'top' | 'bottom' | null = 'top';
    // Only a finger or a fling hits an edge. A list whose content shrinks under
    // it reports a scroll to the edge too, and a folder closing at the bottom
    // of the downloads must not bounce the page.
    let moving = false;
    return {
      onScroll: (e: NativeSyntheticEvent<NativeScrollEvent>) => {
        const m = live.current;
        const { contentOffset, contentSize, layoutMeasurement, velocity } = e.nativeEvent;
        if (contentOffset.y > 0) scrolled.current = true;
        const at =
          contentOffset.y <= 0
            ? 'top'
            : contentOffset.y + layoutMeasurement.height >= contentSize.height - 1
              ? 'bottom'
              : null;
        if (at && at !== resting && moving && m.edge > 0) {
          Animated.sequence([
            Animated.timing(shift, { toValue: edgeReach(at, velocity?.y ?? 0, m), duration: 90, useNativeDriver: true }),
            Animated.spring(shift, { toValue: 0, useNativeDriver: true, ...springOf(m.bounce) }),
          ]).start();
        }
        resting = at;
      },
      onScrollBeginDrag: () => {
        moving = true;
      },
      onScrollEndDrag: () => {
        moving = false;
      },
      onMomentumScrollBegin: () => {
        moving = true;
      },
      onMomentumScrollEnd: () => {
        moving = false;
      },
    };
  }, [shift, scrolled]);

  return {
    line,
    shift,
    scroll: {
      ...handlers,
      scrollEventThrottle: 16,
      overScrollMode: n.edge > 0 ? ('never' as const) : ('auto' as const),
      bounces: n.edge === 0,
    },
  };
}

/** The props a moving page sets itself, so a caller cannot pass a second answer. */
type Owned =
  | 'onScroll'
  | 'onScrollBeginDrag'
  | 'onScrollEndDrag'
  | 'onMomentumScrollBegin'
  | 'onMomentumScrollEnd'
  | 'scrollEventThrottle'
  | 'overScrollMode'
  | 'bounces';

/**
 * The page's slot and the box that moves inside it. The slot clips, so a page
 * running past its bottom edge never slides over the top bar above it.
 */
function Edge({ shift, children }: { shift: Animated.Value; children: ReactNode }) {
  return (
    <View style={styles.slot}>
      <Animated.View style={[styles.fill, { transform: [{ translateY: shift }] }]}>{children}</Animated.View>
    </View>
  );
}

/** A FlatList whose rows arrive like a page's cards and which springs at either end. */
export function MovingList<T>({ lineKey, ...props }: Omit<FlatListProps<T>, Owned> & { lineKey?: string }) {
  const { line, shift, scroll } = useScrollMotion(lineKey);
  return (
    <Arrival.Provider value={line}>
      <Edge shift={shift}>
        <FlatList<T> {...props} {...scroll} />
      </Edge>
    </Arrival.Provider>
  );
}

/** A scrolling page whose cards arrive and which springs at either end. */
export function MovingScroll({ children, ...props }: Omit<ScrollViewProps, Owned>) {
  const { line, shift, scroll } = useScrollMotion();
  return (
    <Edge shift={shift}>
      <ScrollView {...props} {...scroll}>
        <Arrival.Provider value={line}>{children}</Arrival.Provider>
      </ScrollView>
    </Edge>
  );
}

/** A card or a list row taking its place in the page's line; a plain view anywhere else. */
export function Arrive({ style, children }: { style?: StyleProp<ViewStyle>; children: ReactNode }) {
  const arrival = useArrival();
  return <Animated.View style={[style, arrival]}>{children}</Animated.View>;
}

/**
 * A window over the page. Its cards are not the page's, and without this they
 * would take a place at the end of its line when the window opens.
 */
export function NoArrival({ children }: { children: ReactNode }) {
  return <Arrival.Provider value={null}>{children}</Arrival.Provider>;
}

const styles = StyleSheet.create({
  slot: { flex: 1, overflow: 'hidden' },
  fill: { flex: 1 },
});
