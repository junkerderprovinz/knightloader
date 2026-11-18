// How hard the download and collector lists shade their rows: each package a
// band, its header a step above its links, neighbouring headers in two shades
// and the links alternating inside it. Kept per browser like the row height,
// since the screen decides how hard rows are to tell apart, and in a
// module-level store because the lists render outside the settings page that
// changes it.
import { useSyncExternalStore } from 'react';

export type ListShade = 'off' | 'moderate' | 'strong';

export const LIST_SHADES: ListShade[] = ['off', 'moderate', 'strong'];

const CACHE = 'kl-list-shade';

function read(): ListShade {
  try {
    const kept = localStorage.getItem(CACHE);
    return kept === 'off' || kept === 'strong' ? kept : 'moderate';
  } catch {
    return 'moderate';
  }
}

let current: ListShade | undefined;
const listeners = new Set<() => void>();

/** subscribeListShade calls `fn` on every change and returns what stops it. */
export function subscribeListShade(fn: () => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

/** listShade is the level in force, moderate until this browser picks another. */
export function listShade(): ListShade {
  current ??= read();
  return current;
}

/** setListShade stores the level in this browser and redraws every list. */
export function setListShade(level: ListShade): void {
  current = level;
  try {
    if (level === 'moderate') localStorage.removeItem(CACHE);
    else localStorage.setItem(CACHE, level);
  } catch {
    // Storage switched off: the choice holds until the page is reloaded.
  }
  for (const fn of listeners) fn();
}

/** The level in force, re-rendering the caller when it changes. */
export function useListShade(): ListShade {
  return useSyncExternalStore(subscribeListShade, listShade, listShade);
}
