import { useSyncExternalStore } from 'react';

// Whether the command palette is open. Module-level so a command's run() and
// the palette, mounted once in Layout.tsx, share one flag without a prop chain.
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

/** The React binding, read during render rather than an effect later. */
export function useCommandPaletteOpen(): boolean {
  return useSyncExternalStore(subscribeCommandPaletteOpen, commandPaletteOpen, () => commandPaletteOpen());
}
