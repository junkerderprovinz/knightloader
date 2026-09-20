// The session's notification log: everything this window reported, kept so it
// can be read after the bubble has gone.
//
// There is no server side. The script bus is not relayed to browsers and
// /api/history lists finished downloads, so the log starts with the page load
// and offers nothing older; the panel's title hint says so. It is fed from
// toast(), which every notification passes through, so the log and the
// bubbles are the same set of facts.
//
// recordEvent() is called before toast()'s quiet-mode and routing returns, so
// events swallowed by quiet mode still show up here.
//
// The ring stays in memory rather than lib/uistate.ts: that document is
// capped at 256 KiB, and going over refuses all of it, column widths included.
import { useSyncExternalStore } from 'react';
import type { NotificationKind, ToastTone } from './toast';

/**
 * What a logged event points at. Only download rows can be jumped to so far,
 * and every event comes from this machine's own socket, so there is no
 * instance field. Not called EventTarget, which is a DOM global.
 */
export interface EventSubject {
  kind: 'task';
  /** core.Task.id on this instance. */
  id: string;
}

export interface LoggedEvent {
  /** Monotonic within this page load. The unread mark uses the id, because
   *  two events can share a millisecond. */
  id: number;
  /** Date.now() at the moment toast() was called. */
  at: number;
  message: string;
  tone: ToastTone;
  kind: NotificationKind;
  target?: EventSubject;
}

/** The five families the kind filter offers. */
export type EventFamily = 'downloads' | 'archives' | 'captcha' | 'accounts' | 'actions';

/** Keyed by NotificationKind, so a new kind without a family fails to compile. */
export const FAMILY_OF: Record<NotificationKind, EventFamily> = {
  'download-done': 'downloads',
  'download-failed': 'downloads',
  'extraction-done': 'archives',
  'extraction-failed': 'archives',
  'captcha-needs-answer': 'captcha',
  'captcha-resolved': 'captcha',
  'captcha-failed': 'captcha',
  'account-benched': 'accounts',
  'account-restored': 'accounts',
  'action-done': 'actions',
  'action-failed': 'actions',
  info: 'actions',
};

/** Chip order, most specific first: Actions is the catch-all and sits last. */
export const EVENT_FAMILIES: readonly EventFamily[] = ['downloads', 'archives', 'captcha', 'accounts', 'actions'];

/** About 60 kB of page memory at 200 bytes an event. */
export const CAPACITY = 300;

// Newest first, the order the panel draws.
let events: LoggedEvent[] = [];
let seq = 0;
let lastSeenId = 0;
let unread = 0;
const listeners = new Set<() => void>();

// A constant server snapshot, since useSyncExternalStore compares by reference.
const NONE: readonly LoggedEvent[] = [];

function notify(): void {
  for (const l of listeners) l();
}

function subscribe(l: () => void): () => void {
  listeners.add(l);
  return () => {
    listeners.delete(l);
  };
}

// Recounted rather than incremented, because eviction can drop unread events.
function recount(): void {
  let n = 0;
  for (const e of events) if (e.id > lastSeenId) n++;
  unread = n;
}

/** recordEvent adds an event. Only toast() calls it. */
export function recordEvent(e: Omit<LoggedEvent, 'id' | 'at'>): void {
  // A new array, because its identity is React's change signal.
  events = [{ ...e, id: ++seq, at: Date.now() }, ...events];
  if (events.length > CAPACITY) events = events.slice(0, CAPACITY);
  recount();
  notify();
}

/** clearEvents empties the list and leaves the unread mark where it is. */
export function clearEvents(): void {
  events = [];
  recount();
  notify();
}

/**
 * markEventsSeen marks everything held as read. Call it when the panel opens
 * and nowhere else: on mount or on focus the badge would clear without anyone
 * having seen the events.
 */
export function markEventsSeen(): void {
  const top = events[0]?.id ?? lastSeenId;
  if (top === lastSeenId) return;
  lastSeenId = top;
  recount();
  notify();
}

/** The held array itself, so the snapshot keeps its identity. */
export function useEventLog(): readonly LoggedEvent[] {
  return useSyncExternalStore(
    subscribe,
    () => events,
    () => NONE,
  );
}

export function useUnreadEvents(): number {
  return useSyncExternalStore(
    subscribe,
    () => unread,
    () => 0,
  );
}

// Whether the panel is open. Module-level, like lib/commandPaletteOpen.ts, so
// the "open the event list" command can reach the panel without a prop chain.
let panelOpen = false;
const panelListeners = new Set<() => void>();

function setPanel(next: boolean): void {
  if (next === panelOpen) return;
  panelOpen = next;
  for (const fn of panelListeners) fn();
}

export function setEventsPanelOpen(next: boolean): void {
  setPanel(next);
}

export function useEventsPanelOpen(): boolean {
  return useSyncExternalStore(
    (fn) => {
      panelListeners.add(fn);
      return () => {
        panelListeners.delete(fn);
      };
    },
    () => panelOpen,
    () => false,
  );
}
