// How much this app moves, as numbers rather than as animations. A level is a
// different number, never a different animation, so this file is a table of
// figures and a few small functions, and every component that moves reads the
// table instead of typing a duration of its own.
//
// Most of the table is GlimStone's: motionNative.ts is the design language's
// reference copied as it is, so the arrival, the press, the edge spring and the
// springs themselves move the same in every app of the family. What this file
// adds is the gestures only this app has.
//
// On the web the engine is CSS custom properties resolved inside
// `@media (prefers-reduced-motion: no-preference)`, so the accessibility signal
// wins by where the block sits. React Native has neither, so the same property
// is rebuilt from two parts: the signal is read in one place
// (MotionContext.tsx), and every level passes through motionNumbers below,
// which answers `off` for the offered levels while the signal is on.
// check-hidden-motion-level.mjs proves both.
//
// Free of React and of react-native, the same split theme/appearance.ts uses,
// which also lets the check script import and run this.
import { NATIVE_MOTION, springOf, type MotionLevel, type NativeMotion } from './motionNative';

/**
 * The levels, quietest first. `storm` is a real level with real numbers, see
 * the MOTION table, that no picker offers.
 */
export type Motion = MotionLevel;

/** What a picker shows. The storm is not in here; see stormTap below. */
export const MOTION_LEVELS: Motion[] = ['off', 'subtle', 'wild'];

/**
 * What a stored value may say, which is a different list. A persisted storm is
 * accepted at launch although no picker offers it, or the gesture would produce
 * a setting that forgets itself the next time the app is opened.
 */
export const MOTION_STORED: Motion[] = [...MOTION_LEVELS, 'storm'];

/**
 * The default is the middle level (GlimStone 2.1.0). The top one is a
 * statement rather than polish, and somebody who wants it picks it. A default
 * reaches only people who never chose, so a stored `wild` stays `wild`. The
 * system's own reduced-motion setting wins over it either way.
 */
export const DEFAULT_MOTION: Motion = 'subtle';

/** How many taps on the level already chosen open the one below the floor. */
export const STORM_TAPS = 5;

/**
 * The gestures this app has and the reference does not, one figure per level.
 *
 * Every level carries every figure. A level that leaves one out inherits the
 * top level's value and arrives as a slightly slower version of it, which is
 * how a fourth intensity ends up reported as doing nothing. The check script
 * compares the key sets.
 */
interface Gestures {
  /** A control refusing an action: how long the whole gesture takes, in ms. */
  shakeDur: number;
  /** How far that refusal swings, in points, at its widest. Zero at `off`. */
  shakeTravel: number;
  /** What the refusal fades to when it cannot travel. An invisible shake
   *  carries no signal, so `off` swaps the movement for a brief dip in opacity,
   *  the substitution the language prescribes under system reduced motion. 1
   *  leaves the signal to the travel. */
  shakeFadeTo: number;
  /** The "this list is editable now" wiggle: half its swing, in degrees. */
  wiggleDeg: number;
  /** One full wiggle cycle, in ms. Zero means the loop never starts. */
  wiggleDur: number;
  /** The scale a row takes while it is in the hand. */
  liftScale: number;
}

/** Everything a component draws with: GlimStone's numbers and this app's own. */
export type MotionNumbers = NativeMotion & Gestures;

const GESTURES: Record<Motion, Gestures> = {
  // The manual equivalent of system-level reduced motion, not a second recipe
  // tuned separately: the same numbers that signal already produces.
  off: {
    shakeDur: 220,
    shakeTravel: 0,
    shakeFadeTo: 0.45,
    wiggleDeg: 0,
    wiggleDur: 0,
    liftScale: 1,
  },
  // Smaller numbers, the same gestures. Somebody who asked for less movement
  // asked for less movement, not for a faster one, so the swing halves.
  subtle: {
    shakeDur: 220,
    shakeTravel: 2,
    shakeFadeTo: 1,
    wiggleDeg: 0.35,
    // The period stays where the top level has it. An ambient loop is not
    // calmed by running it faster, only by moving less, which is the same
    // arrangement the web's subtle block makes for this figure.
    wiggleDur: 320,
    liftScale: 1.015,
  },
  // The top level a picker offers, and the app's shipped numbers: the 360ms/4pt
  // shake the whole family draws, the 0.7 degree wiggle the web's token names,
  // the 1.03 lift.
  wild: {
    shakeDur: 360,
    shakeTravel: 4,
    shakeFadeTo: 1,
    wiggleDeg: 0.7,
    // The web's --motion-wiggle-dur is .32s, and one gesture having two shapes
    // across one product is what these numbers are written down to prevent.
    wiggleDur: 320,
    liftScale: 1.03,
  },
  // The hidden fourth (GlimStone 1.17.0). Same gestures, same table, bigger
  // figures. The multipliers are the web's own storm ladder rather than
  // invented here: shake 1.8x the travel and 520ms, wiggle 1.8x the swing.
  storm: {
    shakeDur: 520,
    shakeTravel: 7.2,
    shakeFadeTo: 1,
    wiggleDeg: 1.3,
    // The one period that goes down. An ambient loop gets livelier by running
    // faster, the same reasoning that leaves it alone at subtle, where moving
    // less is the only way to calm it. 320 against the web's pulse ratio of
    // 1.4s to 2s.
    wiggleDur: 224,
    liftScale: 1.054,
  },
};

