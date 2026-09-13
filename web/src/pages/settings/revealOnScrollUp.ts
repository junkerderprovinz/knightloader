// The settings search bar is not there until somebody scrolls up for it.
//
// THREE ATTEMPTS, AND WHY THE FIRST TWO WERE WRONG. jdp asked for this in
// stages, and each answer was a reasonable reading that missed the point:
//
//   1. Sticky, sliding away while reading downward. Rejected: "die suchleiste
//      schwebt immer noch ueber alles". The complaint was the floating itself,
//      not its timing - a sticky bar covers content the whole time it is up.
//   2. Static, in the flow above the first card. Rejected: "die suchleiste soll
//      nicht sichtbar sein!!!". In the flow it is still there whenever you are
//      at the top of the column, and he does not want to see it at all.
//
// So: it does not exist until an upward gesture summons it, it appears above
// the first card rather than over anything, and changing settings page puts it
// away again. A search box is a thing you go and get, not a thing that watches
// you read.
//
// WHY A WHEEL LISTENER AND NOT ONLY SCROLL. At the top of the column there is
// nothing left to scroll, so the browser fires no scroll event at all - and the
// top is exactly where somebody stands when they reach for this. Without the
// wheel half, the bar would be unreachable from the one position it belongs to.
// The scroll half still earns its place: it catches a drag on the scrollbar and
// a keyboard Page Up, which produce no wheel event.
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

  // A new page means a new question, and the old box was answering the previous
  // one. This is the whole of jdp's "wenn man den tab wechselt soll sie wieder
  // weg sein".
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
        // Only a reveal from nothing has to be paid for below; re-affirming an
        // open bar moves no content and must not move the column either.
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

  // PAYING FOR THE SPACE THE BAR TAKES.
  //
  // The bar enters the flow ABOVE everything, so inserting it while the column
  // is scrolled pushes the content down by its height - and it does that just
  // as somebody is scrolling UP, which reads as the page fighting the gesture.
  // Adding the same height to scrollTop in the same frame keeps every card
  // exactly where the eye left it, and the bar is simply there once the top is
  // reached.
  //
  // Layout effect, not a plain one: this has to run before the browser paints,
  // or the jump is visible for a frame and the compensation becomes a flicker.
  // Nothing is owed when the column is already at the top, because there the
  // content has nowhere to be pushed from.
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
    /**
     * Summoned by something other than a gesture: the command palette's own
     * "Search all settings" reaches this box by leaving a timestamp, and with
     * the bar absent there would be nothing for it to focus. A command that
     * silently does nothing is worse than no command.
     */
    show: useCallback(() => {
      owed.current = true;
      setRevealed(true);
    }, []),
  };
}
