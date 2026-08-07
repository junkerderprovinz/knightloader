// Where the interface keeps what it has to remember between reloads: column
// widths and order, which packages are folded shut, which settings page was open
// last.
//
// One opaque JSON object per bucket, held here and written through to
// GET/PUT /api/uistate. The rules the rest of this file exists to keep:
//
//   - the local copy is the truth. The server is asked once, at the start; after
//     that nothing ever reads back over what the user is doing. A layout that
//     reverts while somebody is dragging a column edge is worse than one that
//     does not persist at all.
//   - a failed write must not lose the value. Column widths are cheap to lose
//     once and infuriating to lose repeatedly, so a rejected PUT keeps the local
//     copy, keeps the whole document pending and retries — it never rolls back
//     and never gives up quietly.
//   - writes are debounced and coalesced. Dragging a column edge fires a value
//     per animation frame; one request per frame would put a database write on
//     the download disk sixty times a second.

import { useCallback, useEffect, useState } from 'react';

/** The stored document: whatever the interface put in it, by field name. */
export type UIState = Record<string, unknown>;

/**
 * The shared bucket. Two browsers using it is deliberate — a single-user
 * instance wants its layout to follow it from one machine to the next — and a
 * client that wants its own passes a key of its own.
 */
export const DEFAULT_BUCKET = 'default';

// Long enough to swallow a drag, short enough that closing the tab straight
// after a change usually still catches it (and pagehide catches the rest).
const FLUSH_DELAY_MS = 600;

const RETRY_BASE_MS = 2_000;
const RETRY_MAX_MS = 30_000;

// Mirrors store.MaxUIStateBytes. Checked here as well as there so a document that
// can never be accepted stops being retried forever, and says so once.
const MAX_BYTES = 256 << 10;

// keepalive requests are capped at 64 KiB by the fetch specification, so a large
// document falls back to an ordinary request on the way out — which the browser
// may well cancel. Nothing better exists: the route is a PUT, and sendBeacon
// only posts.
const KEEPALIVE_MAX_BYTES = 64 << 10;

interface Bucket {
  local: UIState;
  /** Fields written in this session. They win over whatever the load brings back. */
  written: Set<string>;
  read?: Promise<UIState>;
  timer?: ReturnType<typeof setTimeout>;
  inFlight: boolean;
  /** Something changed since the last request went out. */
  pending: boolean;
  attempt: number;
  /** The document is over the cap; retrying it cannot ever work. */
  refused: boolean;
  subscribers: Set<() => void>;
}

const buckets = new Map<string, Bucket>();

function bucketFor(key: string): Bucket {
  let b = buckets.get(key);
  if (!b) {
    b = {
      local: {},
      written: new Set(),
      inFlight: false,
      pending: false,
      attempt: 0,
      refused: false,
      subscribers: new Set(),
    };
    buckets.set(key, b);
  }
  return b;
}

const url = (key: string) => `/api/uistate?key=${encodeURIComponent(key)}`;

function notify(b: Bucket): void {
  for (const fn of b.subscribers) fn();
}

/**
 * readUIState fetches the bucket once and hands every later caller the same
 * promise. A dozen components asking for their own field on mount is the normal
 * case, and a dozen requests for one document is not.
 *
 * Anything already written locally survives the load: a column dragged before
 * the response came back must not be undone by it.
 */
export function readUIState(key: string = DEFAULT_BUCKET): Promise<UIState> {
  const b = bucketFor(key);
  if (!b.read) {
    b.read = fetch(url(key))
      .then((r) => (r.ok ? (r.json() as Promise<UIState>) : {}))
      .catch(() => ({}) as UIState)
      .then((stored) => {
        for (const [field, value] of Object.entries(stored ?? {})) {
          if (!b.written.has(field)) b.local[field] = value;
        }
        notify(b);
        return b.local;
      });
  }
  return b.read;
}

/** peekUIState is what the bucket holds right now, without waiting for the load. */
export function peekUIState<T>(field: string, fallback: T, key: string = DEFAULT_BUCKET): T {
  const v = bucketFor(key).local[field];
  return v === undefined ? fallback : (v as T);
}

/**
 * writeUIState records a field and schedules the document to be written. It
 * returns nothing and cannot fail: the value is kept locally either way, and the
 * request is this module's problem rather than the caller's.
 */
