import { useEffect, useRef } from 'react';

/**
 * useShake plays .glim-shake on the element behind the returned ref each time
 * count goes up, as a caller's failure counter does. It restarts the animation
 * on the same node: keying the element on the count would remount it, and the
 * focus a keyboard user left on it would fall to the page.
 */
export function useShake<T extends HTMLElement>(count: number) {
  return useReplay<T>(count, 'glim-shake', 'glim-confirm');
}

/**
 * useConfirm plays .glim-confirm, the success pulse, the same way for a
 * caller's success counter: a copy, an import or a restore that went through.
 */
export function useConfirm<T extends HTMLElement>(count: number) {
  return useReplay<T>(count, 'glim-confirm', 'glim-shake');
}

// One element plays one animation, so the other outcome's class goes first:
// a pulse after a refusal would otherwise sit behind the shake's rule.
function useReplay<T extends HTMLElement>(count: number, cls: string, other: string) {
  const ref = useRef<T | null>(null);
  useEffect(() => {
    const el = ref.current;
    if (count === 0 || !el) return;
    el.classList.remove(cls, other);
    // Reading the layout in between makes the browser start the animation again.
    void el.offsetWidth;
    el.classList.add(cls);
  }, [count]);
  return ref;
}
