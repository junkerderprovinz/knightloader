import { useSyncExternalStore } from 'react';
import { rainbowState, subscribeRainbow, type RainbowState } from './appearance';

/**
 * useRainbow is the React binding for the rainbow state, kept out of
 * appearance.ts so that file stays framework-free. useSyncExternalStore reads
 * the palette during render, so an edited swatch never shows the old colour
 * for a frame.
 */
export function useRainbow(): RainbowState {
  return useSyncExternalStore(subscribeRainbow, rainbowState, () => rainbowState());
}
