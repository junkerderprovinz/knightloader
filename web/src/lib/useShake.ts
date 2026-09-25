import { useEffect, useRef } from 'react';

/**
 * useShake plays .glim-shake on the element behind the returned ref each time
 * count goes up, as a caller's failure counter does. It restarts the animation
 * on the same node: keying the element on the count would remount it, and the
 * focus a keyboard user left on it would fall to the page.
 */
export function useShake<T extends HTMLElement>(count: number) {
  const ref = useRef<T | null>(null);
  useEffect(() => {
    const el = ref.current;
    if (count === 0 || !el) return;
    el.classList.remove('glim-shake');
    // Reading the layout in between makes the browser start the animation again.
    void el.offsetWidth;
    el.classList.add('glim-shake');
  }, [count]);
  return ref;
}
