import { createContext, createElement, useContext, useSyncExternalStore, type ReactNode } from 'react';
import { rainbowState, subscribeRainbow, type RainbowState } from './appearance';

// A hue reaches an element as an inline style computed during render, so an
// element only recolours when its component renders again. One subscription
// sits above the routes and hands the state down through a context, which
// wakes every reader wherever it is in the tree; a subscription in the layout
// alone re-renders the layout and never reaches the page inside its outlet.
const RainbowContext = createContext<RainbowState>(rainbowState());

/**
 * RainbowProvider holds the one subscription to the colour engine. It wraps
 * the whole app in router.tsx. useSyncExternalStore reads the palette during
 * render, so an edited swatch never shows the old colour for a frame.
 */
export function RainbowProvider({ children }: { children: ReactNode }) {
  const state = useSyncExternalStore(subscribeRainbow, rainbowState, rainbowState);
  return createElement(RainbowContext.Provider, { value: state }, children);
}

/**
 * useRainbow is the React binding for the rainbow state, kept out of
 * appearance.ts so that file stays framework-free. Every component that paints
 * a palette position calls it, whether or not it reads the value, so it renders
 * again when the palette moves.
 */
export function useRainbow(): RainbowState {
  return useContext(RainbowContext);
}
