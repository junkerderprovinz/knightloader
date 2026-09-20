// The clipboard watch: while on, a link copied anywhere on the machine lands
// in the collector, like JDownloader's clipboard observer.
//
// Reading needs navigator.clipboard.readText, which exists only in a secure
// context, and there is no fallback for reading. On the usual plain-HTTP LAN
// install WATCH_SUPPORTED is false and the switch explains why. Where it does
// exist, Chrome asks for permission and refuses while the document is
// unfocused, and Firefox refuses page scripts, so repeated refusals turn the
// watch off.
//
// Ctrl+V in the window works everywhere, through GlobalIntake.
import { addLinks } from './api';

/** Whether this origin can read the clipboard at all. It cannot change
 *  without a reload. */
export const WATCH_SUPPORTED =
  typeof navigator !== 'undefined' && !!navigator.clipboard?.readText;

/** The remembered-field name, shared by the settings card and the collector's own button. */
export const WATCH_FIELD = 'clipboardWatch';

/** How often the clipboard is re-read while the window has focus. */
const POLL_MS = 1200;

/** Refusals in a row that end the watch. More than one, because "Document is
 *  not focused" happens routinely between hasFocus() and the read. */
const REFUSALS_BEFORE_GIVING_UP = 3;

/** Anything that could be a link. Loose, since the server does the parsing,
 *  but it keeps every copied word from becoming a request. */
const LOOKS_LIKE_A_LINK = /(^|\s)(https?:\/\/|magnet:\?|ftp:\/\/)\S+/i;

export type WatchOutcome =
  | { kind: 'staged'; n: number }
  | { kind: 'none' }
  | { kind: 'denied'; reason: string }
  | { kind: 'failed'; reason: string };

/**
 * startClipboardWatch polls the clipboard until the returned function is
 * called, reporting only when something happened. The first read is only
 * remembered, so switching the watch on does not import what was copied an
 * hour ago.
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
    // Chrome refuses readText() while unfocused, and those refusals would end
    // the watch.
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
      // A link already in the collector answers with an empty list; not a failure.
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
