// How much this app moves, as numbers rather than as animations. A level is a
// different number, never a different animation, so this file is a table of
// figures and four small functions, and every component that moves reads the
// table instead of typing a duration of its own.
//
// On the web the engine is CSS custom properties resolved inside
// `@media (prefers-reduced-motion: no-preference)`, so the accessibility signal
// wins by where the block sits. React Native has neither, so the same property
// is rebuilt from two parts: the signal is read in one place
// (MotionContext.tsx), and every level passes through resolveMotion below,
// which answers `off` while the signal is on. check-hidden-motion-level.mjs
// proves both.
//
// Free of React and of react-native, the same split theme/appearance.ts uses,
// which also lets the check script import and run this.

/**
 * The levels, quietest first. `storm` is a real level with real numbers, see
 * the MOTION table, that no picker offers.
 */
export type Motion = 'off' | 'subtle' | 'wild' | 'storm';

/** What a picker shows. The storm is not in here; see stormTap below. */
export const MOTION_LEVELS: Motion[] = ['off', 'subtle', 'wild'];

/**
 * What a stored value may say, which is a different list. A persisted storm is
 * accepted at launch although no picker offers it, or the gesture would produce
 * a setting that forgets itself the next time the app is opened.
 */
export const MOTION_STORED: Motion[] = [...MOTION_LEVELS, 'storm'];

/**
 * The default is the top visible level, not the quietest: this axis is polish
 * somebody dials down, not a compatibility fallback they opt into. The system's
 * own reduced-motion setting wins over it either way, through resolveMotion.
 */
export const DEFAULT_MOTION: Motion = 'wild';

/** How many taps on the level already chosen open the one below the floor. */
export const STORM_TAPS = 5;

/**
 * Everything this app animates, as one figure per level.
 *
 * Every level carries every figure. A level that leaves one out inherits the
 * top level's value and arrives as a slightly slower version of it, which is
 * how a fourth intensity ends up reported as doing nothing. The check script
 * compares the key sets.
 */
export interface MotionNumbers {
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
  /**
   * The damping ratio of the spring a lifted row settles back on: 0.68 at the
   * top visible level and 0.34 at the storm, as GlimStone 1.17.0 writes them
   * down for the phone. The web reaches the same two shapes with
   * `cubic-bezier(.34, 1.56, .64, 1)` and `cubic-bezier(.22, 1.94, .45, 1)`.
   *
   * A ratio rather than React Native's own `damping`, which means different
   * things against different stiffnesses; springConfig converts it.
   */
  springDamping: number;
  /** Whether that settle is animated at all: 1 yes, 0 snap. A figure in the
   *  table rather than a check at the call site, because a level that needs an
   *  exception list has stopped being a set of numbers. */
  settleScale: number;
}

export const MOTION: Record<Motion, MotionNumbers> = {
  // The manual equivalent of system-level reduced motion, not a second recipe
  // tuned separately: the same numbers that signal already produces.
  off: {
    shakeDur: 220,
    shakeTravel: 0,
    shakeFadeTo: 0.45,
    wiggleDeg: 0,
    wiggleDur: 0,
    liftScale: 1,
    springDamping: 1,
    settleScale: 0,
  },
  // Smaller numbers, the same gestures. Somebody who asked for less movement
  // asked for less movement, not for a faster bounce, so the swing halves and
  // the spring does not overshoot at all.
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
    springDamping: 1,
    settleScale: 1,
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
    springDamping: 0.68,
    settleScale: 1,
  },
  // The hidden fourth (GlimStone 1.17.0). Same gestures, same table, bigger
  // figures: a spring that swings further and takes longer to come to rest.
  //
  // The multipliers are the web's own storm ladder rather than invented here,
  // shake 1.8x the travel and 520ms, wiggle 1.8x the swing, and springDamping
  // 0.34 is the figure the language names for the phone.
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
    springDamping: 0.34,
    settleScale: 1,
  },
};

/**
 * asMotion turns a stored string back into a level. Checked against
 * MOTION_STORED, so a storm found yesterday survives the app being closed.
 */
export function asMotion(v: string | null | undefined): Motion {
  return MOTION_STORED.includes(v as Motion) ? (v as Motion) : DEFAULT_MOTION;
}

/**
 * resolveMotion is the gate, and it is the whole of it.
 *
 * The phone has no `@media (prefers-reduced-motion: no-preference)` block for a
 * level to sit inside, so this is the only function that hands a level to
 * anything that draws, and while the system asks for less motion it hands out
 * `off` for every level there is, the hidden one included. A secret "more
 * animation" switch is the one way this axis could do harm.
 */
export function resolveMotion(chosen: Motion, reduced: boolean): Motion {
  return reduced ? 'off' : chosen;
}

/**
 * springConfig turns the level's damping ratio into what Animated.spring wants.
 *
 * React Native's physics config takes an absolute damping coefficient, which
 * means nothing on its own: the same number is bouncy against a stiff spring
 * and dead against a soft one. The ratio is the figure that travels, so the
 * stiffness and the mass are chosen here, once, and the coefficient computed
 * from them: damping = ratio * 2 * sqrt(stiffness * mass).
 *
 * Returns null at a level that does not animate the settle at all, so the
 * caller snaps rather than running a spring with a zero in it.
 */
export function springConfig(m: Motion): { stiffness: number; damping: number; mass: number } | null {
  const n = MOTION[m];
  if (n.settleScale === 0) return null;
  const stiffness = 180;
  const mass = 1;
  return { stiffness, mass, damping: n.springDamping * 2 * Math.sqrt(stiffness * mass) };
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
