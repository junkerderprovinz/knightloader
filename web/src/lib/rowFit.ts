// Keeps a row of controls on one line. When what the row holds is wider than
// the row, the caller folds one more thing away and the row is measured again,
// until it fits or there is nothing left to fold; a wider window unfolds them
// again in the reverse order.
import { useLayoutEffect, useRef, useState, type RefObject } from 'react';

/**
 * nextFold is the fold level to try after measuring: one up while the content
 * is wider than the room and a step is left, one down once the content the
 * level below asked for fits again, else the same.
 */
export function nextFold(level: number, last: number, want: number, room: number, below?: number): number {
  if (want > room && level < last) return level + 1;
  if (level > 0 && below !== undefined && below <= room) return level - 1;
  return level;
}

/**
 * useRowFit returns how many fold steps, from 0 to `last`, the row at `ref`
 * needs. `content` names what the row holds; when it changes the row starts
 * again from 0, since a width measured with other content says nothing about
 * this one. The row's children keep their own width (shrink-0), so their sum
 * and the gaps between them are what the row asks for.
 */
export function useRowFit(ref: RefObject<HTMLElement | null>, last: number, content: string): number {
  const [fit, setFit] = useState({ level: 0, content });
  const widths = useRef<number[]>([]);
  let level = fit.level;
  if (fit.content !== content) {
    widths.current = [];
    level = 0;
    setFit({ level, content });
  }

  useLayoutEffect(() => {
    const row = ref.current;
    if (!row) return;
    const measure = () => {
      const want = wanted(row);
      widths.current[level] = want;
      const next = nextFold(level, last, want, row.clientWidth, widths.current[level - 1]);
      if (next !== level) setFit((f) => ({ ...f, level: next }));
    };
    measure();
    // A child can grow while the row's own box keeps its size: a chip renamed
    // in place, a font arriving late. So every child is watched as well, and
    // a child that turns up later joins them.
    const watch = new ResizeObserver(measure);
    const observeAll = () => {
      watch.observe(row);
      for (const kid of row.children) watch.observe(kid);
    };
    observeAll();
    const arrivals = new MutationObserver(() => {
      observeAll();
      measure();
    });
    arrivals.observe(row, { childList: true });
    return () => {
      watch.disconnect();
      arrivals.disconnect();
    };
  }, [ref, level, last, content]);

  return level;
}

/** The width the row's children take side by side, a flexible spacer left out. */
function wanted(row: HTMLElement): number {
  const kids = [...row.children].filter((k): k is HTMLElement => k instanceof HTMLElement);
  const gap = parseFloat(getComputedStyle(row).columnGap) || 0;
  const own = kids.filter((k) => !k.hasAttribute('data-spacer')).reduce((sum, k) => sum + k.offsetWidth, 0);
  return own + gap * Math.max(0, kids.length - 1);
}
