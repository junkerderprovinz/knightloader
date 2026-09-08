// Getting from a search result to the actual row, on a page that is not on
// screen yet.
//
// Two halves, and both of them are here because they are the same mechanism:
// the thing that asks and the thing that does it never hold a reference to each
// other.
//
//   - a module-scope store, the same shape lib/commandPaletteOpen.ts uses and
//     for the same reason (that file's doc comment gives it in full): a result
//     row's click handler and the effect that walks the DOM live in different
//     components, and a prop passed between them would have to travel through
//     the router.
//   - the DOM work itself: find the card, find the row inside it, scroll it into
//     view, mark it, put focus on it. No React in any of that - by the time it
//     runs, what matters is what actually rendered, not what was meant to.
import { useSyncExternalStore } from 'react';
import type { TranslationKey } from '../../lib/i18n';

export interface JumpTarget {
  /** The settings page id, as the server hands it out. */
  page: string;
  /** The card's SectionTitle key. Always present: a jump is at least to a card. */
  title: TranslationKey;
  /** Absent for a whole-card result. */
  label?: TranslationKey;
  /**
   * Monotonic, and load-bearing.
   *
   * Picking the same result twice writes an identical value into this store,
   * every subscriber compares it equal, React bails out, and no effect runs -
   * so "show me that row again" would be a no-op with no error anywhere. The
   * nonce is the same trick lib/toast.tsx's push() uses when it mints a fresh id
   * per call, and the same one index.css's own confirm-pulse note describes for
   * replaying an animation: the request has to be a NEW thing, not an equal one.
   */
  nonce: number;
}

let pending: JumpTarget | null = null;
let nonce = 0;
const listeners = new Set<() => void>();

function emit(): void {
  for (const fn of listeners) fn();
}

/** Ask for a jump. Call navigate() to the page yourself, right after this. */
export function requestJump(to: Omit<JumpTarget, 'nonce'>): void {
  pending = { ...to, nonce: ++nonce };
  emit();
}

/** Done with it - success or failure, the request is spent either way. */
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

/** React binding - useSyncExternalStore, read during render rather than an effect-tick late. */
export function usePendingJump(): JumpTarget | null {
  return useSyncExternalStore(subscribeJump, pendingJump, pendingJump);
}

// ---------------------------------------------------------------------------
// The second store: "put the cursor in the settings search box".
//
// Same problem, one size smaller. lib/commands/settings.ts mints a "Search all
// settings" command whose run() has no way to reach the field - it is a plain
// function in the command registry, and the field is a component three routes
// away. It bumps this counter and navigates; the field watches it.
// ---------------------------------------------------------------------------

/**
 * When focus was last asked for, or 0.
 *
 * A timestamp and not a flag, and not a counter either, because of who is asking
 * and when. The command navigates to /settings and asks in the same breath, so
 * the field usually does not exist yet and has to honour a request made BEFORE
 * it mounted - which rules out "react to the value changing", since on a fresh
 * mount nothing has changed. A bare flag then has the opposite problem: run the
 * command with the app somewhere that never reaches Settings, and the flag sits
 * there until the next time somebody opens Settings for their own reasons and
 * has the cursor taken away from them. The age is what tells those two apart.
 */
let focusAskedAt = 0;
const focusListeners = new Set<() => void>();

/** How long a request stays worth honouring. Long enough for a route change and
 *  a mount, far too short to survive somebody walking away. */
export const FOCUS_REQUEST_TTL_MS = 5000;

/** Ask the settings search field to take focus, as soon as it exists. */
export function requestSearchFocus(): void {
  focusAskedAt = Date.now();
  for (const fn of focusListeners) fn();
}

/** Taken, or given up on. Either way the request is spent. */
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

// ---------------------------------------------------------------------------
// The DOM half.
// ---------------------------------------------------------------------------

/**
 * How long to keep looking before giving up.
 *
 * Two unrelated reasons the row is not there the moment the result is picked:
 * the sub-page mounts one render AFTER navigate(), and whole cards only appear
 * once a fetch resolves (DownloadsSettings draws its end-of-queue card only
 * after fetchIdleActions answers, and its resume strip only after fetchOptions
 * does). A single requestAnimationFrame lookup misses both silently, and the
 * reader lands at the top of a page wondering what happened.
 */
const DEADLINE_MS = 2000;

/** How long the mark stays on the row before the class comes back off. */
const MARK_MS = 1400;

/**
 * Everything focusable a row might contain. The caption's own (i) is excluded
 * at the call site rather than here: it is `tabindex=0` and it comes first in
 * document order, so a plain querySelector would open a tooltip instead of
 * landing on the control.
 */
