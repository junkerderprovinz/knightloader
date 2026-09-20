// The shared speed window: one rolling buffer per instance scope, so the
// Overview curve and the shell meter agree and survive navigation. It is
// seeded once per page load from GET /api/stats/speed, the ring the server
// keeps in memory, so a reload does not start from a flat line.
//
// Only scope '' is seeded. The route is not forwarded to peers, so a peer
// scope would get this box's history; it starts empty and fills live.
//
// While the websocket is down the caller keeps reporting its last value, so an
// outage draws a plateau that did not happen. A tab hidden long enough is
// re-seeded when it becomes visible again; a short blip still shows.

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

// The server's two resolutions. Fixed here rather than read from the seed, so
// an unseeded peer scope has the same step.
const FINE_STEP_S = 1;
const COARSE_STEP_S = 10;
const COARSE_EVERY = COARSE_STEP_S / FINE_STEP_S;

// Live history kept beyond the seed: an hour of each resolution, a few
// kilobytes.
const LIVE_CAP = 360;

// Ticks during which a reported 0 means "not known yet" rather than idle.
// useTasks waits for the websocket snapshot, so `value` is 0 until the socket
// opens, which would draw a notch at the join between seed and live tail. The
// server's last reading stands in meanwhile, and the first non-zero value ends
// the grace. Short, because holding a stale reading longer would mislead more.
const LIVE_GRACE_TICKS = 2;

// How long a hidden tab must have been away before it re-seeds on return.
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
  /** Ticks left in which a reported 0 means "not known yet"; see LIVE_GRACE_TICKS. */
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
      // A real reading shows the stream is live.
      sc.grace = 0;
    }
  }

  sc.live1s.push(v);
  trim(sc.live1s);

  sc.bucket.push(v);
  if (sc.bucket.length >= COARSE_EVERY) {
    // The mean, as the server's coarse ring uses, so seeded and live halves
    // join without a step. The last sample alone would turn bursty traffic
    // into noise.
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
  // Started on the first subscriber and never stopped: restarting it on
  // remounts (StrictMode, visiting Overview) would shift its phase and make the
  // samples uneven. The shell meter is mounted for the page's life anyway.
  sc.timer = setInterval(() => tick(sc), FINE_STEP_S * 1000);
}

async function fetchSeed(): Promise<SpeedHistory> {
  const r = await fetch('/api/stats/speed');
  // An older server answers 404 in plain text, which .json() would report as
  // a SyntaxError.
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
      // Without a seed the window fills from live samples alone; no error is shown.
      sc.seedStarted = false;
      sc.seededAt = Date.now();
    },
  );
}

// A tab returning after being hidden long enough drops its live samples and
// re-seeds: a hidden tab's timers are throttled and its socket may have closed,
// while the server's ring kept recording.
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

  // Not padded: zeros would claim the instance was idle before it booted.
  const all = seeded.concat(live);
  const samples = all.length > points ? all.slice(all.length - points) : all;
  return { samples, seconds: samples.length * step };
}

/**
 * useSpeedWindow returns the live window for one instance scope ('' = this
 * instance). `value` is the caller's current reading in bytes/s; the store
 * owns the clock, so every graph samples on the same tick.
 */
export function useSpeedWindow(
  instance: string,
  value: number,
  points: number,
  scale: SpeedScale,
): SpeedWindow {
  // Memoised, or useSyncExternalStore would resubscribe on every render, which
  // happens on every websocket frame.
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

  // A version counter, since a fresh window object per call would re-render
  // forever.
  const version = useSyncExternalStore(
    subscribe,
    () => scopeFor(instance).version,
    () => 0,
  );

  // In an effect, not during render, which must not write to a store.
  useEffect(() => {
    scopeFor(instance).value = value;
  }, [instance, value]);

  return useMemo(() => windowFor(instance, points, scale), [instance, points, scale, version]);
}