export function writeUIState(field: string, value: unknown, key: string = DEFAULT_BUCKET): void {
  const b = bucketFor(key);
  if (value === undefined) delete b.local[field];
  else b.local[field] = value;
  b.written.add(field);
  // A change after a refusal is worth trying again — it may well be the change
  // that brings the document back under the cap. The retry backoff is
  // deliberately not reset: a server that is down stays down, and going back to
  // one request per keystroke would turn an outage into a flood. Nothing is at
  // risk in the meantime, because the value is already held here and the tab
  // closing flushes past the backoff.
  b.refused = false;
  notify(b);
  schedule(b, key, FLUSH_DELAY_MS);
}

function schedule(b: Bucket, key: string, delay: number): void {
  b.pending = true;
  if (b.timer !== undefined) clearTimeout(b.timer);
  b.timer = setTimeout(() => {
    b.timer = undefined;
    void flush(b, key);
  }, delay);
}

async function flush(b: Bucket, key: string, keepalive = false): Promise<void> {
  // One request at a time. A second one would race the first and could land the
  // older document last, which is exactly the lost-layout this module exists to
  // prevent; the flag set here makes the finishing request re-send instead.
  if (b.inFlight || b.refused || !b.pending) return;
  const body = JSON.stringify(b.local);
  if (body.length > MAX_BYTES) {
    b.refused = true;
    b.pending = false;
    console.warn(
      `uistate: bucket "${key}" is ${body.length} bytes and the limit is ${MAX_BYTES}; ` +
        'it is kept for this session but will not survive a reload',
    );
    return;
  }
  b.inFlight = true;
  b.pending = false;
  try {
    const r = await fetch(url(key), {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body,
      keepalive: keepalive && body.length <= KEEPALIVE_MAX_BYTES,
    });
    if (!r.ok) throw new Error(await r.text());
    b.attempt = 0;
  } catch {
    // The local copy is untouched on purpose. The document is re-sent whole on
    // the next attempt, so a write that happened meanwhile rides along and
    // nothing has to be replayed in order.
    b.attempt++;
    b.pending = true;
  } finally {
    b.inFlight = false;
  }
  if (b.pending) {
    schedule(b, key, b.attempt > 0 ? Math.min(RETRY_BASE_MS * 2 ** (b.attempt - 1), RETRY_MAX_MS) : 0);
  }
}

/** flushUIState writes anything outstanding now instead of waiting for the debounce. */
export function flushUIState(key: string = DEFAULT_BUCKET): Promise<void> {
  const b = bucketFor(key);
  if (b.timer !== undefined) {
    clearTimeout(b.timer);
    b.timer = undefined;
  }
  return flush(b, key);
}

// The tab going away is the one moment a debounce costs the user their change,
// and it is also the moment the last change was made — closing the tab right
// after resizing a column is the normal way to do it. pagehide fires where
// unload is unreliable, and visibilitychange catches the mobile case where a tab
// is discarded without either.
if (typeof document !== 'undefined') {
  const flushAll = () => {
    for (const [key, b] of buckets) {
      if (b.timer !== undefined) {
        clearTimeout(b.timer);
        b.timer = undefined;
      }
      if (b.pending) void flush(b, key, true);
    }
  };
  addEventListener('pagehide', flushAll);
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'hidden') flushAll();
  });
}

/**
 * useUIState is one remembered field, with the same shape as useState.
 *
 * The fallback is what the field is worth until the load answers and whenever it
 * has never been written, so a first-run interface renders its built-in layout
 * rather than an empty one that fills in a moment later.
 */
export function useUIState<T>(
  field: string,
  fallback: T,
  key: string = DEFAULT_BUCKET,
): [T, (value: T) => void] {
  const [value, setValue] = useState<T>(() => peekUIState(field, fallback, key));

  useEffect(() => {
    const b = bucketFor(key);
    const sync = () => setValue(peekUIState(field, fallback, key));
    b.subscribers.add(sync);
    void readUIState(key).then(sync);
    return () => {
      b.subscribers.delete(sync);
    };
    // fallback is deliberately left out of the dependencies: callers pass an
    // inline default ({} or []), which is a new reference on every render, and
    // subscribing again each time would re-run the effect forever.
  }, [field, key]);

  const set = useCallback(
    (next: T) => {
      setValue(next);
      writeUIState(field, next, key);
    },
    [field, key],
  );

  return [value, set];
}
