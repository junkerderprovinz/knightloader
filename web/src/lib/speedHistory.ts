// The shared speed window: one rolling buffer per instance scope, seeded once
// per page load from what the server itself has been recording.
//
// WHAT THIS REPLACES. SpeedGraph.tsx used to hold its own window in component
// state, created as `Array(points).fill(0)`. Two consequences, and the second
// one is the reason this file exists at all:
//
//   - the hero curve on Overview and the meter in the shell bar each kept a
//     private buffer of the same reading, so they could not agree and one of
//     them emptied itself whenever somebody navigated away from Overview;
//   - every reload started BOTH of them at a flat line that then took a minute
//     to fill in, even though the server had been watching the whole time.
//
// So the window is hoisted out of both components (which is what
// SpeedGraph.tsx's own comment said would be needed) and seeded from
// GET /api/stats/speed, which serves the ring the instance keeps in memory
// (internal/app/app_speedhistory.go).
//
// ONLY SCOPE '' IS EVER SEEDED. The shell meter reads whichever instance the
// shell is scoped to, and that can be a peer. /api/stats/speed is on neither
// forwarding allowlist - not internal/api/routes_federation.go's and not
// routes_relay.go's - so asking for it while scoped to a peer would hand back
// THIS box's history under a figure describing somebody else's. A peer scope
// therefore gets a live-only window that starts empty, which is exactly what it
// had before, and the graph says so on its own abscissa rather than pretending
// otherwise.
//
// WHAT IT DOES NOT DO. The websocket behind `value` reconnects after 1500 ms
// (api.ts), and while it is down the caller keeps reporting whatever it last
// saw. This store keeps sampling that, so a long outage draws a plateau that
// never happened - a fiction the server's own ring does not share. That is
// pre-existing and it is not fixed here; what IS handled is the case that
// produces the longest outages in practice, a tab that was hidden or a phone
// that was asleep: coming back to a visible document after long enough throws
// the live half away and re-seeds from the server. A short blip still draws its
// plateau.

import { useCallback, useEffect, useMemo, useSyncExternalStore } from 'react';

/** What GET /api/stats/speed answers - internal/app/app_speedhistory.go's SpeedHistory. */
export interface SpeedHistory {
  recent: number[];
  recentStep: number;
  recentCap: number;
  hour: number[];
  hourStep: number;
  hourCap: number;
  recordingSince?: string;
  sampledAt: string;
}

/** The window a graph draws: at most `points` samples, and the span they cover. */
export interface SpeedWindow {
  samples: number[];
  /** samples.length * step, in whole seconds. What the abscissa prints. */
  seconds: number;
}

export type SpeedScale = 'minute' | 'hour';

// The two resolutions, matching the server's rings exactly. They are written
// here as well rather than read out of the seed, because a scope that is never
// seeded (a peer) still has to know what its own live buffer means - and a
// window whose step depended on whether a fetch had landed yet would print two
// different abscissae for the same picture.
const FINE_STEP_S = 1;
const COARSE_STEP_S = 10;
const COARSE_EVERY = COARSE_STEP_S / FINE_STEP_S;

// How much live history one scope keeps beyond its seed. 360 of each is an hour
// of fine samples and an hour of coarse ones: more than any window draws, and
// still only a few kilobytes, so a tab left open all afternoon neither grows
// without bound nor runs out of the hour view.
const LIVE_CAP = 360;

// How many ticks a reported 0 is treated as "the caller does not know yet"
// rather than as "the instance is idle".
//
// useTasks fires no initial GET on purpose (useTasks.ts:18-29): the websocket's
// own snapshot is the first data it ever has, so `value` is 0 for as long as the
// socket takes to open. Sampled straight into a window that was just seeded from
// a busy hour, that draws a single notch to the floor exactly at the join
// between the seeded history and the live tail - the one place a reader is
// looking to see whether the two agree. During the grace the server's own last
// reading stands in instead, and the first non-zero the caller reports ends the
// grace immediately.
//
// Two ticks and not more: this is a mitigation, not a proof. The exact signal
// ("the snapshot has arrived") is known only inside useTasks, and reaching for
// it would mean changing a hook every list page shares. Holding a stale reading
// for longer than a couple of seconds would be the worse lie of the two.
const LIVE_GRACE_TICKS = 2;

