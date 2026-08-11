import { useSyncExternalStore } from 'react';

/**
 * Whether the command palette (components/CommandPalette.tsx) is open,
 * hoisted to module scope for the identical reason lib/langPickerOpen.ts's
 * own store is: the "open command palette" entry lives in
 * lib/commands/global.ts, a command's `run` never holds a ref to the
 * component that renders the overlay it is opening, and the two meet here
 * instead of a prop passed down through however many layers separate
 * whichever surface ran the command from Layout.tsx, where the palette is
 * mounted once (the same reason CaptchaModal, IdleActionBanner and
 * OnboardingWizard are all mounted there rather than per-page).
 *
 * CommandPalette.tsx reads this through useCommandPaletteOpen() instead of a
 * local useState for the same reason LanguagePicker.tsx does: the palette
 * itself, a command's run(), and (once it lands) the keyboard dispatcher all
 * have to agree on one open flag, not three that can drift.
 */
let open = false;
const listeners = new Set<() => void>();

export function commandPaletteOpen(): boolean {
  return open;
}

export function subscribeCommandPaletteOpen(fn: () => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

function set(next: boolean): void {
  if (next === open) return;
  open = next;
  for (const fn of listeners) fn();
}

export function setCommandPaletteOpen(next: boolean): void {
  set(next);
}

export function toggleCommandPaletteOpen(): void {
  set(!open);
}

/** React binding - useSyncExternalStore, same reason useLangPickerOpen() uses it: read during render, not learned an effect-tick late. */
export function useCommandPaletteOpen(): boolean {
  return useSyncExternalStore(subscribeCommandPaletteOpen, commandPaletteOpen, () => commandPaletteOpen());
}
