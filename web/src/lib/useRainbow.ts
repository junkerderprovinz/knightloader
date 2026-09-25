import { createContext, createElement, useContext, useSyncExternalStore, type ReactNode } from 'react';
import { rainbowState, subscribeRainbow, type RainbowState } from './appearance';

// A hued element needs no render to follow the palette: hueVars() points it at
// the root's --rb-N properties, which applyRainbow and disco's walk write. What
// still needs the state is code that decides something from it, such as
// whether a chart's bands take the palette at all. One subscription sits above
// the routes and hands the state down through a context, so every such reader
// wakes wherever it is in the tree.
const RainbowContext = createContext<RainbowState>(rainbowState());

/**
 * RainbowProvider holds the one subscription to the colour engine. It wraps
 * the whole app in router.tsx.
 */
export function RainbowProvider({ children }: { children: ReactNode }) {
  const state = useSyncExternalStore(subscribeRainbow, rainbowState, rainbowState);
  return createElement(RainbowContext.Provider, { value: state }, children);
}

/**
 * useRainbow is the React binding for the rainbow state, kept out of
 * appearance.ts so that file stays framework-free.
 */
export function useRainbow(): RainbowState {
  return useContext(RainbowContext);
}
