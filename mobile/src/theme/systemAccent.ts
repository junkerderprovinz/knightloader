import { useSyncExternalStore } from 'react';
import { AppState } from 'react-native';

import { KnightAccent } from '../../modules/system-accent';
import { argbToHex } from './accentSource';

// Material You's colour, the phone's source for the wand swatch. Android 12 and
// newer build a palette from the wallpaper, which modules/system-accent reads.
// Below 12 and on iOS there is no palette, so no swatch, and an accent set to
// the system's colour shows the instance's.

export interface Tones {
  light: string;
  dark: string;
}

let tones: Tones | undefined;
const listeners = new Set<() => void>();

function read() {
  const got = KnightAccent?.tones() ?? null;
  const next = got ? { light: argbToHex(got.light), dark: argbToHex(got.dark) } : undefined;
  if (next?.light === tones?.light && next?.dark === tones?.dark) return;
  tones = next;
  for (const fn of listeners) fn();
}

/**
 * watchSystemAccent reads the palette now, again whenever the app comes back
 * to the front and whenever Android reports a new one while it is in front,
 * and returns the function that stops.
 */
export function watchSystemAccent(): () => void {
  read();
  const back = AppState.addEventListener('change', (state) => {
    if (state === 'active') read();
  });
  const changed = KnightAccent?.addListener('onChange', read);
  return () => {
    back.remove();
    changed?.remove();
  };
}

function subscribe(fn: () => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

/** useSystemTones is Material 3's primary for each theme, or undefined where the phone has none. */
export function useSystemTones(): Tones | undefined {
  return useSyncExternalStore(subscribe, () => tones);
}
