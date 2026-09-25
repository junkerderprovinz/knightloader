/**
 * The motion engine for a phone app, as numbers. It holds no React Native
 * import, so an app copies it as it is and wires the numbers to Animated and
 * LayoutAnimation itself; "On a phone" under "The motion engine" in
 * design-language.md says how. The levels are the web's, and a level is a
 * different number here too, never a different animation.
 */

export type MotionLevel = "off" | "subtle" | "wild" | "storm";

export type NativeMotion = {
  /** A layout change, in ms: a card arriving in a list, a row leaving it. */
  layout: number;
  fade: number;
  toast: number;
  /** Whether layout changes spring rather than ease. */
  spring: boolean;
  /** LayoutAnimation's springDamping for them: below 1 overshoots. */
  damping: number;
  /** How far a card travels up into place as a page arrives, in points. */
  travel: number;
  /** How far it swings in from the side as well, alternating per card. */
  sway: number;
  /** The delay between one card's arrival and the next one's, in ms. */
  stagger: number;
  /** The scale a pressed button or card gives way to. */
  press: number;
  /** The damping ratio of the arrival, the press and the edge: below 1 overshoots. */
  bounce: number;
  /** How far a page runs on past its top or bottom before it swings back. */
  edge: number;
};

export const NATIVE_MOTION: Record<MotionLevel, NativeMotion> = {
  storm: {
    layout: 760, fade: 200, toast: 420, spring: true, damping: 0.34,
    travel: 72, sway: 48, stagger: 85, press: 0.84, bounce: 0.3, edge: 64,
  },
  wild: {
    layout: 420, fade: 140, toast: 300, spring: true, damping: 0.68,
    travel: 44, sway: 28, stagger: 65, press: 0.9, bounce: 0.42, edge: 36,
  },
  subtle: {
    layout: 140, fade: 70, toast: 120, spring: false, damping: 1,
    travel: 10, sway: 0, stagger: 25, press: 0.97, bounce: 1, edge: 0,
  },
  off: {
    layout: 0, fade: 0, toast: 0, spring: false, damping: 1,
    travel: 0, sway: 0, stagger: 0, press: 1, bounce: 1, edge: 0,
  },
};

/** Past this many cards a long page would still be arriving a second later. */
export const ARRIVAL_CAP = 8;

/**
 * A spring for Animated from a damping ratio, which reads the same at any
 * stiffness where React Native's own `damping` does not.
 */
export function springOf(ratio: number): { stiffness: number; damping: number; mass: number } {
  const stiffness = 180;
  return { stiffness, damping: ratio * 2 * Math.sqrt(stiffness), mass: 1 };
}

/** When the card in `place` starts arriving, in ms after the page. */
export function arrivalDelay(place: number, m: NativeMotion): number {
  return Math.min(place, ARRIVAL_CAP) * m.stagger;
}

/** The side a card swings in from: every other card comes from the other one. */
export function arrivalSide(place: number): -1 | 1 {
  return place % 2 === 0 ? -1 : 1;
}

/**
 * How far past an edge a page runs, signed: positive at the top, where the
 * content moves down. A fast fling hits harder than a slow one. `speed` is the
 * scroll event's velocity in points per millisecond.
 */
export function edgeReach(edge: "top" | "bottom", speed: number, m: NativeMotion): number {
  const toward = edge === "top" ? 1 : -1;
  return toward * m.edge * Math.min(1, 0.4 + Math.abs(speed) / 5);
}
