// How much of a navigation entry is drawn, one setting for both the sidebar
// and the settings rail. A module-level store like sidebarPrefs.ts, because
// the sidebar renders outside the settings provider tree and has to restyle
// the moment the selector changes.
import { useSyncExternalStore } from 'react';

/**
 * The four ways to draw a nav entry. `hover` keeps the `both` geometry: the
 * glyph sits centred at rest and moves aside on hover to make room for the
 * label, so nothing resizes under the pointer. Only `glyph` changes a width,
 * since no label is ever shown.
 */
export type NavLabelMode = 'both' | 'glyph' | 'text' | 'hover';

/** Mirrors internal/settings/settings_appearance.go's own default. */
export const DEFAULT_NAV_LABELS: NavLabelMode = 'both';

let mode: NavLabelMode = DEFAULT_NAV_LABELS;

const listeners = new Set<() => void>();

/** setNavLabels stores the mode and wakes the readers: at boot with the saved
 *  value, and when the selector changes. */
export function setNavLabels(next: NavLabelMode): void {
  if (mode === next) return;
  mode = next;
  for (const fn of listeners) fn();
}

function subscribe(fn: () => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

/** The current mode, re-rendering the caller when it changes. */
export function useNavLabels(): NavLabelMode {
  const read = () => mode;
  return useSyncExternalStore(subscribe, read, read);
}

/** asNavLabelMode narrows a value from the server. An unknown mode would draw
 *  a rail with neither glyph nor label, so it falls back to the default. */
export function asNavLabelMode(v: unknown): NavLabelMode {
  return v === 'glyph' || v === 'text' || v === 'hover' || v === 'both' ? v : DEFAULT_NAV_LABELS;
}
