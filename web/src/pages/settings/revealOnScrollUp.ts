// The settings search bar stays out of sight until somebody at the top of a
// settings page pushes upward once more, and changing settings page puts it
// away again. An upward scroll in the middle of a page is somebody reading
// back, not asking for the search.
//
// At the top of the column there is nothing left to scroll and the browser
// fires no scroll event, so the push past the top is read from the wheel, and
// from a finger dragging the page down. Chromium scrolls before the wheel event
// arrives, so the tick that carries the column to its top would look like a
// push past it too: the column therefore has to have rested at the top for a
// moment before a push counts.
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';

/** How much movement counts as a direction rather than as settling noise. */
const DELTA = 6;

/** The same for a finger, whose first pixels are a press finding its place. */
const TOUCH_DELTA = 12;

/** How long the column has to have rested at its top before a push past it counts. */
export const REST_MS = 250;

/**
 * @param resetKey  changes when the settings page does; the bar goes away.
 * @param pinned    caller's veto: true while the bar is in use, so typing in it
 *                  cannot make it vanish under the hand using it.
 */
export function useRevealOnScrollUp(resetKey: string, pinned: boolean) {
  const [revealed, setRevealed] = useState(false);
  const barRef = useRef<HTMLDivElement>(null);
  const lastTop = useRef(0);
  /** Set while a reveal is waiting to be paid for by the scroll compensation. */
  const owed = useRef(false);

  useEffect(() => {
    setRevealed(false);
  }, [resetKey]);

  const column = useCallback(
    () => document.querySelector<HTMLElement>('[data-settings-content]'),
    [],
  );

  useEffect(() => {
    if (pinned) return;
    const el = column();
    if (!el) return;

    lastTop.current = el.scrollTop;
    // Since when the column has rested at its top; null while it is scrolled.
    let topSince: number | null = el.scrollTop <= 0 ? performance.now() : null;
    let touchY: number | null = null;

    const push = (up: boolean) => {
      if (!up) setRevealed(false);
      else if (topSince !== null && performance.now() - topSince >= REST_MS) setRevealed(true);
    };

    const onWheel = (e: WheelEvent) => {
      if (Math.abs(e.deltaY) > DELTA) push(e.deltaY < 0);
    };
    // Scroll still catches a scrollbar drag and Page Down, which fire no wheel
    // event, and it is where the rest at the top starts and ends.
    const onScroll = () => {
      const y = el.scrollTop;
      const dy = y - lastTop.current;
      lastTop.current = y;
      if (y > 0) topSince = null;
      else topSince ??= performance.now();
      if (dy > DELTA) setRevealed(false);
    };
    const onTouchStart = (e: TouchEvent) => {
      touchY = e.touches[0]?.clientY ?? null;
    };
    // A finger moving down the screen drags the page down, which is the push
    // upward.
    const onTouchMove = (e: TouchEvent) => {
      const y = e.touches[0]?.clientY;
      if (touchY === null || y === undefined) return;
      const dy = y - touchY;
      if (Math.abs(dy) <= TOUCH_DELTA) return;
      push(dy > 0);
      touchY = y;
    };

    el.addEventListener('wheel', onWheel, { passive: true });
    el.addEventListener('scroll', onScroll, { passive: true });
    el.addEventListener('touchstart', onTouchStart, { passive: true });
    el.addEventListener('touchmove', onTouchMove, { passive: true });
    return () => {
      el.removeEventListener('wheel', onWheel);
      el.removeEventListener('scroll', onScroll);
      el.removeEventListener('touchstart', onTouchStart);
      el.removeEventListener('touchmove', onTouchMove);
    };
  }, [column, pinned, resetKey]);

  // Summoned from the middle of a page, the bar enters the flow above
  // everything and would push the content down, so its height is added to
  // scrollTop before the browser paints. Nothing is owed at the top of the
  // column.
  useLayoutEffect(() => {
    if (!revealed || !owed.current) return;
    owed.current = false;
    const el = column();
    const h = barRef.current?.offsetHeight ?? 0;
    if (el && h > 0 && el.scrollTop > 0) {
      el.scrollTop += h;
      // The scroll this causes is the bar's own, not somebody scrolling down.
      lastTop.current = el.scrollTop;
    }
  }, [revealed, column]);

  return {
    revealed,
    barRef,
    hide: useCallback(() => setRevealed(false), []),
    /** Reveals the bar wherever the page stands, for the `/` key and the command palette. */
    show: useCallback(() => {
      setRevealed((was) => {
        // Only a reveal from nothing moves content, so only that one is owed.
        if (!was) owed.current = true;
        return true;
      });
    }, []),
  };
}
