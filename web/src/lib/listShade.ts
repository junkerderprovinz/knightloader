// Whether the download and collector lists shade their rows: each package a
// band, its header a step above its links, and the links alternating faintly
// inside it. Kept per browser like the row height, since the screen decides
// how hard rows are to tell apart, and in a module-level store because the
// lists render outside the settings page that changes it.
import { useSyncExternalStore } from 'react';

const CACHE = 'kl-list-shade';

function read(): boolean {
  try {
    return localStorage.getItem(CACHE) !== 'off';
  } catch {
    return true;
  }
}

let current: boolean | undefined;
const listeners = new Set<() => void>();

/** subscribeListShade calls `fn` on every change and returns what stops it. */
export function subscribeListShade(fn: () => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

/** listShaded is the choice in force, on until this browser turns it off. */
export function listShaded(): boolean {
  current ??= read();
  return current;
}

/** setListShade stores the choice in this browser and redraws every list. */
export function setListShade(on: boolean): void {
  current = on;
  try {
    if (on) localStorage.removeItem(CACHE);
    else localStorage.setItem(CACHE, 'off');
  } catch {
    // Storage switched off: the choice holds until the page is reloaded.
  }
  for (const fn of listeners) fn();
}

/** The choice in force, re-rendering the caller when it changes. */
export function useListShade(): boolean {
  return useSyncExternalStore(subscribeListShade, listShaded, listShaded);
}
