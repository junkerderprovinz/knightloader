import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { AccessibilityInfo, Animated, Easing } from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import {
  DEFAULT_MOTION,
  asMotion,
  motionNumbers,
  resolveMotion,
  springConfig,
  type Motion,
  type MotionNumbers,
} from './motion';
import { springOf } from './motionNative';

// The accessibility gate, and the only place in this app that reads it.
//
// On the web the motion levels live inside `@media (prefers-reduced-motion:
// no-preference)`, so a browser whose owner asked the system for less motion
// never evaluates a `data-motion` selector, and the signal wins by where the
// block sits rather than by a check in front of each animation. A phone has no
// such block, so the guarantee is built from two parts: one subscriber to
// AccessibilityInfo, and one function (motion.motionNumbers) that every level
// passes through.
//
// A screen that asked AccessibilityInfo for itself would be a second answer to
// the same question, and the one that forgets to ask is the one that animates.
// check-hidden-motion-level.mjs fails if a second reader appears.
//
// A provider separate from AppearanceContext rather than four more fields on
// it. Appearance is what the instance may lead on, colour, corners and the
// palette; motion is a property of this phone and the person holding it, never
// travels over the wire, and half of it is an operating-system setting no
// server has business overriding.

export interface MotionState {
  /**
   * What the picker shows as active: the stored choice, not what is drawing.
   *
   * The two differ while the system asks for less motion, and showing the
   * resolved value there would rewrite somebody's setting on screen: they would
   * open the settings, see "off" selected, and have no way to tell the app had
   * chosen that for them.
   */
  chosen: Motion;
  /** What actually draws. `off` for as long as the system asks for less,
   *  unless the storm is chosen. */
  motion: Motion;
  /** The numbers to draw with. Nothing that animates reads anything else. */
  n: MotionNumbers;
  /** Whether the system asks for less motion. It is in charge of every level
   *  except the storm, and the settings screen says so rather than leaving
   *  somebody to wonder why picking the liveliest level changed nothing. */
  reduced: boolean;
  setMotion: (m: Motion) => void;
}

const STORE_KEY = 'glim-motion';

const MotionCtx = createContext<MotionState | null>(null);

export function MotionProvider({ children }: { children: ReactNode }) {
  const [chosen, setChosen] = useState<Motion>(DEFAULT_MOTION);
  const [reduced, setReduced] = useState(false);

  // Read once at start through asMotion, which validates against the stored
  // list rather than the picker's, so a storm found yesterday comes back today.
  // Not awaited before the first paint: the worst case is one screen at the
  // default level before the chosen one arrives.
  useEffect(() => {
    AsyncStorage.getItem(STORE_KEY)
      .then((raw) => raw && setChosen(asMotion(raw)))
      .catch(() => {
        /* an unreadable level is no level, never a crash */
      });
  }, []);

  useEffect(() => {
    let alive = true;
    void AccessibilityInfo.isReduceMotionEnabled().then((on) => {
      if (alive) setReduced(on);
    });
    const sub = AccessibilityInfo.addEventListener('reduceMotionChanged', setReduced);
    return () => {
      alive = false;
      sub.remove();
    };
  }, []);

  const setMotion = useCallback((m: Motion) => {
    setChosen(m);
    // Fire and forget: the value is already in state and on screen, and a
    // failed write costs the choice at next launch, not now. The chosen value
    // is the only thing that goes to storage; storing the fact that somebody
    // found the hidden level would turn a secret into an entry in this picker.
    void AsyncStorage.setItem(STORE_KEY, m).catch(() => {});
  }, []);

  const value = useMemo<MotionState>(
    () => ({ chosen, motion: resolveMotion(chosen, reduced), n: motionNumbers(chosen, reduced), reduced, setMotion }),
    [chosen, reduced, setMotion],
  );

  return <MotionCtx.Provider value={value}>{children}</MotionCtx.Provider>;
}

export function useMotion(): MotionState {
  const v = useContext(MotionCtx);
  if (!v) throw new Error('useMotion must be used inside MotionProvider');
  return v;
}

/**
 * The refusal gesture, as one routine for the whole app.
 *
 * It lives beside the motion table rather than beside the shared controls in
 * components/glim.tsx, because a shake spends that table and the controls file
 * would otherwise depend on this context to draw a refusal.
 *
 * The geometry is the house's: `glim-shake` is a translateX oscillation
 * decaying +-1, -+1, +-.5, -+.5 through five equal segments, while how long and
 * how far are the level's business.
 *
 * At `off` there is no travel, and an invisible shake carries no signal, so it
 * swaps for a brief dip in opacity, the substitution the language prescribes
 * under system-level reduced motion. A control that refuses silently cannot be
 * told apart from one that did not hear the press.
 */
