// How much this app moves, as numbers rather than as animations.
//
// GlimStone's motion engine is an axis the user owns: off, subtle, wild, and
// one more that no picker lists. The language's rule for it is short and is the
// only reason a fourth level costs almost nothing - A LEVEL IS A DIFFERENT
// NUMBER, NEVER A DIFFERENT ANIMATION. So this file is a table of figures and
// four small functions, and every component that moves reads the table instead
// of typing a duration of its own.
//
// THE WEB HALF THIS IS TAKEN FROM, and where it could not be taken. On the web
// the whole engine is CSS custom properties keyed off `data-motion` on <html>,
// resolved inside `@media (prefers-reduced-motion: no-preference)` - so a
// browser whose owner asked the system for less motion never evaluates a
// `data-motion` selector at all, and the accessibility signal wins by WHERE THE
// BLOCK SITS rather than by a check in front of it. React Native has neither
// custom properties nor media queries, so that safety property has to be rebuilt
// out of different parts:
//
//   - the signal is a function call, AccessibilityInfo.isReduceMotionEnabled(),
//     read in exactly one place (MotionContext.tsx) and nowhere else;
//   - every level passes through resolveMotion() below, which answers `off` for
//     any level at all while that signal is on.
//
// That is the same guarantee reached the other way round: on the web nothing
// downstream can see a level because the block is never entered, here because
// the only function that hands a level out refuses to hand out anything else.
// check-hidden-motion-level.mjs calls resolveMotion with every level to prove
// it, and fails if a second file starts reading the signal for itself.
//
// This file is deliberately free of React and of react-native, the same split
// theme/appearance.ts already uses: the constants and the pure functions travel,
// the half that has to subscribe to the platform lives in the context beside it.
// It is also what lets the check script simply import and RUN it.

/**
 * The levels, quietest first.
 *
 * `storm` is deliberately LAST and deliberately not in MOTION_LEVELS below. It
 * is a real level with real numbers - see the MOTION table - and it is not
 * something a picker offers.
 */
export type Motion = 'off' | 'subtle' | 'wild' | 'storm';

/** What a picker shows. The storm is not in here; see stormTap below. */
export const MOTION_LEVELS: Motion[] = ['off', 'subtle', 'wild'];

/**
 * What a STORED value may say, which is a different question and a different
 * list. A persisted storm is accepted at launch even though no picker offers
 * it, or the gesture would have produced a setting that silently forgets itself
 * the next time the app is opened. An axis with a hidden level is exactly where
 * treating validation and population as one question shows up.
 */
export const MOTION_STORED: Motion[] = [...MOTION_LEVELS, 'storm'];

/**
 * The default is the top VISIBLE level, not the quietest.
 *
 * This axis is additive polish somebody dials DOWN, not a compatibility
 * fallback they have to opt INTO - and the signal that genuinely needs a
 * default is the system's own reduced-motion setting, which resolveMotion reads
 * unconditionally and which wins over every value here.
 */
export const DEFAULT_MOTION: Motion = 'wild';

/** How many taps on the level already chosen open the one below the floor. */
export const STORM_TAPS = 5;

/**
 * Everything this app animates, as one figure per level.
 *
 * Every level carries every figure. A level that leaves one out does not get a
 * sensible default, it INHERITS the top level's own value and arrives as a
 * slightly slower version of it - which is how a fourth intensity gets built and
 * then reported as "the egg does nothing". The check script compares the key
 * sets rather than trusting this comment.
 */
