// Which optional sidebar items are hidden. A module-level store like the
// rainbow state in appearance.ts: the toggles live on settings tabs, but the
// sidebar renders outside SettingsProvider and cannot read their draft.
import { useSyncExternalStore } from 'react';

/** The sidebar items that can be switched off. */
export type HidableNavItem = 'accounts' | 'instances' | 'overview';

// The overview flag also decides where the interface opens, and a reload
// would otherwise show the overview for the moment the settings take to load.
const overviewKey = 'kl.hideOverview';

function cachedOverview(): boolean {
  try {
    return localStorage.getItem(overviewKey) === '1';
  } catch {
    return false;
  }
}

const hidden: Record<HidableNavItem, boolean> = {
  accounts: false,
  instances: false,
  overview: cachedOverview(),
};

const listeners = new Set<() => void>();

/** setHidden stores the flag and wakes the readers: at boot with the saved
 *  value, and when the toggle changes, so the item shows or hides at once. */
export function setHidden(item: HidableNavItem, next: boolean): void {
  if (hidden[item] === next) return;
  hidden[item] = next;
  if (item === 'overview') {
    try {
      localStorage.setItem(overviewKey, next ? '1' : '0');
    } catch {
      // Without storage the reload shows the overview until the settings arrive.
    }
  }
  for (const fn of listeners) fn();
}

function subscribe(fn: () => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

/** Whether this item is currently hidden from the sidebar. */
export function useHidden(item: HidableNavItem): boolean {
  const read = () => hidden[item];
  return useSyncExternalStore(subscribe, read, read);
}
