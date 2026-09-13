// Which way the settings column is being scrolled, and therefore whether the
// search bar should be on screen.
//
// THE PROBLEM. The bar is `sticky top-0` over a scrolling column, and it has to
// be opaque: the column behind it is `overflow-y-auto`, so a transparent strip
// would show cards sliding through the text. Opaque plus sticky means every card
// that scrolls past disappears under it, and on a settings page the thing
// directly under the bar is the first card's title - the one line that says what
// you are looking at. jdp's words: it covers the top card everywhere.
//
// Filtering the alternatives, because the obvious two are both worse:
//
//   Make it static      then it scrolls away and a search over twenty-four pages
//                       needs a trip back to the top before it can be used.
//   Shrink it           a half-height bar covers half a card instead of a whole
//                       one. It changes how much is wrong, not whether.
//
// So it hides while you read downward and comes back the moment you head back
// up, which is where you were going anyway when you reached for it.
//
// WHAT IT WILL NOT DO, and this is the part worth keeping: it never hides a bar
// somebody is using. Typing, focus, an open result list - any of those pins it.
// A search field that slid away mid-query would be a far louder bug than the one
// this fixes, and it would be intermittent, which is worse again.
import { useEffect, useRef, useState } from 'react';

/**
 * How far the column must move before the direction is believed.
 *
 * Without it, the bar flickers: a trackpad and a touch screen both emit long
 * tails of one- and two-pixel events as they settle, and their signs alternate.
 * Six pixels is under one wheel notch, so a real scroll still answers on the
 * first event of the gesture.
 */
const DELTA = 6;

/**
 * Reads the settings column's scroll direction.
 *
 * @param ref     the sticky bar itself. Its own height is the zone at the top of
 *                the column where the bar is still partly in normal flow, and
 *                hiding it there would animate it away from a place it is not
 *                covering anything in.
 * @param pinned  caller's veto: true while the bar is in use.
 * @returns       true when the bar should be off screen.
 */
export function useStickyReveal(ref: React.RefObject<HTMLElement | null>, pinned: boolean): boolean {
  const [hidden, setHidden] = useState(false);
  const last = useRef(0);

  useEffect(() => {
    if (pinned) {
      setHidden(false);
      return;
    }
    // The column, found through the attribute settings/jump.ts already relies on
    // rather than through a parent-of-a-parent walk that any layout change
    // silently breaks. No column means no scrolling, which means nothing to do:
    // this component also renders in tests and in Storybook-shaped harnesses
    // where that wrapper does not exist.
    const el = ref.current?.closest<HTMLElement>('[data-settings-content]');
    if (!el) return;

    last.current = el.scrollTop;
    const onScroll = () => {
      const y = el.scrollTop;
      const dy = y - last.current;
      last.current = y;
      // Near the top the bar is still in the document's flow, sitting ABOVE the
      // first card rather than over it, so there is nothing to get out of the
      // way of. Measured live because the padding differs between breakpoints
      // (pt-6 under md, pt-8 above it) and a constant here would be wrong on one
      // of them.
      if (y <= (ref.current?.offsetHeight ?? 0)) {
        setHidden(false);
        return;
      }
      if (Math.abs(dy) < DELTA) return;
      setHidden(dy > 0);
    };

    el.addEventListener('scroll', onScroll, { passive: true });
    return () => el.removeEventListener('scroll', onScroll);
  }, [ref, pinned]);

  return hidden && !pinned;
}
