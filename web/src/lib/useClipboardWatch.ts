import { useCallback, useSyncExternalStore } from 'react';
import { useUIState } from './uistate';
import { isDesktop } from './desktop';
import { TARGET_FIELD, WATCH_FIELD, WATCH_SUPPORTED } from './clipboardWatch';

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
 * poller. In a browser the watch runs in that browser, so the switch is kept
 * there, not in the interface state every browser of the instance shares. The
 * desktop app watches from Go, which reads the switch from the interface state
 * (desktop/clipwatch.go), so there it stays in that state. It reads as off
 * wherever the page cannot watch the clipboard, whatever is stored.
 */
export const useClipboardWatch: () => [boolean, (on: boolean) => void] = isDesktop()
  ? useSharedSwitch
  : useBrowserSwitch;

function useBrowserSwitch(): [boolean, (on: boolean) => void] {
  const stored = useSyncExternalStore(subscribe, readSwitch, () => false);
  const set = useCallback((on: boolean) => writeSwitch(WATCH_SUPPORTED && on), []);
  return [WATCH_SUPPORTED && stored, set];
}

function useSharedSwitch(): [boolean, (on: boolean) => void] {
  const [stored, setStored] = useUIState<boolean>(WATCH_FIELD, false);
  const set = useCallback((on: boolean) => setStored(WATCH_SUPPORTED && on), [setStored]);
  return [WATCH_SUPPORTED && stored, set];
}

/** useClipboardWatchTarget is where copied links go: '' for this instance,
 *  otherwise a peer's name as /api/instances lists it. */
export function useClipboardWatchTarget(): [string, (target: string) => void] {
  return useUIState<string>(TARGET_FIELD, '');
}
