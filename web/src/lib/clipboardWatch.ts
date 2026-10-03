// The clipboard watch: while on, a link copied anywhere on the machine lands
// in the collector, like JDownloader's clipboard observer. Only the links are
// sent, to this instance or to the peer the target names.
//
// Reading needs navigator.clipboard.readText, which exists only in a secure
// context, and there is no fallback for reading. On the usual plain-HTTP LAN
// install the browser cannot watch and the switch explains why. Where it does
// exist, Chrome asks for permission and refuses while the document is
// unfocused, and Firefox refuses page scripts, so repeated refusals turn the
// watch off.
//
// The desktop app watches from Go instead (desktop/clipwatch.go), also while
// its window is hidden, and the page there only shows what it reports.
//
// Ctrl+V in the window works everywhere, through GlobalIntake.
import { addLinks, apiBase } from './api';
import { isDesktop } from './desktop';

/** Whether this page can watch the clipboard at all. It cannot change
 *  without a reload. */
export const WATCH_SUPPORTED =
  typeof window !== 'undefined' && (isDesktop() || !!navigator.clipboard?.readText);

/** The interface-state fields desktop/clipwatch.go reads: the switch, which
 *  only the desktop app keeps there (see useClipboardWatch), and the target. */
export const WATCH_FIELD = 'clipboardWatch';
export const TARGET_FIELD = 'clipboardWatchTarget';

/** How often the clipboard is re-read while the window has focus. */
const POLL_MS = 1200;

/** Refusals in a row that end the watch. More than one, because "Document is
 *  not focused" happens routinely between hasFocus() and the read. */
const REFUSALS_BEFORE_GIVING_UP = 3;

/** A link is one of these schemes and more, at the start of the text or after
 *  white space. Loose, since the server does the parsing, but it keeps every
 *  copied word from becoming a request. The browser extension's watch uses the
 *  same rule (extension/src/clipwatch.js), and extension/check-clipwatch.mjs
 *  keeps the two alike. */
const LOOKS_LIKE_A_LINK = /(^|\s)(https?:\/\/|magnet:\?|ftp:\/\/)\S+/i;

/**
 * clipboardLinks returns the links of a copied text, which is all the watch
 * sends. clipboardWatch.cases.json holds desktop/clipwatch.go to the same
 * rule.
 */
export function clipboardLinks(text: string): string[] {
  const every = new RegExp(LOOKS_LIKE_A_LINK.source, 'gi');
  return Array.from(text.matchAll(every), (m) => m[0].trim());
}

export type WatchOutcome =
  | { kind: 'staged'; n: number }
  | { kind: 'none' }
  | { kind: 'denied'; reason: string }
  | { kind: 'failed'; reason: string }
  /** Only the desktop app on a Wayland session without wl-paste's watch
   *  reports it: the clipboard is visible there only while the window has
   *  focus. */
  | { kind: 'limited' }
  /** Only the desktop app reports it: another device switched its watch off. */
  | { kind: 'stopped' };

/**
 * startClipboardWatch polls the clipboard until the returned function is
 * called, reporting only when something happened. The first read is only
 * remembered, so switching the watch on does not import what was copied an
 * hour ago. `target` is '' for this instance, otherwise a peer's name.
 */
export function startClipboardWatch(target: string, onOutcome: (o: WatchOutcome) => void): () => void {
  if (!navigator.clipboard?.readText) return () => {};

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

    const links = clipboardLinks(trimmed);
    if (links.length === 0) return schedule();

    try {
      const created = await addLinks(links.join('\n'), '', apiBase(target));
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