/** Every level's whole set. The reference comes first and nothing here overrides it. */
export const MOTION: Record<Motion, MotionNumbers> = {
  off: { ...NATIVE_MOTION.off, ...GESTURES.off },
  subtle: { ...NATIVE_MOTION.subtle, ...GESTURES.subtle },
  wild: { ...NATIVE_MOTION.wild, ...GESTURES.wild },
  storm: { ...NATIVE_MOTION.storm, ...GESTURES.storm },
};

/**
 * asMotion turns a stored string back into a level. Checked against
 * MOTION_STORED, so a storm found yesterday survives the app being closed.
 */
export function asMotion(v: string | null | undefined): Motion {
  return MOTION_STORED.includes(v as Motion) ? (v as Motion) : DEFAULT_MOTION;
}

/**
 * resolveMotion is the gate for the level's name.
 *
 * The phone has no `@media (prefers-reduced-motion: no-preference)` block for a
 * level to sit inside, so this is the only function that hands a level to
 * anything that draws, and while the system asks for less motion it hands out
 * `off` for every level a picker offers. Somebody who set that did not go
 * looking for any of them; they got whichever one the app opened on.
 *
 * The storm passes through (GlimStone 2.1.0). Five taps on an option already
 * chosen are a request, not a default anybody inherited, and treating that
 * request as one would override what the person holding the phone asked for.
 * motionNumbers takes the part it may not have.
 */
export function resolveMotion(chosen: Motion, reduced: boolean): Motion {
  return reduced && chosen !== 'storm' ? 'off' : chosen;
}

/**
 * motionNumbers is what a component draws with, and the one place the table is
 * read through the gate.
 *
 * Under reduced motion the storm keeps its one-shot figures and loses its
 * loop: wanting more movement is not wanting something that never stops, so
 * the wiggle stays still at every level while the system asks for less.
 */
export function motionNumbers(chosen: Motion, reduced: boolean): MotionNumbers {
  const n = MOTION[resolveMotion(chosen, reduced)];
  return reduced ? { ...n, wiggleDeg: 0, wiggleDur: 0 } : n;
}

/**
 * springConfig is the spring a row settles into its new place on, for
 * Animated.spring: the level's layout `damping`, 0.68 at the top visible level
 * and 0.34 at the storm, the ratios the web reaches with
 * `cubic-bezier(.34, 1.56, .64, 1)` and `cubic-bezier(.22, 1.94, .45, 1)`.
 *
 * Returns null at a level whose layout changes take no time, so the caller
 * snaps rather than running a spring with a zero in it.
 */
export function springConfig(m: Motion): { stiffness: number; damping: number; mass: number } | null {
  const n = NATIVE_MOTION[m];
  return n.layout === 0 ? null : springOf(n.damping);
}

/**
 * stormTap counts the gesture that reveals the storm: set the motion to the top
 * level, then tap that same option five more times. It cannot be reached from
 * any other level, so tapping "off" five times out of annoyance opens nothing.
 *
 * The level has to be switchable back off and must not turn into a permanent
 * entry in the picker, so what keeps it visible is the current state: it is
 * offered while it is chosen, and otherwise only for as long as the screen
 * stays open. The caller therefore keeps `found` in the settings screen's own
 * state, never in storage, and counts there for the same reason.
 *
 * Returns the level to switch to, or undefined when the tap was not the fifth.
 */
export function stormTap(state: { taps: number }, tapped: string, current: string): Motion | undefined {
  const top = MOTION_LEVELS[MOTION_LEVELS.length - 1];
  if (tapped !== top || current !== top) {
    state.taps = 0;
    return undefined;
  }
  state.taps += 1;
  if (state.taps < STORM_TAPS) return undefined;
  state.taps = 0;
  return 'storm';
}
