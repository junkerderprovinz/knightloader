import { useEffect, useLayoutEffect, useMemo, useRef, useState, type RefObject } from 'react';
import type { Task } from '../lib/api';

/**
 * The flat row model and the window over it — lifted out of TaskList.tsx so
 * that the keyboard can read the same two things the table draws from.
 *
 * It moved for one reason: listKeyboard.ts needs `ListRow` and `RowWindow`,
 * and importing them back out of TaskList.tsx would leave a cycle waiting for
 * whoever next reaches for a value rather than a type across the same seam.
 * Nothing here knows a key was ever pressed; it is the model both the mouse
 * and the keyboard stand on.
 */

/**
 * One line of the table: a folder header, or a link under one.
 *
 * The rows are a flat run and not a tree of sections, and that is the whole of
 * what makes windowing possible - a window is a slice, and there is nothing to
 * slice while the rows are buried one level down inside their packages. Built
 * once per render from the same `view` and the same folded set the table draws
 * from, so this list IS the on-screen order rather than a second opinion about
 * it: the Shift-range walks it, the drag preview stacks it, and the window
 * measures it.
 *
 * `level`, `posinset` and `setsize` are the tree's own sibling-set numbers, and
 * they are on the MODEL rather than derived where the row is drawn on purpose:
 * the DOM holds forty rows out of five thousand, so anything counted off the
 * window would announce "3 of 40" on a list of five thousand, which is a worse
 * answer than none at all. In a tree they are per sibling set, never per list -
 * a link is the third of twelve in ITS folder, and a folder is the second of
 * six folders.
 */
export type ListRow =
  | {
      kind: 'package';
      key: string;
      name: string;
      items: Task[];
      collapsed: boolean;
      divider: boolean;
      /** 1 - a folder is a root of the tree. */
      level: 1;
      /** Its place among the folders, one-based. */
      posinset: number;
      /** How many folders there are. */
      setsize: number;
    }
  | {
      kind: 'task';
      key: string;
      task: Task;
      /** Position among the drawn LINK rows, which is the rainbow palette
       *  position. A folded package contributes none, exactly as before: the
       *  colour walks what is on screen, not what the list holds. */
      index: number;
      /** 2 - a link sits inside its folder. */
      level: 2;
      /** Its place among the links of ITS OWN folder, one-based. */
      posinset: number;
      /** How many links that folder holds. */
      setsize: number;
    };

// --- Windowing -------------------------------------------------------------
//
// Below this many rows the table is drawn whole, spacers and all arithmetic
// skipped. Measured, on a production build at 1600x1000 (web/bench.html, since
// deleted): 200 rows redraw in 11ms and 500 in 26ms, so a list this size is
// already inside a frame's budget for the two things that happen constantly - a
// websocket task update and a click on a row - and windowing it would buy
// nothing but a second code path through the drag preview.
//
// Above it, the same measurement is why this exists at all: 1000 rows cost 62ms
// per websocket tick, 2000 cost 126ms and 5000 cost 357ms, and 99% of that is
// React re-rendering every row (the same change with no changed VALUE costs the
// same 355ms, and a plain selection click, which writes one class, costs 345ms -
// so it is neither the DOM write nor the layout). One running download emits a
// progress update roughly every second; on a five-thousand-link list that was a
// third of a second of frozen interface, every second.
const VIRTUALIZE_ABOVE = 150;

// How many rows above and below the viewport are drawn anyway.
//
// It used to be what kept Tab working, and that reason is now gone: the list is
// one tab stop, not one per row (see listKeyboard.ts), so the browser is no
// longer picking the next focusable row out of the DOM at the moment the key is
// pressed. The buffer still earns its keep, and now for the keyboard rather than
// against it - a single arrow step always lands on a row that is already
// mounted, so the ordinary case of walking the list never has to go through the
// probe-and-scroll path listKeyboard.ts keeps for a real jump. Twelve rows is
// several presses' worth of head start in either direction, and the scroll each
// of those presses causes has moved the window on again long before the buffer
// runs out.
const OVERSCAN = 12;

// What a row is worth before anything has measured one. Only ever used for rows
// that have not been on screen yet; every row that has is remembered at its own
// measured height (see useRowWindow), so the scrollbar describes the real list
// rather than an average of it.
const ROW_ESTIMATE = { package: 45, task: 37 };

/** The first index whose row ENDS after y, i.e. the first row still on screen. */
function firstAfter(offsets: Float64Array, y: number): number {
  let lo = 0;
  let hi = offsets.length - 1;
  while (lo < hi) {
    const mid = (lo + hi) >> 1;
    if (offsets[mid + 1] <= y) lo = mid + 1;
    else hi = mid;
  }
  return lo;
}

