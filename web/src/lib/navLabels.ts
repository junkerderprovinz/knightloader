// How much of a navigation entry is drawn, one setting for both the sidebar
// and the settings rail, and a second one for the phone's bottom bar. A
// module-level store like sidebarPrefs.ts, because the sidebar renders outside
// the settings provider tree and has to restyle the moment the selector
// changes.
import { useSyncExternalStore } from 'react';

/**
 * The four ways to draw a nav entry. `hover` keeps the `both` geometry: the
 * glyph sits centred at rest and moves aside on hover to make room for the
 * label, so nothing resizes under the pointer. Only `glyph` changes a width,
 * since no label is ever shown.
 */
export type NavLabelMode = 'both' | 'glyph' | 'text' | 'hover';

/**
 * The bottom bar's setting: one of the four modes, or `follow`, which draws it
 * the way the sidebar is drawn (settings.BottomBarFollowsNav says why the bar
 * has its own).
 */
export type BarLabelMode = NavLabelMode | 'follow';

/** Mirrors internal/settings/settings_appearance.go's own default. */
export const DEFAULT_NAV_LABELS: NavLabelMode = 'both';

/** Mirrors settings.BottomBarFollowsNav, the Go default. */
export const DEFAULT_BAR_LABELS: BarLabelMode = 'follow';

let mode: NavLabelMode = DEFAULT_NAV_LABELS;
let barMode: BarLabelMode = DEFAULT_BAR_LABELS;

const listeners = new Set<() => void>();

function wake(): void {
  for (const fn of listeners) fn();
}

/** setNavLabels stores the mode and wakes the readers: at boot with the saved
 *  value, and when the selector changes. */
export function setNavLabels(next: NavLabelMode): void {
  if (mode === next) return;
  mode = next;
  wake();
}

/** setBarLabels is setNavLabels for the bottom bar's own setting. */
export function setBarLabels(next: BarLabelMode): void {
  if (barMode === next) return;
  barMode = next;
  wake();
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

/** The bottom bar's setting as chosen, `follow` included, for its selector. */
export function useBarLabelSetting(): BarLabelMode {
  const read = () => barMode;
  return useSyncExternalStore(subscribe, read, read);
}

/** How the bottom bar draws its entries: its own mode, or the sidebar's. */
export function useBarLabels(): NavLabelMode {
  const read = () => (barMode === 'follow' ? mode : barMode);
  return useSyncExternalStore(subscribe, read, read);
}

/** asNavLabelMode narrows a value from the server. An unknown mode would draw
 *  a rail with neither glyph nor label, so it falls back to the default. */
export function asNavLabelMode(v: unknown): NavLabelMode {
  return v === 'glyph' || v === 'text' || v === 'hover' || v === 'both' ? v : DEFAULT_NAV_LABELS;
}

/** asBarLabelMode narrows the bar's setting the same way. */
export function asBarLabelMode(v: unknown): BarLabelMode {
  return v === 'follow' || v === 'glyph' || v === 'text' || v === 'hover' || v === 'both' ? v : DEFAULT_BAR_LABELS;
}
