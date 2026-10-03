// Who else watches the clipboard. Every watcher (a tab of this interface, the
// browser extension, the desktop app) renews a lease with the instance it sends
// to, and an instance lists the whole group's, so switching a watch on can name
// the devices already watching and offer to stop them.
import { apiBase, json, ok } from './api';

export type WatcherKind = 'web' | 'extension' | 'desktop';

export interface ClipboardWatcher {
  id: string;
  name: string;
  kind: WatcherKind;
  /** The instance the watcher sends its links to. */
  instance?: string;
}

/** How often a running watch renews its lease. The server keeps one for three
 *  minutes, so a missed renewal does not drop it. */
const RENEW_MS = 60_000;

const ID_KEY = 'knightloader.clipboardWatcher';

/** Where the tabs of this browser tell each other that one of them closed. */
const TABS_CHANNEL = 'knightloader.clipboardWatcher';

/** How long the other tabs wait before renewing after one closed, so its
 *  keepalive leave reaches the instance first. */
const REJOIN_MS = 1_000;

let memoryId = '';

/**
 * watcherId is this browser's watcher id. It lives in localStorage so every tab
 * of the browser renews one entry; where storage is blocked it lasts as long as
 * the page.
 */
export function watcherId(): string {
  try {
    const kept = localStorage.getItem(ID_KEY);
    if (kept) return kept;
    const id = freshId();
    localStorage.setItem(ID_KEY, id);
    return id;
  } catch {
    memoryId ||= freshId();
    return memoryId;
  }
}

function freshId(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(12));
  return 'web-' + Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
}

/**
 * deviceLabel names a browser and its system, such as "Firefox, Windows", which
 * is what a person recognises in a warning. Brave and Vivaldi report
 * themselves as Chrome.
 */
export function deviceLabel(ua: string = navigator.userAgent): string {
  const browser = /Edg\//.test(ua)
    ? 'Edge'
    : /OPR\//.test(ua)
      ? 'Opera'
      : /Firefox\//.test(ua)
        ? 'Firefox'
        : /Chrome\//.test(ua)
          ? 'Chrome'
          : /Safari\//.test(ua)
            ? 'Safari'
            : 'Browser';
  const system = /Windows/.test(ua)
    ? 'Windows'
    : /Android/.test(ua)
      ? 'Android'
      : /iPhone|iPad/.test(ua)
        ? 'iOS'
        : /Mac OS X/.test(ua)
          ? 'macOS'
          : /CrOS/.test(ua)
            ? 'ChromeOS'
            : /Linux/.test(ua)
              ? 'Linux'
              : '';
  return system ? `${browser}, ${system}` : browser;
}

const watcherPath = (id: string, instance = '') => `${apiBase(instance)}/clipboard-watchers/${encodeURIComponent(id)}`;

/** listWatchers is every clipboard watcher in the group, this browser's included. */
export async function listWatchers(): Promise<ClipboardWatcher[]> {
  return (await json<ClipboardWatcher[] | null>(await fetch('/api/clipboard-watchers'))) ?? [];
}

/** stopWatcher asks another device's watch to stop; it hears on its next renewal. */
export async function stopWatcher(id: string): Promise<void> {
  await ok(await fetch(`${watcherPath(id)}/stop`, { method: 'POST' }));
}

async function renew(instance: string): Promise<boolean> {
  const r = await fetch(watcherPath(watcherId(), instance), {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name: deviceLabel(), kind: 'web' }),
  });
  return (await json<{ stop: boolean }>(r)).stop;
}

/**
 * startLease keeps this tab's watch listed until the returned function is
 * called, which takes it off the list. The lease is held by `instance`, the
 * one the watch sends its links to ('' for this one), so that is where the
 * group finds it and where a stop waits. onStopped runs when another device
 * asked this watch to stop.
 */
export function startLease(instance: string, onStopped: () => void): () => void {
  let ended = false;
  let rejoin: ReturnType<typeof setTimeout> | undefined;
  const round = async () => {
    try {
      if ((await renew(instance)) && !ended) onStopped();
    } catch {
      // The watch keeps running; it is only missing from the list until the
      // next round gets through.
    }
  };
  // keepalive lets the request outlive a tab that is closing.
  const leave = () =>
    void fetch(watcherPath(watcherId(), instance), { method: 'DELETE', keepalive: true }).catch(() => {});

  // Every tab of the browser renews the one entry, so a tab that closes takes
  // it off the list for the others too. They put it back once its leave is
  // through.
  const tabs = new BroadcastChannel(TABS_CHANNEL);
  tabs.onmessage = () => {
    clearTimeout(rejoin);
    rejoin = setTimeout(() => void round(), REJOIN_MS);
  };
  // A closing tab never gets to run the cleanup below.
  const onHide = () => {
    leave();
    tabs.postMessage('left');
  };
  // A page restored from the back-forward cache left the list when it was
  // hidden.
  const onShow = (e: PageTransitionEvent) => {
    if (e.persisted) void round();
  };
  addEventListener('pagehide', onHide);
  addEventListener('pageshow', onShow);

  void round();
  const timer = setInterval(() => void round(), RENEW_MS);
  return () => {
    ended = true;
    clearInterval(timer);
    clearTimeout(rejoin);
    tabs.close();
    removeEventListener('pagehide', onHide);
    removeEventListener('pageshow', onShow);
    leave();
  };
}
