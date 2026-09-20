// What the interface remembers between reloads: column widths and order,
// folded packages, the settings page open last. One JSON object per bucket,
// written through to GET/PUT /api/uistate.
//
// The local copy is the truth: the server is read once at the start and never
// overrides what the user is doing. A failed write keeps the value and
// retries rather than rolling back. Writes are debounced, because dragging a
// column edge produces a value per frame.

import { useCallback, useEffect, useState } from 'react';

/** The stored document: whatever the interface put in it, by field name. */
export type UIState = Record<string, unknown>;

/** The shared bucket, so the layout follows the single user between browsers.
 *  A client that wants its own passes another key. */
export const DEFAULT_BUCKET = 'default';

// Long enough to swallow a drag; pagehide catches a tab closed in the meantime.
const FLUSH_DELAY_MS = 600;

const RETRY_BASE_MS = 2_000;
const RETRY_MAX_MS = 30_000;

// Mirrors store.MaxUIStateBytes, so a document that can never be accepted is
// not retried forever.
const MAX_BYTES = 256 << 10;

// The fetch spec caps keepalive bodies at 64 KiB. A larger document goes out as
// an ordinary request the browser may cancel; sendBeacon cannot PUT.
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
 * promise. Fields written locally before the response arrives are kept.
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
 * cannot fail: the value is kept locally, and retries are handled here.
 */
export function writeUIState(field: string, value: unknown, key: string = DEFAULT_BUCKET): void {
  const b = bucketFor(key);
  if (value === undefined) delete b.local[field];
  else b.local[field] = value;
  b.written.add(field);
  // This change may bring the document back under the cap, so try again. The
  // backoff is kept, so an outage does not turn into a request per keystroke.
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
  // One request at a time, or an older document could land last. A change
  // made meanwhile leaves pending set, and the finishing request re-sends.
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
    // The whole document is re-sent next time, including later writes.
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

// Closing the tab right after a change is common, so outstanding writes are
// flushed on pagehide, and on visibilitychange for mobile tabs that are
// discarded without one.
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
 * useUIState is one remembered field, with the same shape as useState. The
 * fallback applies until the load answers and whenever the field was never
 * written.
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
    // fallback is left out: callers pass inline defaults, a new reference on
    // every render, which would re-run the effect forever.
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