const FOCUSABLE = 'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])';

/** What a completed attempt found. */
export type JumpOutcome = 'row' | 'card' | 'nothing';

/** The settings content column, and never the whole document: the rail draws
 *  the same page names, and an InfoBubble's tip is portaled onto document.body
 *  where it would match card and row text alike. */
function contentRoot(): HTMLElement | null {
  return document.querySelector<HTMLElement>('[data-settings-content]');
}

/**
 * The card whose title badge reads exactly `title`.
 *
 * ui.tsx's SectionTitle draws `.glim-section-badge` containing the title text
 * and, optionally, an InfoBubble whose trigger is an `<svg>` only - so the
 * badge's own textContent is exactly the translated card title, with no markup
 * change anywhere and no id to keep in step at 88 call sites.
 */
function findCard(root: HTMLElement, title: string): HTMLElement | null {
  for (const badge of root.querySelectorAll<HTMLElement>('.glim-section-badge')) {
    if ((badge.textContent ?? '').trim() !== title) continue;
    const card = badge.closest<HTMLElement>('.glim-card');
    // Keep looking rather than returning null: a badge that is not inside a card
    // is not the answer, but it is also no reason to stop before the one that is.
    if (card) return card;
  }
  return null;
}

/**
 * The caption inside that card whose text is exactly `label`.
 *
 * Compares the PROPERTY, never a built selector: a caption can contain a quote,
 * an apostrophe or a bracket in any of 42 languages, and
 * `[data-glim-label="..."]` would either throw or quietly match nothing.
 */
function findRow(card: HTMLElement, label: string): HTMLElement | null {
  for (const el of card.querySelectorAll<HTMLElement>('[data-glim-label]')) {
    if (el.dataset.glimLabel === label) return el;
  }
  return null;
}

/** Whether this browser and this reader want anything to move. Both halves,
 *  the way index.css pairs them: the OS preference and the app's own picker. */
function motionAllowed(): boolean {
  const reduced = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false;
  return !reduced && document.documentElement.dataset.motion !== 'off';
}

/**
 * Put the mark on an element, replayably.
 *
 * remove / read offsetWidth / add is the DOM equivalent of the fresh-node
 * discipline index.css spells out for glim-confirm: a class that is already
 * there does not restart its animation, so jumping to the same row twice would
 * scroll and then sit there doing nothing.
 */
function mark(el: HTMLElement): void {
  el.classList.remove('glim-locate');
  void el.offsetWidth;
  el.classList.add('glim-locate');
  window.setTimeout(() => el.classList.remove('glim-locate'), MARK_MS);
}

/**
 * Find the card (and the row inside it, when one was asked for), bring it into
 * view and mark it. Returns a cancel function.
 *
 * `onDone` is called exactly once.
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
    // Disconnect BEFORE touching anything. The observer watches the content
    // column; the mark below adds a class to a node inside it; that mutation
    // would re-enter this callback, find the same node and mark it again. A
    // re-entrancy flag would paper over it - disconnecting is what actually
    // stops it, and it is what has to happen anyway.
    observer?.disconnect();
    observer = null;
    if (timer !== null) window.clearTimeout(timer);
    timer = null;

    if (card) {
      // The whole ROW, not the caption inside it. `row` is the caption span; the
      // thing worth ringing and scrolling to is the caption together with the
      // control it names, which is the <label> a Field wraps them both in, or
      // the flex div a ToggleRow uses instead. Falling back to the card means a
      // ring around the card, which is exactly right when no row was asked for.
      const marked = row ? (row.closest('label') ?? row.parentElement ?? card) : card;
      marked.scrollIntoView({ block: 'center', behavior: smooth ? 'smooth' : 'auto' });
      mark(marked);
      if (row) {
        // Focus, and ONLY focus. The first focusable thing in a settings row is
        // very often a role="switch", every settings tab autosaves 600ms after
        // an edit, and there is no Save button anywhere to catch a mistake - so
        // anything that focused AND activated would flip a real setting because
        // somebody searched for it. The rail had to learn the same lesson from
        // the other end: Settings.tsx passes activateOnFocus={false} so arrow
        // keys move without selecting.
        //
        // Skipping anything inside the caption is not tidiness: the caption's own
        // (i) is tabindex=0 and comes first in document order, so the plain
        // "first focusable" would open a tooltip instead of landing on the
        // control. preventScroll because the smooth scroll above is still running
        // and focus() would restart it from wherever the row currently is.
        const control = [...marked.querySelectorAll<HTMLElement>(FOCUSABLE)].find((n) => !row.contains(n));
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
      // The card is here and the row is not - keep waiting, unless this was the
      // last look. A row can still be one fetch away.
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
