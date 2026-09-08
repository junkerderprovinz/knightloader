// "Show me that row" - the request, and the one answer it is allowed to get.
//
// A press in the event panel (components/EventBell.tsx) names a task id and
// nothing else. Whether that row can actually be shown is a question only the
// download list can answer, and it may not be able to answer it at all: a
// logged download can have been removed by a clean-up, by retention or by hand
// long before somebody opens the bell, and the page may still be filling its
// task map from a fresh socket snapshot when the request arrives. So this is a
// request with a deadline rather than a call with a return value.
//
// WHY THE DEADLINE LIVES HERE and not in the page that watches for the
// request: two lists both timing out would produce two "that download is gone"
// bubbles for one press. One store, one timer, one answer.
//
// WHY A NONCE and not a bare id: revealing the same row twice in a row has to
// work. TaskListCard guards its scroll effect with the last value it handled
// (it must - see its own note on the scroll/window feedback loop), so a second
// request carrying the identical string is a request the guard swallows.
import { useSyncExternalStore } from 'react';

export interface RevealRequest {
  /** New on every request, so the same row can be revealed twice running. */
  nonce: number;
  /** core.Task.id. */
  id: string;
}

/**
 * Long enough for a navigation plus the socket snapshot that follows it, short
 * enough that "nothing happened" is never the answer somebody is left with.
 * Measured against the real failure it guards: Layout keys its page div on the
 * section alone, so going from /downloads?instance=nas to /downloads does not
 * remount the page - useTasks resets to {} and refills from the socket, and the
 * row appears a beat after the navigation rather than with it.
 */
const DEADLINE_MS = 2500;

let current: RevealRequest | null = null;
let onMissed: (() => void) | null = null;
let timer: ReturnType<typeof setTimeout> | undefined;
let seq = 0;
const listeners = new Set<() => void>();

function notify(): void {
  for (const l of listeners) l();
}

function subscribe(l: () => void): () => void {
  listeners.add(l);
  return () => {
    listeners.delete(l);
  };
}

function disarm(): void {
  if (timer !== undefined) clearTimeout(timer);
  timer = undefined;
}

/**
 * Ask whichever list is mounted to show this row.
 *
 * `missed` fires exactly once, and only if no list claimed the request before
 * the deadline. It is the caller's because the caller is the one with a bubble
 * to raise; this file has no opinion about how a miss is reported.
 */
export function requestReveal(id: string, missed: () => void): void {
  disarm();
  const nonce = ++seq;
  current = { nonce, id };
  onMissed = missed;
  timer = setTimeout(() => {
    timer = undefined;
    // Guarded even though disarm() runs on every path that replaces the
    // request: a timer that has already fired cannot be cleared, and the queued
    // callback would otherwise cancel a request made in the same tick.
    if (current?.nonce !== nonce) return;
    current = null;
    const cb = onMissed;
    onMissed = null;
    notify();
    cb?.();
  }, DEADLINE_MS);
  notify();
}

/**
 * "I have it, I am showing it." Called by the list BEFORE it starts clearing
 * filters and expanding packages, so the effect that watches the request does
 * not run a second time against its own state changes.
 */
export function claimReveal(nonce: number): void {
  if (current?.nonce !== nonce) return;
  disarm();
  current = null;
  onMissed = null;
  notify();
}

/** The held object itself - a fresh one per call would render for ever. */
export function useRevealRequest(): RevealRequest | null {
  return useSyncExternalStore(
    subscribe,
    () => current,
    () => null,
  );
}