export interface RowWindow {
  /** The slice to render: [start, end). */
  start: number;
  end: number;
  /** The height of everything before and after it, as two empty boxes. */
  padTop: number;
  padBottom: number;
  /**
   * Where a row starts, in the strip's own coordinates - what a keyboard jump
   * to a row nobody has drawn aims its probe at (see listKeyboard.ts).
   *
   * ONLY TRUSTWORTHY WHILE THE WINDOW IS ON. The offsets are a running total of
   * measured heights, and the effect that measures them returns early on a list
   * short enough to be drawn whole, so below VIRTUALIZE_ABOVE every number here
   * is the estimate and nothing else. That is harmless for the one caller there
   * is - under 150 rows every row is already in the DOM and no probe is ever
   * placed - but anyone reaching for this for something else is reading guesses.
   */
  topOf: (i: number) => number;
  /** What one row is worth, under the same caveat as topOf. */
  heightOf: (i: number) => number;
}

/**
 * Which slice of the table is worth putting in the DOM.
 *
 * The list card has no scroll box of its own (Downloads.tsx renders it in the
 * page's own flow and Collector.tsx wraps it in an `overflow-y-auto` of its
 * own), so the window is read off the VIEWPORT rather than off a container this
 * component owns: `getBoundingClientRect` on the row strip says where the strip
 * sits relative to the screen, whichever ancestor happens to be doing the
 * scrolling, and a scroll listener in the capture phase hears that ancestor
 * without having to know which one it is. Using the whole viewport height where
 * a caller's own box is shorter only ever draws MORE rows than strictly needed,
 * which is the safe direction to be wrong in.
 *
 * Heights are measured and remembered per row, not assumed: a package header is
 * taller than a link row, and a link row that failed carries a second line with
 * the reason on it. An average would leave the scrollbar promising a length the
 * list does not have: off by a couple of pixels per row is off by thousands over
 * a few thousand rows. So every row that has been drawn once keeps its own
 * height, and only a row nobody has scrolled past yet uses the estimate.
 */
