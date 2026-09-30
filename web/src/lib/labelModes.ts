// How much of a control is drawn, set apart for four kinds of control, since
// the right answer differs: a sidebar reduced to glyphs narrows the page, a
// button reduced to one is only denser. A module-level store like
// sidebarPrefs.ts, because the sidebar renders outside the settings provider
// tree and has to restyle the moment a selector changes.
import { useSyncExternalStore } from 'react';
import type { Settings } from './api';

/**
 * The four ways to draw a control, in the order the settings offer them. In
 * the sidebar and the settings rail `hover` keeps the `both` geometry: the
 * glyph sits centred at rest and moves aside on hover to make room for the
 * label, so nothing resizes under the pointer. There only `glyph` changes a
 * width, since no label is ever shown.
 */
export type LabelMode = 'text' | 'both' | 'glyph' | 'hover';

export const LABEL_MODES: LabelMode[] = ['text', 'both', 'glyph', 'hover'];

/** The kinds of control with a setting each, in the order the settings card lists them. */
export type LabelAxis = 'buttons' | 'sidebar' | 'tabs' | 'bottombar';

export const LABEL_AXES: LabelAxis[] = ['buttons', 'sidebar', 'tabs', 'bottombar'];

/** Where each axis is stored in the instance's settings. */
export const LABEL_SETTING = {
  buttons: 'buttonLabels',
  sidebar: 'sidebarLabels',
  tabs: 'tabLabels',
  bottombar: 'bottomBarLabels',
} as const satisfies Record<LabelAxis, keyof Settings>;

/** Mirrors internal/settings/settings_appearance.go's own default. */
export const DEFAULT_LABEL_MODE: LabelMode = 'both';

const modes: Record<LabelAxis, LabelMode> = {
  buttons: DEFAULT_LABEL_MODE,
  sidebar: DEFAULT_LABEL_MODE,
  tabs: DEFAULT_LABEL_MODE,
  bottombar: DEFAULT_LABEL_MODE,
};

const listeners = new Set<() => void>();

/** setLabelMode stores one axis's mode and wakes its readers, when a selector changes. */
export function setLabelMode(axis: LabelAxis, next: LabelMode): void {
  if (modes[axis] === next) return;
  modes[axis] = next;
  for (const fn of listeners) fn();
}

/** seedLabelModes takes all four from the saved settings, at boot. */
export function seedLabelModes(s: Pick<Settings, (typeof LABEL_SETTING)[LabelAxis]>): void {
  for (const axis of LABEL_AXES) setLabelMode(axis, asLabelMode(s[LABEL_SETTING[axis]]));
}

function subscribe(fn: () => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

/** The axis's current mode, re-rendering the caller when it changes. */
export function useLabelMode(axis: LabelAxis): LabelMode {
  const read = () => modes[axis];
  return useSyncExternalStore(subscribe, read, read);
}

/** asLabelMode narrows a value from the server. An unknown mode would draw a
 *  control with neither glyph nor label, so it falls back to the default. */
export function asLabelMode(v: unknown): LabelMode {
  return LABEL_MODES.includes(v as LabelMode) ? (v as LabelMode) : DEFAULT_LABEL_MODE;
}
