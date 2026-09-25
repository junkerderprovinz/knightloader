// Disco walks the rainbow palette. Every animation frame it moves each root
// --rb-N property a little further along the loop in discoLoop.js, and every
// hued element follows, because hueVars() points it at the root. One clock
// drives the whole walk: a timer stepping the colours and a transition gliding
// them would each keep their own time, and every step would land a little
// early or late, as a jolt in the glide.
//
// Ported from glimstone's reference/disco.ts with the types stripped; the
// module state carries a disco prefix, since the pages share one global scope.
// The switch is stored in chrome.storage (rainbowDisco); the walk never is,
// and it never touches the rainbow state. The mode has no reduced-motion gate,
// because somebody found it on purpose, but the glide does: with reduced
// motion it steps one colour at a time instead. The extension has no motion
// level of its own, so the system setting is the only one it reads.

/** The walk covers one palette colour's worth of loop every 2.4 seconds, so a
 *  full turn of eight colours takes 19.2 seconds. Stepping, it moves one
 *  colour on at the same interval. */
const DISCO_TICK_MS = 2400;

/** Turn-ons of rainbow mode that unlock it. */
const DISCO_UNLOCK_TURN_ONS = 5;

/** The longest pause between two turn-ons of one run. Without it, somebody
 *  comparing the page with and without the rainbow over a minute unlocks a
 *  mode they never went looking for. */
const DISCO_UNLOCK_WINDOW_MS = 3000;

let discoFrame = null;
let discoLastFrame;
let discoLoop = buildLoop(RAINBOW);
// The palette entry --rb-0 starts on, the same as at rest.
let discoStart = 0;
// How much of a full turn the walk has covered, from 0 up to 1.
let discoTravelled = 0;
// The colours last written, so a frame that moves nothing writes nothing.
let discoPainted = [];
let discoReducedMotion;

function discoPaint() {
  const root = document.documentElement;
  discoReducedMotion ??= window.matchMedia('(prefers-reduced-motion: reduce)');
  const steps = discoReducedMotion.matches;
  const n = discoLoop.palette.length;
  for (let i = 0; i < n; i++) {
    const colour = steps
      ? discoLoop.palette[(i + discoStart + Math.floor(discoTravelled * n)) % n]
      : colourAt(discoLoop, discoLoop.at[(i + discoStart) % n] + discoTravelled * discoLoop.at[n]);
    if (discoPainted[i] === colour) continue;
    discoPainted[i] = colour;
    root.style.setProperty(`--rb-${i}`, colour);
    root.style.setProperty(`--rb-ink-${i}`, contrastOn(colour));
  }
}

function discoWalk(now) {
  const turnMs = DISCO_TICK_MS * discoLoop.palette.length;
  discoTravelled = (discoTravelled + (now - (discoLastFrame ?? now)) / turnMs) % 1;
  discoLastFrame = now;
  discoPaint();
  discoFrame = requestAnimationFrame(discoWalk);
}

/** Stops the walk, and does nothing when none is running. The colours stay
 *  where the last frame left them; applyDisco puts the palette back. */
function stopDisco() {
  if (discoFrame !== null) {
    cancelAnimationFrame(discoFrame);
    discoFrame = null;
  }
}

/**
 * Starts or stops the walk to match the switch and stamps `data-disco` on the
 * root. Call it after the rainbow state is applied, and again whenever the
 * switch or the rainbow state changes. A walk that is already running takes up
 * a new palette and carries on from where it is.
 *
 * With rainbow off nothing on screen is hued, so the walk stops and the switch
 * stays on; it resumes when rainbow comes back.
 */
function applyDisco(on) {
  const root = document.documentElement;
  if (on) root.setAttribute('data-disco', 'on');
  else root.removeAttribute('data-disco');

  const live = rainbowNow;
  if (!on || !live.on) {
    if (discoFrame !== null) {
      stopDisco();
      // The walk never changed the state, so applying it again writes the
      // resting palette back over the walked one.
      applyRainbow(live);
    }
    return;
  }

  discoLoop = buildLoop(live.palette);
  discoStart = live.rotate ? live.seed : 0;
  discoPainted = [];
  if (discoFrame === null) {
    discoTravelled = 0;
    discoLastFrame = undefined;
    discoFrame = requestAnimationFrame(discoWalk);
  }
}

/**
 * discoTap counts the unlock gesture: five turn-ons of rainbow mode, each
 * within DISCO_UNLOCK_WINDOW_MS of the last, and true on the fifth. Turn-ons
 * rather than clicks, so the gesture ends with the rainbow on, the one state
 * in which the reward can be seen. The count lives with the caller and never
 * in storage, or finding the mode once would leave its switch on the page for
 * good.
 */
function discoTap(state, turnedOn, now) {
  if (!turnedOn) return false;
  const gap = now - state.last;
  state.last = now;
  state.taps = state.taps > 0 && gap <= DISCO_UNLOCK_WINDOW_MS ? state.taps + 1 : 1;
  if (state.taps < DISCO_UNLOCK_TURN_ONS) return false;
  state.taps = 0;
  return true;
}
