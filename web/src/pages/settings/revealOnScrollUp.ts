// The settings search bar appears above the first card only when somebody
// scrolls up for it, and changing settings page puts it away again.
//
// It listens for wheel as well as scroll because at the top of the column the
// browser fires no scroll event; scroll still catches a scrollbar drag and Page
// Up, which fire no wheel event.
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';

/** How much movement counts as a direction rather than as settling noise. */
const DELTA = 6;

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

    const up = () =>
      setRevealed((was) => {
        // Only a reveal from nothing moves content, so only that one is owed.
        if (!was) owed.current = true;
        return true;
      });

    const onWheel = (e: WheelEvent) => {
      if (e.deltaY < -DELTA) up();
      else if (e.deltaY > DELTA) setRevealed(false);
    };
    const onScroll = () => {
      const y = el.scrollTop;
      const dy = y - lastTop.current;
      lastTop.current = y;
      if (Math.abs(dy) < DELTA) return;
      if (dy < 0) up();
      else setRevealed(false);
    };

    el.addEventListener('wheel', onWheel, { passive: true });
    el.addEventListener('scroll', onScroll, { passive: true });
    return () => {
      el.removeEventListener('wheel', onWheel);
      el.removeEventListener('scroll', onScroll);
    };
  }, [column, pinned, resetKey]);

  // The bar enters the flow above everything and would push the content down
  // mid-gesture, so its height is added to scrollTop before the browser paints.
  // Nothing is owed at the top of the column.
  useLayoutEffect(() => {
    if (!revealed || !owed.current) return;
    owed.current = false;
    const el = column();
    const h = barRef.current?.offsetHeight ?? 0;
    if (el && h > 0 && el.scrollTop > 0) {
      el.scrollTop += h;
      lastTop.current = el.scrollTop;
    }
  }, [revealed, column]);

  return {
    revealed,
    barRef,
    hide: useCallback(() => setRevealed(false), []),
    /** Reveals the bar for the command palette's "Search all settings". */
    show: useCallback(() => {
      owed.current = true;
      setRevealed(true);
    }, []),
  };
}
