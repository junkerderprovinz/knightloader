// Whether the Konten item is shown in the sidebar is a property of the
// document, not of the settings route: the toggle lives on the Accounts
// settings tab, but the sidebar renders outside SettingsProvider's tree (it
// wraps the whole app, settings is one route within it) and cannot read that
// page's draft. Same shape as appearance.ts's rainbow store, for the same
// reason - two components that never meet in the tree, agreeing on one flag.
import { useSyncExternalStore } from 'react';

let hideAccounts = false;
const listeners = new Set<() => void>();

/** hideAccountsState is the current snapshot. Stable identity between changes. */
export function hideAccountsState(): boolean {
  return hideAccounts;
}

/**
 * setHideAccounts stores the new value and wakes the readers. Called from two
 * places: once on boot with whatever the server last saved, and again the
 * instant the Accounts tab's own toggle changes it - the second call is what
 * makes the sidebar item appear/disappear live, without the user having to
 * navigate away first (jdp, 2026-08-23: "Kontentab in der sidebar wird nicht
 * live ein und ausgeblendet").
 */
export function setHideAccounts(next: boolean): void {
  if (hideAccounts === next) return;
  hideAccounts = next;
  for (const fn of listeners) fn();
}

function subscribe(fn: () => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

export function useHideAccounts(): boolean {
  return useSyncExternalStore(subscribe, hideAccountsState, () => hideAccountsState());
}
