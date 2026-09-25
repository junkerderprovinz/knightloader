// Getting from a search result to the actual row on a page that is not on
// screen yet. A module-scope store carries the request from the result's click
// handler to the page, as lib/commandPaletteOpen.ts does, and the DOM half
// finds the card and row, scrolls to them, marks them and focuses the control.
import { useSyncExternalStore } from 'react';
import type { TranslationKey } from '../../lib/i18n';

export interface JumpTarget {
  /** The settings page id, as the server hands it out. */
  page: string;
  /** The card's SectionTitle key; a jump always reaches at least the card. */
  title: TranslationKey;
  /** Absent for a whole-card result. */
  label?: TranslationKey;
  /**
   * Bumped on every request, so picking the same result twice is still a new
   * value that subscribers react to.
   */
  nonce: number;
}

let pending: JumpTarget | null = null;
let nonce = 0;
const listeners = new Set<() => void>();

function emit(): void {
  for (const fn of listeners) fn();
}

/** requestJump asks for a jump; the caller navigates to the page right after. */
export function requestJump(to: Omit<JumpTarget, 'nonce'>): void {
  pending = { ...to, nonce: ++nonce };
  emit();
}

/** clearJump spends the request, whether or not it succeeded. */
export function clearJump(): void {
  if (pending === null) return;
  pending = null;
  emit();
}

export function pendingJump(): JumpTarget | null {
  return pending;
}