export interface MotionNumbers {
  /** A control refusing an action: how long the whole gesture takes, in ms. */
  shakeDur: number;
  /** How far that refusal swings, in points, at its widest. Zero at `off`. */
  shakeTravel: number;
  /**
   * What the refusal fades to when it cannot travel.
   *
   * An invisible shake carries no "rejected" signal at all, so `off` swaps the
   * movement for a brief dip in opacity rather than for nothing - the same
   * substitution the language prescribes under system-level reduced motion.
   * 1 means "do not fade, the travel says it".
   */
  shakeFadeTo: number;
  /** The "this list is editable now" wiggle: half its swing, in degrees. */
  wiggleDeg: number;
  /** One full wiggle cycle, in ms. Zero means the loop never starts. */
  wiggleDur: number;
  /** The scale a row takes while it is in the hand. */
  liftScale: number;
  /**
   * The damping RATIO of the spring a lifted row settles back on.
   *
   * This is the figure GlimStone 1.17.0 writes down for the phone BY HAND:
   * 0.68 at the top visible level, 0.34 at the storm. 1 is critically damped
   * and does not overshoot at all; below 1 swings past and comes back, and the
   * lower it goes the longer it keeps doing that. The web reaches the same two
   * shapes with `cubic-bezier(.34, 1.56, .64, 1)` and `cubic-bezier(.22, 1.94,
   * .45, 1)`, which is the whole reason the pair is named per surface: the two
   * OVERSHOOT ALIKE rather than merely both overshooting.
   *
   * A ratio and not React Native's own `damping`, because the ratio is the
   * number that means the same thing on both surfaces - see springConfig for
   * the conversion, which is where a stiffness and a mass get chosen.
   */
  springDamping: number;
  /**
   * Whether that settle is animated at all: 1 yes, 0 snap.
   *
   * `off` is "the manual equivalent of what system-level reduced motion already
   * does", so it snaps. It is a figure in the table rather than an `if` at the
   * call site for the same reason every other one is - the moment a level needs
   * an exception list, it has stopped being a set of numbers.
   */
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
  // asked for less movement, not for a faster bounce - so the swing halves and
  // the spring does not overshoot at all.
  subtle: {
    shakeDur: 220,
    shakeTravel: 2,
    shakeFadeTo: 1,
    wiggleDeg: 0.35,
    // The PERIOD is deliberately left where the top level has it. An ambient
    // loop is not calmed by running it faster, only by moving less - the same
    // arrangement the web's own subtle block makes for this one figure.
    wiggleDur: 320,
    liftScale: 1.015,
    springDamping: 1,
    settleScale: 1,
  },
  // The top level a picker offers, and the app's own shipped numbers: the
  // 360ms/4pt shake the whole family draws, the 0.7 degree wiggle the web's
  // token names, the 1.03 lift this list has always used.
  wild: {
    shakeDur: 360,
    shakeTravel: 4,
    shakeFadeTo: 1,
    wiggleDeg: 0.7,
    // 320 and not the 360 this loop shipped with. The web's own
    // --motion-wiggle-dur is .32s, and one gesture having two shapes across one
    // product is the thing these numbers are written down to prevent - the same
    // correction the refusal shake already went through here when it turned out
    // to be running five 55ms steps against the house's 360ms.
    wiggleDur: 320,
    liftScale: 1.03,
    springDamping: 0.68,
    settleScale: 1,
  },
  // THE HIDDEN FOURTH (GlimStone 1.17.0). Same gestures, same table, bigger
  // figures: a spring that swings further and takes longer to come to rest.
  //
  // The multipliers are the web's own storm ladder rather than invented here -
  // shake 1.8x the travel and 520ms, wiggle 1.8x the swing - and the one figure
  // the language names for the phone directly, springDamping 0.34, is copied
  // rather than derived.
  storm: {
    shakeDur: 520,
    shakeTravel: 7.2,
    shakeFadeTo: 1,
    wiggleDeg: 1.3,
    // The one period that goes DOWN. An ambient loop gets livelier by running
    // faster, which is the same reasoning that leaves it alone at subtle, where
    // moving less is the only way to calm it. 320 against the web's own
    // pulse ratio of 1.4s to 2s.
    wiggleDur: 224,
    liftScale: 1.054,
    springDamping: 0.34,
    settleScale: 1,
  },
};

/**
 * asMotion turns a stored string back into a level.
 *
 * Checked against MOTION_STORED and never against MOTION_LEVELS: a storm
 * somebody found yesterday has to survive the app being closed, or the gesture
 * produced a setting that quietly forgets itself.
 */
export function asMotion(v: string | null | undefined): Motion {
  return MOTION_STORED.includes(v as Motion) ? (v as Motion) : DEFAULT_MOTION;
}

/**
 * resolveMotion is the gate, and it is the whole of it.
 *
 * The phone has no `@media (prefers-reduced-motion: no-preference)` block for a
 * level to sit inside, so the guarantee that block gives on the web is rebuilt
 * here: this is the only function that hands a level to anything that draws,
 * and while the system asks for less motion it hands out `off` for every level
 * there is - the hidden one included. A secret "more animation" switch is the
 * one way this axis could genuinely do harm, and this line is where that is
 * closed.
 */
export function resolveMotion(chosen: Motion, reduced: boolean): Motion {
  return reduced ? 'off' : chosen;
}

/**
 * springConfig turns the level's damping RATIO into what Animated.spring wants.
 *
 * React Native's physics config takes an absolute damping coefficient, which
 * means nothing on its own - the same number is bouncy against a stiff spring
 * and dead against a soft one. The ratio is the figure that travels, so the
 * stiffness and the mass are chosen here, once, and the coefficient is computed
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
 * The gesture that reveals the storm, and the rule it carries.
 *
 * SET THE MOTION TO THE TOP LEVEL, THEN TAP THAT SAME OPTION FIVE MORE TIMES.
 * It is the gesture of somebody pressing a button that is already pressed
 * because they wanted more of it, which is exactly who this level is for. It
 * cannot be reached from any other level on purpose: tapping "off" five times
 * means somebody is annoyed, not curious, and a secret that opens under
 * annoyance is a bug report waiting to be filed.
 *
 * THE RULE, and it is the part worth copying rather than the numbers: AN EASTER
 * EGG THAT CHANGES BEHAVIOUR MUST BE SWITCHABLE BACK OFF, AND MUST NOT QUIETLY
 * BECOME A PERMANENT ENTRY IN A SETTINGS LIST. The web's first build of this
 * stored a "found it" flag, so a single gesture put a fourth option in the
 * picker for ever - which turns a secret into a setting somebody has to explain
 * to themselves months later with no memory of how it got there. Reported as
 * exactly that (jdp, 13.09.2026: "sturm soll wieder verschwinden wenn man zb
 * sanft einstellt und die einstellungen verlässt").
 *
 * So what keeps it visible is the plain truth about the current state:
 *
 *   - It is offered while it is CHOSEN, because a picker that hid the value it
 *     is currently showing would be lying about the interface.
 *   - Otherwise it is offered only for as long as the screen stays open. Choose
 *     something else and leave, and it is gone until the gesture is made again.
 *
 * The caller owns the screen and therefore owns how long "open" means: keep
 * `found` in the settings screen's own state, never in storage. Counting lives
 * in the caller for the same reason.
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
