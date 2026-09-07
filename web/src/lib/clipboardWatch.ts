// The clipboard watch: with it on, a link copied anywhere on the machine lands
// in the collector by itself, the way JDownloader's own clipboard observer
// works (jdp, 2026-09-07: "Zusätzlich können wir dort auch ein Toggle für die
// Zwischenablage implementieren, so dass links die man in die zwischenablage
// legt automatisch in den Linksammler gehen").
//
// Two things are worth being precise about, because both decide whether this
// feature is present at all on a given instance:
//
//  1. Reading the clipboard needs navigator.clipboard.readText, which exists
//     ONLY in a secure context - HTTPS, or localhost. KnightLoader's ordinary
//     deployment is a bare http://192.168.x.x:8749 address, where the whole
//     navigator.clipboard object is undefined. There is no fallback for
//     reading the way copyToClipboard() has one for writing: execCommand
//     ('paste') was never permitted for a page. So on a plain-HTTP LAN
//     instance this watch cannot run, WATCH_SUPPORTED is false, and the
//     switch says so instead of sitting there pretending.
//
//  2. Even where it exists, the browser gates it. Chrome grants clipboard-read
//     per origin after a prompt and refuses outright while the document is not
//     focused; Firefox does not grant it to page script at all. So every read
//     is attempted, never assumed - and a run of consecutive refusals switches
//     the watch back off rather than polling a permission the user has already
//     declined, forever, once a second.
//
// The always-available path is unaffected and is still the one most people
// use: Ctrl+V anywhere in the window, handled by GlobalIntake through
// event.clipboardData, which needs no permission and no secure context.
import { addLinks } from './api';

/**
 * Whether this browser, on this origin, can read the clipboard at all. Read
 * once at module scope: the answer is a property of the deployment (secure
 * context or not) and cannot change without a reload.
 */
export const WATCH_SUPPORTED =
  typeof navigator !== 'undefined' && !!navigator.clipboard?.readText;

/** The remembered-field name, shared by the settings card and the collector's own button. */
export const WATCH_FIELD = 'clipboardWatch';

/** How often the clipboard is re-read while the window has focus. */
const POLL_MS = 1200;

/**
 * How many refusals in a row end the watch. Not 1: a single "Document is not
 * focused" fires routinely in the gap between hasFocus() and the read
 * resolving, and switching off on that would make the feature unusable.
 */
const REFUSALS_BEFORE_GIVING_UP = 3;

/**
 * Anything that could be a link. Deliberately loose - the server does the real
 * parsing and answers with what it accepted - but not empty: without this every
 * copied word would be a POST, and every one of them a toast.
 */
const LOOKS_LIKE_A_LINK = /(^|\s)(https?:\/\/|magnet:\?|ftp:\/\/)\S+/i;

export type WatchOutcome =
  | { kind: 'staged'; n: number }
  | { kind: 'none' }
  | { kind: 'denied'; reason: string }
  | { kind: 'failed'; reason: string };

/**
 * startClipboardWatch polls the clipboard until the returned function is
 * called. `onOutcome` is called only when something actually happened - a
 * staged batch, or the watch giving up - never once per quiet poll.
 *
 * The seed matters: the first read is compared against the clipboard's content
 * at the moment the watch was switched on, so turning it on does not
 * immediately import whatever happened to be copied an hour ago. Only what is
 * copied FROM NOW is picked up.
 */
export function startClipboardWatch(onOutcome: (o: WatchOutcome) => void): () => void {
  if (!WATCH_SUPPORTED) return () => {};

  let stopped = false;
  let last: string | null = null;
  let seeded = false;
  let refusals = 0;
  let timer: ReturnType<typeof setTimeout> | undefined;

  async function tick() {
    if (stopped) return;
    // Not merely an optimisation: Chrome rejects readText() outright while the
    // document is unfocused, so polling a background tab would only ever
    // produce refusals and then switch the watch off for no reason.
    if (!document.hasFocus()) return schedule();

    let text: string;
    try {
      text = await navigator.clipboard.readText();
      refusals = 0;
    } catch (e) {
      refusals++;
      if (refusals >= REFUSALS_BEFORE_GIVING_UP) {
        stopped = true;
        onOutcome({ kind: 'denied', reason: e instanceof Error ? e.message : String(e) });
        return;
      }
      return schedule();
    }

    const trimmed = text.trim();
    if (!seeded) {
      seeded = true;
      last = trimmed;
      return schedule();
    }
    if (trimmed === last) return schedule();
    last = trimmed;

    if (!trimmed || !LOOKS_LIKE_A_LINK.test(trimmed)) return schedule();

    try {
      const created = await addLinks(trimmed, '');
      // A copied link that was already in the collector answers with an empty
      // list. That is the ordinary case when somebody copies the same link
      // twice, so it is reported as 'none' and not as a failure.
      onOutcome(created.length ? { kind: 'staged', n: created.length } : { kind: 'none' });
    } catch (e) {
      onOutcome({ kind: 'failed', reason: e instanceof Error ? e.message : String(e) });
    }
    schedule();
  }

  function schedule() {
    if (stopped) return;
    timer = setTimeout(() => void tick(), POLL_MS);
  }

  schedule();
  return () => {
    stopped = true;
    if (timer !== undefined) clearTimeout(timer);
  };
}
