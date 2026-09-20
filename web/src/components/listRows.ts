import { useEffect, useLayoutEffect, useMemo, useRef, useState, type RefObject } from 'react';
import type { Task } from '../lib/api';

// The flat row model and the window over it, shared by TaskList.tsx and
// listKeyboard.ts.

/**
 * ListRow is one line of the table: a package header or a link. The rows are
 * flat so a window can slice them, and they are the on-screen order that the
 * Shift-range, drag preview and window all use.
 *
 * `level`, `posinset` and `setsize` live on the model because the DOM holds
 * only the window; they count within each sibling set.
 */
export type ListRow =
  | {
      kind: 'package';
      key: string;
      name: string;
      items: Task[];
      collapsed: boolean;
      divider: boolean;
      level: 1;
      posinset: number;
      setsize: number;
    }
  | {
      kind: 'task';
      key: string;
      task: Task;
      /** Position among the drawn link rows, which sets the rainbow hue. */
      index: number;
      level: 2;
      posinset: number;
      setsize: number;
    };

// Below this many rows the table is drawn whole. Measured on a production
// build: 500 rows redraw in 26ms, while 1000 rows cost 62ms and 5000 cost
// 357ms per websocket tick, almost all of it React re-rendering rows.
const VIRTUALIZE_ABOVE = 150;

// Rows drawn beyond the viewport, so a single arrow step always lands on a
// mounted row.
const OVERSCAN = 12;

// Used only for rows never measured; every drawn row keeps its measured height.
const ROW_ESTIMATE = { package: 45, task: 37 };

/** The first index whose row ends after y: the first row still on screen. */
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
   * Where a row starts in strip coordinates, the target of a keyboard probe.
   * Below VIRTUALIZE_ABOVE nothing is measured, so these are estimates; no
   * probe is placed there because every row is already drawn.
   */
  topOf: (i: number) => number;
  /** A row's height, with the same caveat as topOf. */
  heightOf: (i: number) => number;
}

/**
 * useRowWindow picks the slice of rows worth putting in the DOM. The list has
 * no scroll box of its own, so the window is read against the viewport through
 * the strip's bounding rect and a capture-phase scroll listener; overshooting a
 * shorter scroll box only draws extra rows.
 *
 * Heights are measured and kept per row, since package headers and failed rows
 * differ, and an average would put the scrollbar off by thousands of pixels.
 */
export function useRowWindow(rows: ListRow[], stripRef: RefObject<HTMLDivElement | null>): RowWindow {
  const heights = useRef(new Map<string, number>());
  const estimate = useRef({ ...ROW_ESTIMATE });
  // Bumped only when a measurement moved, which ends the measure-render loop.
  const [measured, setMeasured] = useState(0);
  // Seeded to the top of the strip so the first render is already windowed; a
  // whole-table first pass at 5000 rows cost 3.4s. The layout effect corrects
  // any other position before paint.
  const [viewport, setViewport] = useState(() => ({
    top: 0,
    height: typeof window === 'undefined' ? 0 : window.innerHeight,
  }));

  const on = rows.length > VIRTUALIZE_ABOVE;

  // Running row offsets, recomputed on row or measurement changes, not scroll.
  const offsets = useMemo(() => {
    const known = heights.current;
    // Pruned to live rows once it grows well past the list, so a long-open tab
    // does not keep every row it has ever shown.
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
    // `measured` signals changes to the heights and estimate refs.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rows, measured]);

  // After every commit, since the strip can move without a scroll, for example
  // when the toolbar above it wraps.
  useLayoutEffect(() => {
    // A list drawn whole needs no reflow.
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
    // At most one read per frame.
    const schedule = () => {
      if (!frame) frame = requestAnimationFrame(read);
    };
    // Capture, because scroll does not bubble and the scrolling ancestor
    // differs by page.
    window.addEventListener('scroll', schedule, true);
    window.addEventListener('resize', schedule);
    return () => {
      if (frame) cancelAnimationFrame(frame);
      window.removeEventListener('scroll', schedule, true);
      window.removeEventListener('resize', schedule);
    };
  }, [stripRef]);

  // Both returns carry the same readers, windowed or not.
  const last = rows.length;
  const topOf = (i: number) => offsets[Math.max(0, Math.min(i, last))];
  const heightOf = (i: number) => (i < 0 || i >= last ? 0 : offsets[i + 1] - offsets[i]);

  if (!on || viewport.height === 0) return { start: 0, end: rows.length, padTop: 0, padBottom: 0, topOf, heightOf };
  const start = Math.max(0, firstAfter(offsets, viewport.top) - OVERSCAN);
  const end = Math.min(rows.length, firstAfter(offsets, viewport.top + viewport.height) + 1 + OVERSCAN);
  return { start, end, padTop: offsets[start], padBottom: offsets[rows.length] - offsets[end], topOf, heightOf };
}

/**
 * measureRows records the drawn rows' heights and reports whether any changed.
 * It uses getBoundingClientRect because offsetHeight rounds to whole pixels.
 * Every `data-row-key` element feeds the estimate, so nothing but a row, not
 * the spacers or the keyboard probe, may carry that attribute.
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
  // Unmeasured rows are estimated from the average of their drawn siblings.
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

/** RowDragKey is what one row drag carries: a link or a whole package. */
export type RowDragKey = { kind: 'task'; id: string } | { kind: 'package'; name: string };

/**
 * rowKey is the string a row is known by in drags and in the keyboard cursor.
 * The kind prefix keeps a package and a task with the same name apart, and
 * `pkg:` alone is the ungrouped package.
 */
export function rowKey(u: RowDragKey): string {
  return u.kind === 'task' ? `task:${u.id}` : `pkg:${u.name}`;
}
