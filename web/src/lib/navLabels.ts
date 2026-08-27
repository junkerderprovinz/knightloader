// How much of a navigation entry is drawn.
//
// One setting, two sets of tabs: the sidebar and the settings rail (jdp,
// 2026-08-27: "Man soll per horizontalem Selektor wählen können ob bei den
// Tabs (Settings und Sidebar) nur glyph, nur text oder text und glyph
// angezeigt werden soll oder glyph und text nur bei mouseover"). Somebody who
// wants glyphs wants glyphs; two switches for that would be two switches to
// keep in step.
//
// Same store shape as sidebarPrefs.ts, and here for the same reason spelled
// out there: the sidebar renders OUTSIDE the settings route's provider tree,
// so the control that changes this and the two components that obey it never
// meet in the React tree at all. A module-level value plus
// useSyncExternalStore is what lets the rail restyle itself while the pointer
// is still on the selector, rather than on the next navigation.
import { useSyncExternalStore } from 'react';

/**
 * The four ways to draw a nav entry.
 *
 * `hover` is not the collapsing rail the word suggests, and the difference is
 * the whole point of it (jdp's own description, chosen over the three
 * alternatives offered): NOTHING resizes. The tile and the sidebar row keep
 * exactly the size they have under `both`. At rest the glyph sits centred in
 * that space; on hover it moves aside - up in a settings tile, left in a
 * sidebar row - and the label appears in the room it leaves. A rail that grows
 * or overlays on hover moves the page around under the pointer; this one
 * cannot, because its geometry never changes.
 *
 * `glyph` is the only mode that does change a width: with no label ever shown,
 * the rail has nothing to be wide for.
 */
export type NavLabelMode = 'both' | 'glyph' | 'text' | 'hover';

/** Mirrors internal/settings/settings_appearance.go's own default. */
export const DEFAULT_NAV_LABELS: NavLabelMode = 'both';

let mode: NavLabelMode = DEFAULT_NAV_LABELS;

const listeners = new Set<() => void>();

/**
 * setNavLabels stores the new value and wakes the readers. Called from two
 * places, exactly like sidebarPrefs' own setter: once on boot with whatever
 * the server last saved, and again the instant the Aussehen tab's selector
 * changes it.
 */
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

/**
 * asNavLabelMode narrows whatever the server sent.
 *
 * The server sanitises this field on every write (settings_appearance.go), so
 * a bad value should be impossible - but "should be impossible" arriving from
 * a network read is exactly the value that ends up in a className, and an
 * unrecognised mode here would draw a rail with neither glyph nor label in it.
 */
export function asNavLabelMode(v: unknown): NavLabelMode {
  return v === 'glyph' || v === 'text' || v === 'hover' || v === 'both' ? v : DEFAULT_NAV_LABELS;
}
