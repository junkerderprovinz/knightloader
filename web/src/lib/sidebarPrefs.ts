// Which optional sidebar items are hidden. A module-level store like the
// rainbow state in appearance.ts: the toggles live on settings tabs, but the
// sidebar renders outside SettingsProvider and cannot read their draft.
import { useSyncExternalStore } from 'react';

/** The sidebar items that can be switched off. */
export type HidableNavItem = 'accounts' | 'instances';

const hidden: Record<HidableNavItem, boolean> = {
  accounts: false,
  instances: false,
};

const listeners = new Set<() => void>();

/** setHidden stores the flag and wakes the readers: at boot with the saved
 *  value, and when the toggle changes, so the item shows or hides at once. */
export function setHidden(item: HidableNavItem, next: boolean): void {
  if (hidden[item] === next) return;
  hidden[item] = next;
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
