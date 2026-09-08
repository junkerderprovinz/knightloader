// The session's own notification log: everything this window told you, kept
// long enough to read it after the bubble has gone.
//
// WHAT THIS IS NOT, first, because the whole interface hangs off it. There is
// no route behind this. internal/script/bus.go publishes eleven triggers and
// says in so many words that the Hub is deliberately NOT a second subscriber
// to it, so no browser ever sees the bus; GET /api/history is a durable list
// of finished DOWNLOADS, not of notifications. The server therefore cannot
// answer "what happened before you opened this page" or "what happened while
// the tab was shut", and nothing built on top of this may pretend that it can:
// no "load older", no date range, no "since your last visit". The panel's own
// title bubble (events.titleHint) says the list starts with the page load, and
// that sentence is the honest ceiling of this file rather than an apology for
// it.
//
// So it is fed at the one choke point every notification in this app already
// passes through - toast() in lib/toast.tsx - and lives in module memory for
// as long as the tab does. Feeding it there rather than from a second
// WebSocket subscription is what makes the log and the bubbles the same set of
// facts: there is no second source that could disagree.
//
// TRAP, and it is the one worth reading twice: recordEvent() is called ABOVE
// quiet mode's own early return and above the notify matrix's 'silent'/'system'
// branches, never beside setItems. Drop it in the natural-looking place and
// every non-critical event is silently missing from the log exactly when quiet
// mode is on - which is precisely the state somebody opens a bell in to find
// out what they missed. Quiet mode is supposed to mean "do not interrupt me",
// not "never tell me".
//
// TRAP, the second: this ring is deliberately NOT persisted through
// lib/uistate.ts. That bucket is shared between browsers (uistate.ts's own
// note), the whole document is capped at 256 KiB, and going over the cap makes
// it refuse the ENTIRE document and stop persisting for the session - so three
// hundred events sharing a document with every column width and every folded
// package name does not fail as "events do not survive a reload", it fails as
// "column widths stopped saving and nobody knows why". Module memory, and the
// hint says so.
import { useSyncExternalStore } from 'react';
import type { NotificationKind, ToastTone } from './toast';

/**
 * What a logged event points at, when it points at anything.
 *
 * One shape rather than a union: the download list is the only surface with a
 * row to jump to today, and a union member nothing constructs is a branch
 * nothing tests. There is no instance field either - every recorded event comes
 * from Layout's own local socket (no base, no peer), so a target is always
 * about this machine and a field for it would only be something to get wrong.
 *
 * Named EventSubject and not EventTarget on purpose: `EventTarget` is a DOM
 * global, and a project type that shadows one is a type somebody eventually
 * writes by accident meaning the other.
 */
export interface EventSubject {
  kind: 'task';
  /** core.Task.id on this instance. */
  id: string;
}

export interface LoggedEvent {
  /**
   * Monotonic within this page load. The unread mark is an id and not a
   * timestamp because two events can share a millisecond, and a mark that
   * cannot separate them either re-reads one or skips one.
   */
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

/**
 * Derived from NotificationKind rather than hand-listed a second time, so the
 * thirteenth kind somebody adds over in toast.tsx is a compile error here
 * instead of an event that quietly belongs to no family and is unreachable
 * through every chip in the panel.
 */
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

/**
 * A few hundred. 300 events at roughly 200 bytes is about 60 kB of page memory
 * and nothing else - the ring never leaves this tab, so the only budget it
 * spends is one nobody else is drawing on.
 */
export const CAPACITY = 300;

// Newest first, which is also the order the panel draws: a log read from the
// top is a log where the thing that just happened is the thing you see.
let events: LoggedEvent[] = [];
let seq = 0;
let lastSeenId = 0;
let unread = 0;
const listeners = new Set<() => void>();

// One frozen empty array for the server snapshot, never a fresh literal:
// useSyncExternalStore compares by reference, and a getter that builds its own
// value renders for ever (lib/listview.ts says the same thing in its own
// words).
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

// Recounted from the ring on every mutation rather than incremented as events
// arrive: eviction can drop an entry that was never read (300 unread in one
// sitting is rare, not impossible), and a counter that only ever goes up would
// then promise a row the list no longer holds.
function recount(): void {
  let n = 0;
  for (const e of events) if (e.id > lastSeenId) n++;
  unread = n;
}

/**
 * The one writer. Called from toast(), and from nowhere else - a second caller
 * would be a second source of truth about what this window has said.
 */
export function recordEvent(e: Omit<LoggedEvent, 'id' | 'at'>): void {
  // A new array, not a mutation: the snapshot below hands this reference
  // straight to React, so the identity IS the change signal.
  events = [{ ...e, id: ++seq, at: Date.now() }, ...events];
  if (events.length > CAPACITY) events = events.slice(0, CAPACITY);
  recount();
  notify();
}

/**
 * Empties the list and leaves the unread mark where it stands.
 *
 * The one destructive control in the panel, with no undo behind it, which is
 * why the button that calls it sits at the foot of the panel away from the
 * rows rather than anywhere a press aimed at a row could land on it.
 */
export function clearEvents(): void {
  events = [];
  recount();
  notify();
}

/**
 * Everything currently held is read.
 *
 * Belongs to the panel OPENING and to nothing else. Called on mount, the badge
 * is zero for ever after a reload; hung off the bell's focus, tabbing past the
 * sidebar clears it; hung off a timer, an event nobody was looking at counts as
 * read. Open the panel and it is read, because it is on screen.
 */
export function markEventsSeen(): void {
  const top = events[0]?.id ?? lastSeenId;
  if (top === lastSeenId) return;
  lastSeenId = top;
  recount();
  notify();
}

/** The held array itself. See NONE above for why the getter builds nothing. */
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

// --- The panel's own open flag ---------------------------------------------
//
// Module scope for the same reason lib/commandPaletteOpen.ts's is: the "open
// the event list" command in lib/commands/global.ts runs with no reference to
// the component that draws the panel, and the two have to meet somewhere that
// is not a prop chain from whichever surface ran the command down to the
// sidebar.
//
// It lives in this file rather than in a lib/eventsPanelOpen.ts of its own
// because the bell is one feature with one reader: a second module holding a
// single boolean about this same panel is a file whose only job is to be
// imported.
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