export function useShake(): { style: { transform: { translateX: Animated.AnimatedInterpolation<number> }[]; opacity: Animated.AnimatedInterpolation<number> | number }; shake: () => void } {
  const { n } = useMotion();
  const v = useRef(new Animated.Value(0)).current;
  // The numbers ride in a ref as well, so a level change does not rebuild the
  // callback and force it back out to every caller.
  const live = useRef(n);
  live.current = n;

  const shake = useCallback(() => {
    const cur = live.current;
    v.setValue(0);
    if (cur.shakeTravel === 0) {
      const half = cur.shakeDur / 2;
      Animated.sequence([
        Animated.timing(v, { toValue: 1, duration: half, easing: Easing.out(Easing.ease), useNativeDriver: true }),
        Animated.timing(v, { toValue: 0, duration: half, easing: Easing.in(Easing.ease), useNativeDriver: true }),
      ]).start();
      return;
    }
    const step = cur.shakeDur / 5;
    Animated.sequence(
      [1, -1, 0.5, -0.5, 0].map((to) =>
        Animated.timing(v, { toValue: to, duration: step, easing: Easing.inOut(Easing.ease), useNativeDriver: true }),
      ),
    ).start();
  }, [v]);

  const style = useMemo(
    () => ({
      transform: [{ translateX: v.interpolate({ inputRange: [-1, 1], outputRange: [-n.shakeTravel, n.shakeTravel] }) }],
      // A plain 1 whenever the travel says it: interpolating an opacity that
      // never moves would put a whole extra animated node on a control for
      // nothing.
      opacity:
        n.shakeFadeTo === 1 ? 1 : v.interpolate({ inputRange: [0, 1], outputRange: [1, n.shakeFadeTo] }),
    }),
    [v, n.shakeTravel, n.shakeFadeTo],
  );

  return { style, shake };
}

/**
 * The success gesture, the shake's opposite, for a copy or a save that landed:
 * the control swells a little and settles, on the web's `glim-confirm` timing.
 * Only its scale half is drawn, since the glow ring the web puts around the
 * control has no counterpart on a phone.
 *
 * Put `style` on an Animated.View around the control, or spread its transform
 * into one the view already has. At `off` nothing moves.
 */
export function useConfirm(): { style: { transform: { scale: Animated.Value }[] }; confirm: () => void } {
  const { n } = useMotion();
  const scale = useRef(new Animated.Value(1)).current;
  const live = useRef(n);
  live.current = n;

  const confirm = useCallback(() => {
    const cur = live.current;
    if (cur.confirmDur === 0 || cur.confirmScale === 0) return;
    scale.setValue(1);
    Animated.sequence([
      Animated.timing(scale, {
        toValue: 1 + cur.confirmScale * 0.05,
        duration: cur.confirmDur * 0.4,
        easing: Easing.out(Easing.ease),
        useNativeDriver: true,
      }),
      Animated.timing(scale, {
        toValue: 1,
        duration: cur.confirmDur * 0.6,
        easing: Easing.out(Easing.ease),
        useNativeDriver: true,
      }),
    ]).start();
  }, [scale]);

  const style = useMemo(() => ({ transform: [{ scale }] }), [scale]);
  return { style, confirm };
}

/**
 * A button or card giving way under the finger: it scales to the level's
 * `press` while held and springs back on `bounce` when let go, so a tap lands
 * at the top levels and barely registers at `subtle`.
 *
 * Spread `onPressIn` and `onPressOut` onto the touchable and put `scale` in the
 * transform of the view that should shrink. The handlers read the numbers
 * through a ref, so they stay the same functions for the life of the control.
 */
export function usePress(): { scale: Animated.Value; onPressIn: () => void; onPressOut: () => void } {
  const { n } = useMotion();
  const scale = useRef(new Animated.Value(1)).current;
  const live = useRef(n);
  live.current = n;
  // Whether this press shrank the control, so the release springs it back even
  // when the level was turned down while the finger was still on it.
  const shrunk = useRef(false);

  return useMemo(() => {
    const to = (value: number) =>
      Animated.spring(scale, { toValue: value, useNativeDriver: true, ...springOf(live.current.bounce) }).start();
    return {
      scale,
      onPressIn: () => {
        if (live.current.press === 1) return;
        shrunk.current = true;
        to(live.current.press);
      },
      onPressOut: () => {
        if (!shrunk.current) return;
        shrunk.current = false;
        to(1);
      },
    };
  }, [scale]);
}

/**
 * settle brings an Animated.Value home, on the level's own spring.
 *
 * The spring is the level's layout `damping` from GlimStone's table: 0.68 at
 * the top visible level swings a little past and comes back, 0.34 keeps doing
 * it for longer, and the web reaches the same two shapes with two
 * cubic-beziers. At `off` there is no spring and the value arrives.
 *
 * `done` runs when the spring ends, straight away at `off`. A spring that is
 * stopped or replaced by another ends too, before its target, so a caller that
 * starts several checks the call is still its own.
 *
 * `rest` is how close counts as there, in the value's own units: Animated's
 * default of a thousandth suits a value between 0 and 1 but keeps an offset in
 * points going for most of a second after it has visibly arrived. The speed
 * threshold scales with it (a third of that distance per frame), or a spring
 * swinging through its target would count as arrived mid-swing.
 */
export function settle(
  value: Animated.Value,
  to: number,
  m: Motion,
  { rest, done }: { rest?: number; done?: () => void } = {},
): void {
  const spring = springConfig(m);
  if (!spring) {
    value.setValue(to);
    done?.();
    return;
  }
  const thresholds = rest === undefined ? {} : { restDisplacementThreshold: rest, restSpeedThreshold: rest * 20 };
  Animated.spring(value, { toValue: to, ...spring, ...thresholds, useNativeDriver: true }).start(done);
}