export function useRowWindow(rows: ListRow[], stripRef: RefObject<HTMLDivElement | null>): RowWindow {
  const heights = useRef(new Map<string, number>());
  const estimate = useRef({ ...ROW_ESTIMATE });
  // Bumped only when a measurement actually moved, which is what keeps the
  // measure-then-render loop from running forever.
  const [measured, setMeasured] = useState(0);
  // Seeded rather than left at zero, and that is worth a paragraph: the layout
  // effect below cannot run until AFTER a render, so a window that waits for it
  // draws the whole table once and throws it away on the very next pass. Measured
  // at 5000 rows, that one wasted pass cost 3.4s against 0.6s for a window that
  // was right the first time - it built all 188k nodes, then tore them down.
  //
  // The list starts at the top of its own strip, which is where it is when a page
  // has just been opened; anything else (a browser restoring a scroll position, a
  // list re-mounted further down a scrolled page) is corrected by the effect
  // before the frame is painted.
  const [viewport, setViewport] = useState(() => ({
    top: 0,
    height: typeof window === 'undefined' ? 0 : window.innerHeight,
  }));

  const on = rows.length > VIRTUALIZE_ABOVE;

  // Where every row starts, as a running total. Recomputed when the rows change
  // or when a measurement corrects one of them, and deliberately NOT on scroll:
  // scrolling only moves the window, it does not change what the rows are.
  const offsets = useMemo(() => {
    const known = heights.current;
    // The cache is keyed by row, and rows come and go for as long as the app
    // stays open - a clean-up removes a few hundred, a crawl adds a few
    // thousand. Rebuilt from the rows that actually exist once it has grown to
    // several times the list's own length, so an instance somebody leaves open
    // for a week is not carrying the heights of every link it has ever shown.
    if (known.size > rows.length * 4 + 64) {
      const kept = new Map<string, number>();
      for (const row of rows) {
        const h = known.get(row.key);
        if (h !== undefined) kept.set(row.key, h);
      }
      heights.current = kept;
    }
    const out = new Float64Array(rows.length + 1);
    for (let i = 0; i < rows.length; i++) {
      const row = rows[i];
      const h = heights.current.get(row.key) ?? estimate.current[row.kind];
      out[i + 1] = out[i] + h;
    }
    return out;
    // heights and estimate are refs read at the moment this runs; `measured` is
    // the signal that either of them has changed.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rows, measured]);

  // Re-read after every commit, not only when something in here changed: the
  // strip moves down the page when the toolbar above it grows a row, and it is
  // this read - not a scroll - that notices.
  useLayoutEffect(() => {
    // Nothing here is worth a forced reflow on a list that is drawn whole: with
    // no window there is no slice to place and no spacer to size, so a short
    // list pays none of this.
    if (!on) return;
    const el = stripRef.current;
    if (!el) return;
    if (measureRows(el, heights.current, estimate.current)) setMeasured((n) => n + 1);
    const r = el.getBoundingClientRect();
    const top = Math.max(0, -r.top);
    const height = window.innerHeight;
    setViewport((p) => (Math.abs(p.top - top) < 1 && p.height === height ? p : { top, height }));
  });

  useEffect(() => {
    let frame = 0;
    const read = () => {
      frame = 0;
      const el = stripRef.current;
      if (!el) return;
      const r = el.getBoundingClientRect();
      const top = Math.max(0, -r.top);
      const height = window.innerHeight;
      setViewport((p) => (Math.abs(p.top - top) < 1 && p.height === height ? p : { top, height }));
    };
    // One read per frame at most: a scroll fires far more often than the screen
    // is repainted, and a window recomputed per event would re-render the table
    // several times for one flick of a wheel.
    const schedule = () => {
      if (!frame) frame = requestAnimationFrame(read);
    };
    // Capture, because a scroll event does not bubble: the ancestor actually
    // doing the scrolling is the page's own <main> for the downloads list and a
    // wrapper of its own for the collector, and neither is reachable from here.
    window.addEventListener('scroll', schedule, true);
    window.addEventListener('resize', schedule);
    return () => {
      if (frame) cancelAnimationFrame(frame);
      window.removeEventListener('scroll', schedule, true);
      window.removeEventListener('resize', schedule);
    };
  }, [stripRef]);

  // Both exits hand out the same two readers, closed over the same offsets: a
  // caller must not have to ask whether the window happens to be on today
  // before it can ask where a row is.
  const last = rows.length;
  const topOf = (i: number) => offsets[Math.max(0, Math.min(i, last))];
  const heightOf = (i: number) => (i < 0 || i >= last ? 0 : offsets[i + 1] - offsets[i]);

  if (!on || viewport.height === 0) return { start: 0, end: rows.length, padTop: 0, padBottom: 0, topOf, heightOf };
  const start = Math.max(0, firstAfter(offsets, viewport.top) - OVERSCAN);
  const end = Math.min(rows.length, firstAfter(offsets, viewport.top + viewport.height) + 1 + OVERSCAN);
  return { start, end, padTop: offsets[start], padBottom: offsets[rows.length] - offsets[end], topOf, heightOf };
}

/**
 * Reads back what the rows that are currently drawn actually measure, and says
 * whether anything moved.
 *
 * getBoundingClientRect and not offsetHeight: offsetHeight is rounded to whole
 * pixels, and half a pixel per row is a couple of thousand pixels of scrollbar
 * over a list this long. A row mid-drag is translated rather than scaled, so its
 * measured height is the same either way.
 *
 * It finds rows by `data-row-key` and averages every one it finds into the
 * estimate, which is why nothing that is not a row may ever carry that
 * attribute - the two spacers do not, and neither does the keyboard's own
 * one-pixel scroll probe (listKeyboard.ts). A 1px row in this average drags the
 * guess for every unvisited row in the list toward zero, and takes the
 * scrollbar with it.
 */
function measureRows(
  strip: HTMLElement,
  heights: Map<string, number>,
  estimate: { package: number; task: number },
): boolean {
  let changed = false;
  let pkgSum = 0;
  let pkgCount = 0;
  let taskSum = 0;
  let taskCount = 0;
  strip.querySelectorAll<HTMLElement>('[data-row-key]').forEach((el) => {
    const key = el.dataset.rowKey ?? '';
    const h = el.getBoundingClientRect().height;
    if (h <= 0) return;
    if (el.dataset.rowKind === 'package') {
      pkgSum += h;
      pkgCount++;
    } else {
      taskSum += h;
      taskCount++;
    }
    const known = heights.get(key);
    if (known === undefined || Math.abs(known - h) >= 0.5) {
      heights.set(key, h);
      changed = true;
    }
  });
  // The estimate follows what this list's own rows actually measure, so the
  // rows nobody has scrolled to yet are guessed at from siblings rather than
  // from a constant written months ago against a different font size.
  if (pkgCount > 0) {
    const avg = pkgSum / pkgCount;
    if (Math.abs(estimate.package - avg) >= 0.5) {
      estimate.package = avg;
      changed = true;
    }
  }
  if (taskCount > 0) {
    const avg = taskSum / taskCount;
    if (Math.abs(estimate.task - avg) >= 0.5) {
      estimate.task = avg;
      changed = true;
    }
  }
  return changed;
}

/**
 * What one row drag is carrying — a single link, or a whole package moved as
 * one block. See TaskListCard's own "Row drag-to-reorder" section, the only
 * place this is built; TaskRow and PackageGroup only read it.
 */
export type RowDragKey = { kind: 'task'; id: string } | { kind: 'package'; name: string };

/**
 * The one string a row is known by across the whole drag: the geometry snapshot,
 * the previewed arrangement and the per-row offset all key on this. The keyboard
 * cursor is held as this string too, and for a related reason - an index into a
 * list the websocket rebuilds every second points at a different row a moment
 * later.
 *
 * A package name and a task id live in the same map, so the kind has to be part
 * of the key - a folder called after one of its own links would otherwise share
 * a slot with it. `pkg:` for the unnamed package is a real key, not a missing
 * one, which is the same reason data-package-row is present-and-empty rather
 * than absent.
 */
export function rowKey(u: RowDragKey): string {
  return u.kind === 'task' ? `task:${u.id}` : `pkg:${u.name}`;
}
