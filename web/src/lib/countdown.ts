// The three parts a live countdown needs, in one place.
//
// There were already two identical copies of them - IdleActionBanner's and
// CaptchaModal's - and a third is the one that drifts: the moment two of them
// round differently, the same wait reads as "1:00" in one corner of the app and
// "59s" in another. Moving those two onto this module is deliberately not part
// of this change; the point here is only that the third copy does not get
// written.

import { useEffect, useState } from 'react';

// Go's encoding/json does not drop a zero time.Time - omitempty has no effect on
// a struct - so a deadline nobody has set arrives as "0001-01-01T00:00:00Z"
// rather than as an absent field. Comparing the year is what format.ts already
// does for the same reason: matching the literal string would break the moment
// the server changed its precision.
const GO_ZERO_YEAR = 1;

/**
 * goTimeMs turns a Go timestamp into a moment, or into null for the three ways
 * of having none: absent, unparseable, and the zero time above.
 *
 * Null and never NaN, so a caller tests the deadline instead of testing the
 * arithmetic that came out of it. NaN propagates silently through a subtraction
 * and only shows up on screen.
 */
export function goTimeMs(iso?: string): number | null {
  if (!iso) return null;
  const d = new Date(iso);
  if (Number.isNaN(d.getTime()) || d.getUTCFullYear() <= GO_ZERO_YEAR) return null;
  return d.getTime();
}

/**
 * fmtCountdown is "45s" under a minute and "4:12" above it, lifted verbatim from
 * IdleActionBanner so the app has one countdown shape rather than one per
 * feature.
 *
 * Never localised, and never handed to a translator: it is digits and a colon in
 * every language this app ships, and a string like "4:12" put in front of forty
 * translators can only come back wrong in some of them.
 */
export function fmtCountdown(totalSeconds: number): string {
  const s = Math.max(0, totalSeconds);
  const m = Math.floor(s / 60);
  const rem = s % 60;
  if (m === 0) return `${rem}s`;
  return `${m}:${String(rem).padStart(2, '0')}`;
}

/**
 * useCountdown is the whole seconds left until `deadline`, or null when there is
 * nothing left to count to.
 *
 * ONE interval, and only while a real deadline is on screen: the rule
 * IdleActionBanner and CaptchaModal both already follow, and the reason the
 * hook exists rather than a setInterval per caller.
 *
 * WHERE THIS IS CALLED MATTERS MORE THAN WHAT IT DOES. It re-renders whatever
 * component calls it once a second, and the download list's own post-commit
 * measuring pass (TaskListCard's useLayoutEffect, deliberately written with no
 * dependency array) reads a bounding rect off every drawn row on every commit
 * of the card. Call this in the card, in the page above it, or in a shared
 * clock the card consumes, and the whole visible slice is re-measured once a
 * second for as long as one row is waiting, for ever. It belongs in a leaf that
 * draws a single span and nothing else.
 *
 * graceMs is how long past the deadline this keeps answering zero instead of
 * null. Nothing is wrong with those seconds: the server's own timer fires at
 * the deadline and the requeue reaches this tab over the socket a moment later,
 * so a countdown that vanished the instant it reached zero would blink out
 * before the row it belongs to has changed. Past the grace the answer is null,
 * because a deadline that old is one nothing is going to act on, and a frozen
 * "0:00" beside the words "next in" is a promise the app will never keep.
 */
export function useCountdown(deadline: number | null, graceMs = 0): number | null {
  const [now, setNow] = useState(() => Date.now());
  // The deadline this clock was last read against. Without it, a component that
  // stays mounted while its deadline changes - a row that fails, retries and
  // fails onto a longer wait, which is exactly the row this feature is for -
  // paints one frame of the OLD clock against the NEW deadline, so a fresh
  // 4:12 flashes up as 14:12 before the first tick corrects it. Adjusting state
  // during render is React's own answer to that and re-renders before the
  // browser paints, where an effect would only fix it a frame too late.
  const [armed, setArmed] = useState(deadline);
  if (armed !== deadline) {
    setArmed(deadline);
    setNow(Date.now());
  }

  useEffect(() => {
    if (deadline === null || Date.now() > deadline + graceMs) return;
    const id = setInterval(() => {
      const t = Date.now();
      setNow(t);
      // Stops itself once there is nothing left to count. The effect's own
      // dependencies can notice a new deadline arriving; they cannot notice
      // time passing, so without this a row whose wait is long over goes on
      // waking the tab once a second for the rest of the session.
      if (t > deadline + graceMs) clearInterval(id);
    }, 1000);
    return () => clearInterval(id);
  }, [deadline, graceMs]);

  if (deadline === null || now > deadline + graceMs) return null;
  return Math.max(0, Math.round((deadline - now) / 1000));
}
