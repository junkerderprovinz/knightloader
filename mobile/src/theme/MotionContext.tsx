import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { AccessibilityInfo, Animated, Easing } from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import {
  DEFAULT_MOTION,
  MOTION,
  asMotion,
  resolveMotion,
  springConfig,
  type Motion,
  type MotionNumbers,
} from './motion';

// THE ACCESSIBILITY GATE, and the only place in this app that reads it.
//
// On the web the motion levels live inside `@media (prefers-reduced-motion:
// no-preference)`, so a browser whose owner asked the system for less motion
// never evaluates a `data-motion` selector at all - the signal wins because of
// WHERE THE BLOCK SITS, not because of a check in front of each animation. A
// phone has no such block, so the same guarantee has to be built out of two
// parts: one subscriber to AccessibilityInfo, and one function
// (motion.resolveMotion) that every level passes through.
//
// That is why this file is the only reader. A screen that asked
// AccessibilityInfo for itself would be a second answer to the same question,
// and the one that forgets to ask is the one that animates - which is exactly
// the failure this arrangement makes impossible rather than merely discouraged.
// check-hidden-motion-level.mjs fails if a second reader appears.
//
// It is deliberately a SEPARATE provider from AppearanceContext rather than
// four more fields on it. Appearance is what the instance may lead on - colour,
// corners, the palette - and motion is not: it is a property of this phone and
// the person holding it, it never travels over the wire, and half of it is an
// operating-system setting no server has any business overriding.

export interface MotionState {
  /**
   * What the PICKER shows as active: the stored choice, not what is drawing.
   *
   * The two are different exactly when the system asks for less motion, and
   * showing the resolved value there would silently rewrite somebody's setting
   * on screen - they would open the settings, see "off" selected, and have no
   * way to tell the app had chosen that for them.
   */
  chosen: Motion;
  /** What actually draws. `off` for as long as the system asks for less. */
  motion: Motion;
  /** The numbers for `motion`. Nothing that animates reads anything else. */
  n: MotionNumbers;
  /** Whether the system is the one in charge right now. The settings screen
   *  says so in a line rather than leaving somebody to wonder why picking the
   *  liveliest level changed nothing. */
  reduced: boolean;
  setMotion: (m: Motion) => void;
}

const STORE_KEY = 'glim-motion';

const MotionCtx = createContext<MotionState | null>(null);

export function MotionProvider({ children }: { children: ReactNode }) {
  const [chosen, setChosen] = useState<Motion>(DEFAULT_MOTION);
  const [reduced, setReduced] = useState(false);

  // Read once at start, and through asMotion, which validates against the
  // STORED list rather than the picker's - so a storm somebody found yesterday
  // comes back today. Not awaited before the first paint: the default is the
  // top visible level, so the worst case is one screen at full liveliness
  // before a quieter choice arrives.
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
    // failed write costs the choice at next launch, not now. The CHOSEN value
    // is the only thing that ever goes to storage - never the fact that
    // somebody found the hidden level, which is what would turn a secret into a
    // permanent entry in this picker.
    void AsyncStorage.setItem(STORE_KEY, m).catch(() => {});
  }, []);

  const value = useMemo<MotionState>(() => {
    const motion = resolveMotion(chosen, reduced);
    return { chosen, motion, n: MOTION[motion], reduced, setMotion };
  }, [chosen, reduced, setMotion]);

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
 * IT WAS TYPED OUT TWICE before this existed, in RelayConnectScreen and in
 * SettingsScreen, and the second copy carried a note saying so and asking for
 * exactly this. That note put it in components/glim.tsx, beside the shared
 * controls; it lives here instead, and the reason is the thing that changed
 * since: a shake is now a spend of the motion table, so putting it beside the
 * buttons would make the controls file depend on this context to draw a
 * refusal. The substance of that note - one routine, not three - is what
 * mattered, and it is honoured.
 *
 * THE GEOMETRY IS THE HOUSE'S. `glim-shake` is a translateX oscillation
 * decaying +-1, -+1, +-.5, -+.5 through five equal segments, and how long and
 * how far are the level's business rather than this function's.
 *
 * At `off` there is no travel at all, and an invisible shake carries no
 * "rejected" signal - so it swaps for a brief dip in opacity, which is the same
 * substitution the language prescribes under system-level reduced motion. Never
 * nothing: a control that refuses silently is indistinguishable from one that
 * did not hear the press.
 */
export function useShake(): { style: { transform: { translateX: Animated.AnimatedInterpolation<number> }[]; opacity: Animated.AnimatedInterpolation<number> | number }; shake: () => void } {
  const { n } = useMotion();
  const v = useRef(new Animated.Value(0)).current;
  // The numbers ride in a ref as well, so the callback does not have to be
  // rebuilt - and therefore does not have to be re-passed to every caller -
  // every time the level changes.
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
 * This is where GlimStone 1.17.0's springDamping is actually spent: 0.68 at the
 * top visible level swings a little past and comes back, 0.34 keeps doing it
 * for noticeably longer, and the web reaches those same two shapes with two
 * cubic-beziers. At `off` there is no spring at all and the value simply
 * arrives, which is what that level means.
 */
export function settle(value: Animated.Value, to: number, m: Motion): void {
  const spring = springConfig(m);
  if (!spring) {
    value.setValue(to);
    return;
  }
  Animated.spring(value, { toValue: to, ...spring, useNativeDriver: true }).start();
}
