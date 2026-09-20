import { useSyncExternalStore } from 'react';

// Whether the sidebar's language dropdown is open. Module-level so palette
// commands can open and close it; the picker and the palette never meet in
// the component tree.
let open = false;
const listeners = new Set<() => void>();

export function langPickerOpen(): boolean {
  return open;
}

export function subscribeLangPickerOpen(fn: () => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

function set(next: boolean): void {
  if (next === open) return;
  open = next;
  for (const fn of listeners) fn();
}

export function setLangPickerOpen(next: boolean): void {
  set(next);
}

export function toggleLangPickerOpen(): void {
  set(!open);
}

/** The React binding, read during render rather than an effect later. */
export function useLangPickerOpen(): boolean {
  return useSyncExternalStore(subscribeLangPickerOpen, langPickerOpen, () => langPickerOpen());
}
