import { useCallback, useSyncExternalStore } from 'react';
import { WATCH_SUPPORTED } from './clipboardWatch';

/** Where this browser keeps the switch. */
const SWITCH_KEY = 'knightloader.clipboardWatch';

// Where storage is blocked the switch lasts as long as the page.
let inMemory = false;
const listeners = new Set<() => void>();

function readSwitch(): boolean {
  try {
    return localStorage.getItem(SWITCH_KEY) === 'on';
  } catch {
    return inMemory;
  }
}

function writeSwitch(on: boolean): void {
  inMemory = on;
  try {
    localStorage.setItem(SWITCH_KEY, on ? 'on' : 'off');
  } catch {
    // Kept in memory instead.
  }
  for (const fn of listeners) fn();
}

function subscribe(fn: () => void): () => void {
  // The storage event carries a flip made in another tab of this browser, so
  // a stop that tab heard ends the watch here too.
  const onStorage = (e: StorageEvent) => {
    if (e.key === SWITCH_KEY) fn();
  };
  listeners.add(fn);
  window.addEventListener('storage', onStorage);
  return () => {
    listeners.delete(fn);
    window.removeEventListener('storage', onStorage);
  };
}

/**
 * useClipboardWatch is the clipboard watch's on/off state, shared by the
 * settings card, the collector's button and GlobalIntake, which runs the
 * poller. The watch runs in one browser, so the switch is kept in that
 * browser, not in the interface state every browser of the instance shares.
 * It reads as off wherever the browser cannot read the clipboard, whatever is
 * stored.
 */
export function useClipboardWatch(): [boolean, (on: boolean) => void] {
  const stored = useSyncExternalStore(subscribe, readSwitch, () => false);
  const set = useCallback((on: boolean) => writeSwitch(WATCH_SUPPORTED && on), []);
  return [WATCH_SUPPORTED && stored, set];
}
