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
 * settle brings an Animated.Value home, on the level's own spring.
 *
 * This is where GlimStone 1.17.0's springDamping is spent: 0.68 at the top
 * visible level swings a little past and comes back, 0.34 keeps doing it for
 * longer, and the web reaches the same two shapes with two cubic-beziers. At
 * `off` there is no spring and the value arrives.
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
