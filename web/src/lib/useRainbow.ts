import { useSyncExternalStore } from 'react';
import { rainbowState, subscribeRainbow, type RainbowState } from './appearance';

/**
 * useRainbow is the React binding for the document-level rainbow state. It is
 * separate from appearance.ts so that file stays framework-free for a sibling
 * app to copy.
 *
 * useSyncExternalStore rather than an effect + local state: the palette is read
 * during render to colour a row, so a component that learns about a change one
 * paint late would show the previous colour for a frame every time the user
 * edits a swatch.
 */
export function useRainbow(): RainbowState {
  return useSyncExternalStore(subscribeRainbow, rainbowState, () => rainbowState());
}
