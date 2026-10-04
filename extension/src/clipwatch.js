// The clipboard watch: while it is on, a link copied anywhere on the computer
// goes to the default instance, as the web interface's watch does from its own
// tab (web/src/lib/clipboardWatch.ts). Only the links in a copied text leave
// the browser; anything else that is copied stays here.
//
// Chromium reads the clipboard in an offscreen document (offscreen.js), since a
// service worker has no document. Firefox reads it in the background page and
// unloads that page when it is idle, so an alarm wakes it every minute
// (background.js).
//
// Every watcher in the group holds a lease with the instance it sends to
// (internal/clipwatch), so switching a watch on somewhere can name the others.

/** The web interface's rule (LOOKS_LIKE_A_LINK), kept alike by check-clipwatch.mjs. */
const CLIP_LINK_RULE = /(^|\s)(https?:\/\/|magnet:\?|ftp:\/\/)\S+/i;

/** How often the clipboard is read, the web interface's interval. */
const CLIP_POLL_MS = 1200;

/** Failed reads in a row that end the watch, as in the web interface. */
const CLIP_REFUSALS_BEFORE_GIVING_UP = 3;

/** clipLinks returns the links in text, one per line, or '' when it has none. */
function clipLinks(text) {
  const every = new RegExp(CLIP_LINK_RULE.source, 'gi');
  return Array.from(String(text ?? '').matchAll(every), (m) => m[0].trim()).join('\n');
}

/**
 * clipHash stands for a clipboard text, so the last one read can be remembered
 * across a Firefox background restart without keeping the text itself.
 */
function clipHash(text) {
  let h = 0x811c9dc5;
  for (let i = 0; i < text.length; i++) {
    h ^= text.charCodeAt(i);
    h = Math.imul(h, 0x01000193) >>> 0;
  }
  return h.toString(16);
}

/**
 * readClipboard returns the text on the clipboard. Pasting into a textarea
 * works in a document without focus once clipboardRead is granted, which
 * readText does not.
 */
async function readClipboard() {
  const area = document.createElement('textarea');
  document.body.append(area);
  area.focus();
  const pasted = document.execCommand('paste');
  const text = area.value;
  area.remove();
  if (pasted) return text;
  return navigator.clipboard.readText();
}

/**
 * startClipPoller reads the clipboard until the returned function is called.
 * A text that differs from the one read before and holds links hands only the
 * links to onLinks. `remembered` is the clipHash of the last text read, or null
 * when nothing was read yet, in which case whatever is on the clipboard now
 * counts as old: switching the watch on does not send what was copied an hour
 * ago. onSeen gets the hash of every new text, onRefused fires once when reads
 * keep failing, and the poller stops then.
 */
function startClipPoller({ remembered, onLinks, onSeen, onRefused, read = readClipboard }) {
  let last = remembered;
  let refusals = 0;
  let stopped = false;
  let timer;

  const tick = async () => {
    if (stopped) return;
    let text;
    try {
      text = String((await read()) ?? '').trim();
      refusals = 0;
    } catch {
      if (++refusals >= CLIP_REFUSALS_BEFORE_GIVING_UP) {
        stopped = true;
        onRefused();
        return;
      }
      return schedule();
    }
    const hash = clipHash(text);
    if (hash !== last) {
      const first = last === null;
      last = hash;
      onSeen(hash);
      const links = first ? '' : clipLinks(text);
      if (links) onLinks(links);
    }
    schedule();
  };

  const schedule = () => {
    if (!stopped) timer = setTimeout(() => void tick(), CLIP_POLL_MS);
  };

  void tick();
  return () => {
    stopped = true;
    clearTimeout(timer);
  };
}

/**
 * clipState is the switch and whether the browser granted clipboardRead. The
 * watch runs only with both.
 */
async function clipState() {
  const { clipWatch } = await chrome.storage.local.get('clipWatch');
  const wanted = clipWatch === true;
  let granted = false;
  try {
    granted = await chrome.permissions.contains({ permissions: ['clipboardRead'] });
  } catch {
    granted = false;
  }
  return { wanted, on: wanted && granted };
}

/** clipWatcherId is this browser's id on the group's list of watchers. */
async function clipWatcherId() {
  const { clipWatcherId: kept } = await chrome.storage.local.get('clipWatcherId');
  if (typeof kept === 'string' && kept) return kept;
  const bytes = crypto.getRandomValues(new Uint8Array(12));
  const id = 'ext-' + Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
  await chrome.storage.local.set({ clipWatcherId: id });
  return id;
}

/**
 * clipDeviceLabel names this browser and its system, such as "Firefox,
 * Windows", the way web/src/lib/clipboardWatchers.ts does. Brave and Vivaldi
 * report themselves as Chrome.
 */
function clipDeviceLabel(ua = navigator.userAgent) {
  const browser = /Edg\//.test(ua)
    ? 'Edge'
    : /OPR\//.test(ua)
      ? 'Opera'
      : /Firefox\//.test(ua)
        ? 'Firefox'
        : /Chrome\//.test(ua)
          ? 'Chrome'
          : 'Browser';
  const system = /Windows/.test(ua)
    ? 'Windows'
    : /Android/.test(ua)
      ? 'Android'
      : /Mac OS X/.test(ua)
        ? 'macOS'
        : /CrOS/.test(ua)
          ? 'ChromeOS'
          : /Linux/.test(ua)
            ? 'Linux'
            : '';
  return system ? `${browser}, ${system}` : browser;
}

/** The path of one watcher's lease on an instance. */
function clipWatcherPath(id) {
  return `/api/clipboard-watchers/${encodeURIComponent(id)}`;
}
