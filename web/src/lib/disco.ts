// Disco walks the rainbow palette. Every animation frame it moves each root
// --rb-N property a little further along the loop in discoLoop.ts, and every
// hued element follows, because hueVars() points it at the root. One clock
// drives the whole walk: a timer stepping the colours and a transition gliding
// them would each keep their own time, and every step would land a little
// early or late, as a jolt in the glide.
//
// The switch is the app's to store; the walk never is, and it never touches
// the rainbow state. The walk has no prefers-reduced-motion gate, because a
// hidden mode somebody found on purpose is not a default anybody inherited.
// The glide itself follows the motion engine: with reduced motion, or at the
// "off" level, disco steps one colour at a time instead.
import { applyRainbow, contrastOn, rainbowState, RAINBOW } from './appearance';
import { buildLoop, colourAt, type Loop } from './discoLoop';

/** The walk covers one palette colour's worth of loop every 2.4 seconds, so a
 *  full turn of eight colours takes 19.2 seconds. Stepping, it moves one
 *  colour on at the same interval. */
export const DISCO_TICK_MS = 2400;

/** Turn-ons needed to unlock, matching STORM_TAPS. */
export const DISCO_UNLOCK_TURN_ONS = 5;

/** How long a run of turn-ons may pause before it counts as a new run, so
 *  somebody comparing rainbow on and off over a minute does not unlock disco. */
export const DISCO_UNLOCK_WINDOW_MS = 3000;

let frame: number | null = null;
let lastFrame: number | undefined;
let loop: Loop = buildLoop(RAINBOW);
// The palette entry --rb-0 starts on, the same as at rest.
let start = 0;
// How much of a full turn the walk has covered, from 0 up to 1.
let travelled = 0;
// The colours last written, so a frame that moves nothing writes nothing.
let painted: string[] = [];
let reducedMotion: MediaQueryList | undefined;

function paint(): void {
  const root = document.documentElement;
  reducedMotion ??= window.matchMedia('(prefers-reduced-motion: reduce)');
  const steps = root.getAttribute('data-motion') === 'off' || reducedMotion.matches;
  const n = loop.palette.length;
  for (let i = 0; i < n; i++) {
    const colour = steps
      ? loop.palette[(i + start + Math.floor(travelled * n)) % n]!
      : colourAt(loop, loop.at[(i + start) % n]! + travelled * loop.at[n]!);
    if (painted[i] === colour) continue;
    painted[i] = colour;
    root.style.setProperty(`--rb-${i}`, colour);
    root.style.setProperty(`--rb-ink-${i}`, contrastOn(colour));
  }
}

function walk(now: number): void {
  const turnMs = DISCO_TICK_MS * loop.palette.length;
  travelled = (travelled + (now - (lastFrame ?? now)) / turnMs) % 1;
  lastFrame = now;
  paint();
  frame = requestAnimationFrame(walk);
}

/** Stops the walk, and does nothing when none is running. The colours stay
 *  where the last frame left them; applyDisco puts the palette back. */
export function stopDisco(): void {
  if (frame !== null) {
    cancelAnimationFrame(frame);
    frame = null;
  }
}

/**
 * Starts or stops the walk to match the switch and stamps `data-disco` on the
 * root. Call it at boot, after the rainbow state is applied, and again
 * whenever the switch or the rainbow state changes. A walk that is already
 * running takes up a new palette and carries on from where it is, so calling
 * it again never starts a second walk or sends the colours back to the start.
 *
 * With rainbow off nothing on screen is hued, so the walk stops and the switch
 * stays on; it resumes when rainbow comes back.
 */
export function applyDisco(on: boolean): void {
  const root = document.documentElement;
  if (on) root.setAttribute('data-disco', 'on');
  else root.removeAttribute('data-disco');

  const live = rainbowState();
  if (!on || !live.on) {
    if (frame !== null) {
      stopDisco();
      // The walk never changed the state, so applying it again writes the
      // resting palette back over the walked one.
      applyRainbow(live);
    }
    return;
  }

  loop = buildLoop(live.palette);
  start = live.rotate ? live.seed : 0;
  painted = [];
  if (frame === null) {
    travelled = 0;
    lastFrame = undefined;
    frame = requestAnimationFrame(walk);
  }
}

/**
 * The unlock gesture: five turn-ons of Rainbow Mode, each within
 * DISCO_UNLOCK_WINDOW_MS of the last. Returns true on the fifth.
 *
 * Counting turn-ons rather than clicks leaves rainbow on, the only state where
 * disco has colours to walk. As with stormTap, the count lives in the caller
 * and is never persisted.
 */
export function discoTap(
  state: { taps: number; last: number },
  turnedOn: boolean,
  clock: { now: number },
): boolean {
  if (!turnedOn) return false;
  const gap = clock.now - state.last;
  state.last = clock.now;
  state.taps = state.taps > 0 && gap <= DISCO_UNLOCK_WINDOW_MS ? state.taps + 1 : 1;
  if (state.taps < DISCO_UNLOCK_TURN_ONS) return false;
  state.taps = 0;
  return true;
}
