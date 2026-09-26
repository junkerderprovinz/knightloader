// The round-two animations of the motion engine, switched from the page. The
// keyframes and the dials live in index.css; this only sets classes.
import { useLayoutEffect, useRef, type CSSProperties } from 'react';

/**
 * useStagger lets the children of the element `list` returns arrive one after
 * another. A child is counted the first time it is seen, from the first new
 * one on, so a card or a row added to a list already on screen comes in at
 * once and the ones around it stay where they are. Seen children are
 * remembered apart from their class, which React rewrites whenever a child's
 * own className changes; a child that lost it has already arrived and must not
 * arrive twice.
 */
export function useStagger(list: () => Element | null | undefined): void {
  const seen = useRef(new WeakSet<Element>());
  useLayoutEffect(() => {
    const el = list();
    if (!el) return;
    let arriving = 0;
    for (const child of el.children) {
      if (seen.current.has(child)) continue;
      seen.current.add(child);
      (child as HTMLElement).style.setProperty('--row-i', String(arriving++));
      child.classList.add('glim-stagger-row');
    }
  });
}

/**
 * useTabSlide returns the class and the direction that slide a tab in from
 * its side when `tab` changes: from the trailing edge toward a later tab in
 * `order`, from the leading edge toward an earlier one. The first tab shown
 * does not slide, since the page's own entrance already brings it in. The
 * answer holds while the tab stays, so a render in the middle of the slide
 * does not cut it short.
 */
export function useTabSlide(tab: string, order: string[]): { className: string; style?: CSSProperties } {
  const last = useRef<{ tab: string; slide: { className: string; style?: CSSProperties } } | null>(null);
  if (last.current?.tab !== tab) {
    const from = last.current?.tab;
    const dir = from !== undefined && order.indexOf(tab) < order.indexOf(from) ? -1 : 1;
    last.current = {
      tab,
      slide: from === undefined ? { className: '' } : { className: 'glim-tab-slide', style: { ['--tab-dir' as string]: dir } },
    };
  }
  return last.current.slide;
}
