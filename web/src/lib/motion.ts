// The round-two animations of the motion engine, switched from the page. The
// keyframes and the dials live in index.css; this only sets classes.
import { useLayoutEffect, useRef, type RefObject } from 'react';

/**
 * replay plays a one-shot animation class again, also on an element that still
 * has it: taking the class off and forcing a style pass before putting it back
 * restarts the animation without remounting the element, so a keyboard user's
 * focus stays where it was.
 */
export function replay(el: Element | null, cls: string): void {
  if (!el) return;
  el.classList.remove(cls);
  void (el as HTMLElement).offsetWidth;
  el.classList.add(cls);
}

/**
 * useStagger lets the children of `box` arrive one after another. A child is
 * counted the first time it is seen, from the first new one on, so a card or a
 * row added to a list already on screen comes in at once and the ones around
 * it stay where they are. Seen children are remembered apart from their class,
 * which React rewrites whenever a child's own className changes; a child that
 * lost it has already arrived and must not arrive twice.
 */
export function useStagger(box: RefObject<HTMLElement | null>): void {
  const seen = useRef(new WeakSet<Element>());
  useLayoutEffect(() => {
    const el = box.current;
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