// How long a scope has to have been out of sight before coming back re-seeds it
// rather than trusting the live buffer. Half a minute is well past a tab switch
// and well short of a lunch break.
const RESEED_AFTER_MS = 30_000;

interface Scope {
  /** Oldest first, as served. Empty for a scope that is not seeded. */
  seedRecent: number[];
  seedHour: number[];
  /** Everything sampled in this page's own lifetime, oldest first. */
  live1s: number[];
  live10s: number[];
  /** The fine samples since the last coarse entry closed. */
  bucket: number[];
  /** What the caller last reported, read by the tick. */
  value: number;
  /** Ticks left in which a reported 0 means "not known yet" - see LIVE_GRACE_TICKS. */
  grace: number;
  /** The server's own last reading, which stands in during the grace. */
  carry: number;
  /** The seed fetch has been started for this scope. StrictMode's double mount must not fetch twice. */
  seedStarted: boolean;
  seededAt: number;
  version: number;
  listeners: Set<() => void>;
  timer: ReturnType<typeof setInterval> | undefined;
}

const scopes = new Map<string, Scope>();

function scopeFor(instance: string): Scope {
  let sc = scopes.get(instance);
  if (!sc) {
    sc = {
      seedRecent: [],
      seedHour: [],
      live1s: [],
      live10s: [],
      bucket: [],
      value: 0,
      grace: LIVE_GRACE_TICKS,
      carry: 0,
      seedStarted: false,
      seededAt: 0,
      version: 0,
      listeners: new Set(),
      timer: undefined,
    };
    scopes.set(instance, sc);
  }
  return sc;
}

function emit(sc: Scope): void {
  sc.version++;
  for (const l of sc.listeners) l();
}

/** Keeps the newest `cap` entries. Mutates in place: the array is never handed out. */
function trim(a: number[]): void {
  if (a.length > LIVE_CAP) a.splice(0, a.length - LIVE_CAP);
}

function tick(sc: Scope): void {
  let v = sc.value;
  if (sc.grace > 0) {
    if (v === 0) {
      // Not known yet rather than idle: the server's own last reading stands in.
      v = sc.carry;
      sc.grace--;
    } else {
      // A real reading proves the caller's stream is live, so the grace is over
      // whether or not it had run out on its own.
      sc.grace = 0;
    }
  }

  sc.live1s.push(v);
  trim(sc.live1s);

  sc.bucket.push(v);
  if (sc.bucket.length >= COARSE_EVERY) {
    // The MEAN of the ten, which is the same rule the server uses for its own
    // coarse ring. It has to be: the seeded half and the live half of the hour
    // curve are drawn as one line, and two different summaries of the same ten
    // seconds would put a step at the join that nothing explains. Taking the
    // last of the ten instead would turn bursty traffic into noise - a transfer
    // alternating 40 MB/s and nothing draws as a solid 40 or a solid 0
    // depending on phase alone.
    let sum = 0;
    for (const s of sc.bucket) sum += s;
    sc.live10s.push(Math.round(sum / sc.bucket.length));
    trim(sc.live10s);
    sc.bucket = [];
  }

  emit(sc);
}

function start(sc: Scope): void {
  if (sc.timer !== undefined) return;
  // Started on the first subscriber and NEVER stopped. React 19's StrictMode
  // mounts, unmounts and remounts everything in development, and the shell
  // meter is mounted for the life of the page anyway - so a timer torn down on
  // the last unsubscribe would restart its one-second phase every time somebody
  // visited Overview, which is precisely the "the interval is a lie" failure
  // SpeedGraph.tsx's old comment warned about. Stopping it would save one
  // wake-up a second and cost the evenness of the whole curve.
  sc.timer = setInterval(() => tick(sc), FINE_STEP_S * 1000);
}

async function fetchSeed(): Promise<SpeedHistory> {
  const r = await fetch('/api/stats/speed');
  // Checked BEFORE .json(). A route the server does not have answers 404 with
  // plain text (internal/api/routes.go's own fallback), and .json() on that
  // throws a SyntaxError about an unexpected token - which is a confusing way
  // to say "this build has no speed record yet".
  if (!r.ok) throw new Error(`GET /api/stats/speed: ${r.status}`);
  return (await r.json()) as SpeedHistory;
}