export function subscribeJump(fn: () => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

/** usePendingJump reads the request during render. */
export function usePendingJump(): JumpTarget | null {
  return useSyncExternalStore(subscribeJump, pendingJump, pendingJump);
}

// The second store puts the cursor in the settings search box. The "Search all
// settings" command cannot reach the field, which lives three routes away, so
// it bumps this and navigates, and the field watches it.

/**
 * When focus was last asked for, or 0. A timestamp, because the field usually
 * mounts after the request, and a request that never reached Settings has to
 * expire instead of stealing focus the next time somebody opens it.
 */
let focusAskedAt = 0;
const focusListeners = new Set<() => void>();

/** How long a request stays worth honouring: enough for a route change and a mount. */
export const FOCUS_REQUEST_TTL_MS = 5000;

/** requestSearchFocus asks the settings search field to take focus once it exists. */
export function requestSearchFocus(): void {
  focusAskedAt = Date.now();
  for (const fn of focusListeners) fn();
}

/** clearSearchFocus spends the request, taken or given up on. */
export function clearSearchFocus(): void {
  if (focusAskedAt === 0) return;
  focusAskedAt = 0;
  for (const fn of focusListeners) fn();
}

export function searchFocusAskedAt(): number {
  return focusAskedAt;
}

export function subscribeSearchFocus(fn: () => void): () => void {
  focusListeners.add(fn);
  return () => focusListeners.delete(fn);
}

export function useSearchFocusAskedAt(): number {
  return useSyncExternalStore(subscribeSearchFocus, searchFocusAskedAt, searchFocusAskedAt);
}

/**
 * How long to keep looking: the sub-page mounts a render after navigate(), and
 * some cards appear only once a fetch resolves.
 */
const DEADLINE_MS = 2000;

const MARK_MS = 1400;

/**
 * Everything focusable a row might contain. The caption's own (i) is excluded
 * at the call site, since it comes first and would open a tooltip.
 */
const FOCUSABLE = 'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])';

export type JumpOutcome = 'row' | 'card' | 'nothing';

/**
 * contentRoot is the settings content column, not the document: the rail
 * repeats page names and tooltips are portaled onto document.body.
 */
function contentRoot(): HTMLElement | null {
  return document.querySelector<HTMLElement>('[data-settings-content]');
}

/**
 * findCard returns the card whose title badge reads exactly `title`. The
 * badge's textContent is the translated title, since the InfoBubble trigger is
 * an svg.
 */
function findCard(root: HTMLElement, title: string): HTMLElement | null {
  for (const badge of root.querySelectorAll<HTMLElement>('.glim-section-badge')) {
    if ((badge.textContent ?? '').trim() !== title) continue;
    const card = badge.closest<HTMLElement>('.glim-card');
    // A badge outside a card is not the answer; keep looking.
    if (card) return card;
  }
  return null;
}

/**
 * findRow returns the caption in the card whose text is exactly `label`. It
 * compares the property, because a built selector breaks on quotes.
 */
function findRow(card: HTMLElement, label: string): HTMLElement | null {
  for (const el of card.querySelectorAll<HTMLElement>('[data-glim-label]')) {
    if (el.dataset.glimLabel === label) return el;
  }
  return null;
}

/** motionAllowed checks both the OS preference and the app's own motion level. */
function motionAllowed(): boolean {
  const reduced = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false;
  return !reduced && document.documentElement.dataset.motion !== 'off';
}

/**
 * mark puts the locate class on an element and restarts its animation, which
 * a class that is already present would not do.
 */
function mark(el: HTMLElement): void {
  el.classList.remove('glim-locate');
  void el.offsetWidth;
  el.classList.add('glim-locate');
  window.setTimeout(() => el.classList.remove('glim-locate'), MARK_MS);
}

/**
 * locateAndMark finds the card, and the row inside it when one was asked for,
 * scrolls it into view and marks it. It returns a cancel function and calls
 * `onDone` exactly once.
 */
export function locateAndMark(opts: {
  /** The card title, already translated. */
  title: string;
  /** The row caption, already translated. Omit for a whole-card jump. */
  label?: string;
  onDone: (outcome: JumpOutcome) => void;
}): () => void {
  const { title, label, onDone } = opts;
  const smooth = motionAllowed();
  let finished = false;
  let observer: MutationObserver | null = null;
  let timer: number | null = null;

  function finish(outcome: JumpOutcome, card: HTMLElement | null, row: HTMLElement | null): void {
    if (finished) return;
    finished = true;
    // Disconnect first, or marking the node would re-enter this callback.
    observer?.disconnect();
    observer = null;
    if (timer !== null) window.clearTimeout(timer);
    timer = null;

    if (card) {
      // Mark the whole row: the <label> a Field wraps around caption and
      // control, or the ToggleRow's div.
      const marked = row ? (row.closest('label') ?? row.parentElement ?? card) : card;
      marked.scrollIntoView({ block: 'center', behavior: smooth ? 'smooth' : 'auto' });
      mark(marked);
      if (row) {
        // Focus only, never activate: the first control is often a switch and
        // settings autosave. The caption's own (i) is skipped, and
        // preventScroll keeps the smooth scroll above running.
        const inRow = [...marked.querySelectorAll<HTMLElement>(FOCUSABLE)].filter((n) => !row.contains(n));
        // A link beside the control leads to another page, such as a module
        // switch's way to the same switch on the Modules page.
        const control = inRow.find((n) => !n.matches('a[href]')) ?? inRow[0];
        control?.focus({ preventScroll: true });
      }
    }
    onDone(outcome);
  }

  function attempt(final: boolean): void {
    const root = contentRoot();
    const card = root ? findCard(root, title) : null;
    if (card) {
      const row = label ? findRow(card, label) : null;
      if (row) return finish('row', card, row);
      if (!label) return finish('card', card, null);
      // The card is here but the row may still be one fetch away.
      if (final) return finish('card', card, null);
      return;
    }
    if (final) finish('nothing', null, null);
  }

  attempt(false);
  if (finished) return () => {};

  const root = contentRoot();
  if (root) {
    observer = new MutationObserver(() => attempt(false));
    observer.observe(root, { childList: true, subtree: true });
  }
  timer = window.setTimeout(() => attempt(true), DEADLINE_MS);

  return () => {
    if (finished) return;
    finished = true;
    observer?.disconnect();
    if (timer !== null) window.clearTimeout(timer);
  };
}
