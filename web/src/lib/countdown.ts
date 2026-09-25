// What a live countdown needs, in one place, so every countdown in the app
// rounds the same way.

import { useEffect, useState } from 'react';
import { ltr } from './bidi';

// omitempty does not drop a zero time.Time, so an unset deadline arrives as
// year one. Comparing the year survives a change in the server's precision.
const GO_ZERO_YEAR = 1;

/**
 * goTimeMs turns a Go timestamp into epoch milliseconds, or null when it is
 * absent, unparseable or the zero time. Null rather than NaN, which would
 * travel silently through arithmetic.
 */
export function goTimeMs(iso?: string): number | null {
  if (!iso) return null;
  const d = new Date(iso);
  if (Number.isNaN(d.getTime()) || d.getUTCFullYear() <= GO_ZERO_YEAR) return null;
  return d.getTime();
}

/**
 * happened reports whether a Go timestamp names a real moment. Use it instead
 * of `!!task.someTime`: the zero time is a non-empty string, so that test is
 * always true. check-go-timestamps.mjs refuses truthiness tests on Go
 * timestamps.
 */
export function happened(iso?: string): boolean {
  return goTimeMs(iso) !== null;
}

/**
 * fmtCountdown is "45s" under a minute and "4:12" above it. Never translated:
 * digits and a colon read the same in every language.
 */
export function fmtCountdown(totalSeconds: number): string {
  const s = Math.max(0, totalSeconds);
  const m = Math.floor(s / 60);
  const rem = s % 60;
  return ltr(m === 0 ? `${rem}s` : `${m}:${String(rem).padStart(2, '0')}`);
}

/**
 * useCountdown returns the whole seconds left until `deadline`, or null when
 * there is nothing to count to. It runs one interval, only while a deadline
 * is live.
 *
 * It re-renders its caller every second, so call it in a leaf that draws one
 * span. In the download list card, whose layout effect measures every row on
 * every commit, it would re-measure the whole list each second.
 *
 * graceMs keeps answering zero for a while past the deadline, because the
 * server's requeue reaches the tab a moment after its timer fires. After the
 * grace the answer is null rather than a frozen "0:00".
 */
export function useCountdown(deadline: number | null, graceMs = 0): number | null {
  const [now, setNow] = useState(() => Date.now());
  // Reset the clock during render when the deadline changes, or one frame
  // would show the old clock against the new deadline.
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
      // Stop once the grace is over; the dependencies cannot notice time passing.
      if (t > deadline + graceMs) clearInterval(id);
    }, 1000);
    return () => clearInterval(id);
  }, [deadline, graceMs]);

  if (deadline === null || now > deadline + graceMs) return null;
  return Math.max(0, Math.round((deadline - now) / 1000));
}
