// "Show me that row": a request from the event panel (components/EventBell.tsx)
// to whichever download list is mounted. The row may have been removed since,
// or the list may still be refilling from the socket, so it is a request with
// a deadline rather than a call with a return value. The deadline lives here
// so two lists cannot both report a miss for one press.
import { useSyncExternalStore } from 'react';

export interface RevealRequest {
  /** New on every request, so the same row can be revealed twice running;
   *  TaskListCard ignores a repeat of the value it last handled. */
  nonce: number;
  /** core.Task.id. */
  id: string;
}

// Long enough for a navigation plus the socket snapshot after it: switching
// instance on /downloads does not remount the page, so the row arrives a beat
// after the navigation.
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
 * requestReveal asks the mounted list to show a row. `missed` fires once if no
 * list claims the request before the deadline; the caller decides how to
 * report it.
 */
export function requestReveal(id: string, missed: () => void): void {
  disarm();
  const nonce = ++seq;
  current = { nonce, id };
  onMissed = missed;
  timer = setTimeout(() => {
    timer = undefined;
    // A timer that already fired cannot be cleared, so check it still owns the request.
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
 * claimReveal takes the request. The list calls it before clearing filters
 * and expanding packages, so its own state changes do not trigger it again.
 */
export function claimReveal(nonce: number): void {
  if (current?.nonce !== nonce) return;
  disarm();
  current = null;
  onMissed = null;
  notify();
}

/** The held object itself; a fresh one per call would render forever. */
export function useRevealRequest(): RevealRequest | null {
  return useSyncExternalStore(
    subscribe,
    () => current,
    () => null,
  );
}
