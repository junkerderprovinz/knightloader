import { useSyncExternalStore } from 'react';

/**
 * Whether the sidebar's language picker (components/LanguagePicker.tsx) has
 * its dropdown open, hoisted to module scope for the identical reason
 * appearance.ts's rainbow state is (see useRainbow.ts's own doc comment):
 * the command palette and the picker never meet in the component tree, so a
 * palette command that opens or closes the dropdown needs a store neither
 * side has to be handed a prop through several intermediate components to
 * reach, not a second, disconnected open flag.
 *
 * LanguagePicker itself reads this through useLangPickerOpen() instead of a
 * local useState - same component, same click-outside/Escape handling,
 * just driven by state that lives one level higher so something outside it
 * can drive it too.
 */
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

/** React binding - useSyncExternalStore, same reason useRainbow() uses it: read during render, not learned an effect-tick late. */
export function useLangPickerOpen(): boolean {
  return useSyncExternalStore(subscribeLangPickerOpen, langPickerOpen, () => langPickerOpen());
}