function seed(sc: Scope): void {
  if (sc.seedStarted) return;
  sc.seedStarted = true;
  void fetchSeed().then(
    (h) => {
      sc.seedRecent = Array.isArray(h.recent) ? h.recent : [];
      sc.seedHour = Array.isArray(h.hour) ? h.hour : [];
      sc.carry = sc.seedRecent.length > 0 ? sc.seedRecent[sc.seedRecent.length - 1] : 0;
      sc.grace = LIVE_GRACE_TICKS;
      sc.seededAt = Date.now();
      emit(sc);
    },
    () => {
      // No seed, and nothing else: the window then fills from live samples
      // alone, which is exactly what every version before this one did. A
      // failed seed must not take the live curve down with it, and it must not
      // put an error on a page whose subject is a download queue.
      sc.seedStarted = false;
      sc.seededAt = Date.now();
    },
  );
}

// Coming back to a tab that was hidden long enough throws the live half away
// and asks the server again. While a document is hidden the browser throttles
// or suspends setInterval and the websocket may have been closed under it, so
// the live buffer of a tab that was away for an hour is a plateau of whatever
// it last saw - and the server has the truth of that hour sitting in its ring.
// Registered once, at module level, the same way uistate.ts registers its own
// flush-on-hide.
if (typeof document !== 'undefined') {
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState !== 'visible') return;
    const sc = scopes.get('');
    if (!sc || sc.seededAt === 0 || Date.now() - sc.seededAt < RESEED_AFTER_MS) return;
    sc.live1s = [];
    sc.live10s = [];
    sc.bucket = [];
    sc.seedStarted = false;
    seed(sc);
  });
}

function windowFor(instance: string, points: number, scale: SpeedScale): SpeedWindow {
  const sc = scopeFor(instance);
  const step = scale === 'hour' ? COARSE_STEP_S : FINE_STEP_S;
  const seeded = scale === 'hour' ? sc.seedHour : sc.seedRecent;
  const live = scale === 'hour' ? sc.live10s : sc.live1s;

  // Neither half is padded. A window holding forty seconds reports forty
  // seconds and the abscissa says `-40s`; padding to `points` with zeros would
  // claim this instance was idle for the minute before it booted, which is the
  // same lie the server refuses to tell by not padding its own rings.
  const all = seeded.concat(live);
  const samples = all.length > points ? all.slice(all.length - points) : all;
  return { samples, seconds: samples.length * step };
}

/**
 * The live window for one instance scope, seeded once per page load from
 * GET /api/stats/speed and kept ticking afterwards. Module-level, so navigating
 * off the Overview page and back does not empty it.
 *
 * `instance` is the scope the caller's own task stream is on ('' = this
 * instance). Only '' is ever seeded: /api/stats/speed is on neither forwarding
 * allowlist, so a peer-scoped caller would otherwise be handed THIS box's
 * history under a number describing somebody else's.
 *
 * `value` is the caller's own live reading in bytes/s, reported rather than
 * sampled: the store owns the clock, so the two readings on screen tick
 * together instead of each on the phase of its own mount.
 */
export function useSpeedWindow(
  instance: string,
  value: number,
  points: number,
  scale: SpeedScale,
): SpeedWindow {
  // The subscription is memoised on `instance` alone. Left inline it would be a
  // new function on every render, and useSyncExternalStore would unsubscribe
  // and resubscribe on each one - many times a second under load, since the
  // caller re-renders on every websocket task frame.
  const subscribe = useCallback(
    (onChange: () => void) => {
      const sc = scopeFor(instance);
      sc.listeners.add(onChange);
      start(sc);
      if (instance === '') seed(sc);
      return () => {
        sc.listeners.delete(onChange);
      };
    },
    [instance],
  );

  // A version counter and not the window itself. useSyncExternalStore compares
  // snapshots by reference, and a getter that built a fresh { samples, seconds }
  // each time it was asked would re-render for ever.
  const version = useSyncExternalStore(
    subscribe,
    () => scopeFor(instance).version,
    () => 0,
  );

  // Reported in an effect rather than during render: writing to a module-level
  // store while rendering is the thing StrictMode's double render exists to
  // catch, and one tick's worth of lateness is invisible against a one second
  // clock.
  useEffect(() => {
    scopeFor(instance).value = value;
  }, [instance, value]);

  return useMemo(() => windowFor(instance, points, scale), [instance, points, scale, version]);
}
