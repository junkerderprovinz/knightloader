// Which optional items the sidebar shows is a property of the document, not
// of the settings route: the toggles live on their own settings tabs, but the
// sidebar renders outside SettingsProvider's tree (it wraps the whole app,
// settings is one route within it) and cannot read those pages' draft. Same
// shape as appearance.ts's rainbow store, for the same reason - two
// components that never meet in the tree, agreeing on one flag.
//
// Two flags now (jdp, 2026-08-27 asked for the Instanzen item to hide the way
// Konten already could), kept in one keyed record rather than as a second
// copy of every function below: the pair is identical in every respect except
// the word, and two hand-mirrored stores are two places for the next one to
// be forgotten.
import { useSyncExternalStore } from 'react';

/** The sidebar items that can be switched off. */
export type HidableNavItem = 'accounts' | 'instances';

const hidden: Record<HidableNavItem, boolean> = {
  accounts: false,
  instances: false,
};

const listeners = new Set<() => void>();

/**
 * setHidden stores the new value and wakes the readers. Called from two
 * places per item: once on boot with whatever the server last saved, and
 * again the instant that tab's own toggle changes it - the second call is
 * what makes the sidebar item appear/disappear live, without the user having
 * to navigate away first (jdp, 2026-08-23: "Kontentab in der sidebar wird
 * nicht live ein und ausgeblendet").
 */
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
