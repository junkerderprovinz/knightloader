// Moving a list column by its head, the way GlimStone's "Reordering by
// dragging" moves an item: the column lifts and follows the pointer, head and
// cells together, the others slide aside as it passes their middle, and on
// release it settles into the gap. Escape puts everything back and writes
// nothing. The order is written once, on the drop.
//
// The live order goes through React, so the grid template and every row draw
// from it; the pointer's travel does not. It goes to one custom property on the
// table, read by every cell of the carried column, so a move costs one style
// write however many rows are drawn. index.css draws the states from the
// --drag-* tokens, which makes every slide instant under reduced motion and at
// the motion level "off".
import { useEffect, useLayoutEffect, useRef, useState, type PointerEvent as ReactPointerEvent, type RefObject } from 'react';
import { flushSync } from 'react-dom';
import { COLUMN_BY_ID, moveColumn, type ColumnId, type ResolvedLayout } from './columns';
import { HOLD_MS, HOLD_SLOP_PX, sameOrder, settleMs } from './dragLift';
import { pastThreshold } from './rowDrag';

/** A column as laid out in the head row: its left edge and its width. */
export interface ColumnBox {
  id: ColumnId;
  x: number;
  w: number;
}

/**
 * liveColumnOrder is the order the carried column asks for when its left edge
 * stands at `x`. A column counts as passed once the carried one's leading edge,
 * the left one on the way left and the right one on the way right, is past
 * the column's middle. The centre would not do: a wide column held at the
 * table's edge keeps its centre beyond a narrow neighbour's and could never
 * reach the end. The passed column moves away by the carried one's width, so
 * two columns of different widths cannot trade places back and forth.
 */
export function liveColumnOrder(boxes: ColumnBox[], held: ColumnId, x: number, rtl: boolean): ColumnId[] {
  const from = boxes.find((b) => b.id === held);
  const edge = from && x > from.x ? x + from.w : x;
  const rest: ColumnId[] = [];
  let ahead = 0;
  for (const b of boxes) {
    if (b.id === held) continue;
    const mid = b.x + b.w / 2;
    if (rtl ? mid > edge : mid < edge) ahead++;
    rest.push(b.id);
  }
  rest.splice(ahead, 0, held);
  return rest;
}

/**
 * dropColumn is the stored order after `held` was dropped where `live` shows
 * it. `live` holds only the columns on show; a hidden one keeps its place
 * between them, as it does for stepColumn.
 */
export function dropColumn(order: ColumnId[], live: ColumnId[], held: ColumnId): ColumnId[] {
  const at = live.indexOf(held);
  return at > 0 ? moveColumn(order, held, live[at - 1], true) : moveColumn(order, held, live[1], false);
}

/** withVisible is `layout` drawing its columns in `ids`. */
export function withVisible(layout: ResolvedLayout, ids: ColumnId[]): ResolvedLayout {
  if (sameOrder(ids, layout.visible.map((c) => c.id))) return layout;
  return { ...layout, visible: ids.map((id) => COLUMN_BY_ID.get(id)!) };
}

// The pointer's travel, set on the table and read by the carried column's cells.
const TRAVEL = '--kl-col-dx';

interface Press {
  id: ColumnId;
  x: number;
  y: number;
  pointerId: number;
  /** A finger has to hold still first, or the press is a scroll or a tap. */
  touch: boolean;
  armed: boolean;
}

interface Carried {
  id: ColumnId;
  downX: number;
  /** The head's left edge and width when it lifted. */
  fromX: number;
  w: number;
  /** How far its left edge may go, so it stays over the table. */
  minX: number;
  maxX: number;
  rtl: boolean;
}

export interface ColumnDragOptions {
  /** The columns on show, in their resting order. */
  ids: ColumnId[];
  /** The table: the head row and the rows, every cell marked with data-cell. */
  table: RefObject<HTMLElement | null>;
  /** Called on a drop that changed the order, with the order it shows. */
  onDrop: (held: ColumnId, live: ColumnId[]) => void;
}

export interface ColumnDrag {
  /** The order to draw: the live one while a column is carried or landing. */
  order: ColumnId[];
  /** Whether a column is lifted, for the heads that tremble meanwhile. */
  held: boolean;
  /** The pointerdown handler on a column's head. */
  press: (e: ReactPointerEvent<HTMLElement>, id: ColumnId) => void;
  /** Whether a column is under the pointer. */
  carrying: () => boolean;
  /** Drops a press that has not lifted anything, for a long press that opened a menu instead. */
  cancel: () => void;
  /** Called before the order changes some other way, so the columns slide to their new places. */
  slide: () => void;
  /** For the head's click handler: whether this click ends a drag and is to be swallowed. */
  endsDrag: (e: { detail: number }) => boolean;
}

/** useColumnDrag is the gesture on a list's column heads; see the file comment. */
export function useColumnDrag({ ids, table, onDrop }: ColumnDragOptions): ColumnDrag {
  const [live, setLive] = useState<ColumnId[] | null>(null);
  const latest = useRef({ ids, onDrop, live });
  latest.current = { ids, onDrop, live };
  const [gesture] = useState(() => new Gesture(table, latest, setLive));

  useLayoutEffect(() => gesture.afterRender());
  useEffect(() => () => gesture.stop(), [gesture]);

  // A column switched on mid-drag is drawn after the live order rather than
  // left out until it ends.
  const order = live ? [...live.filter((id) => ids.includes(id)), ...ids.filter((id) => !live.includes(id))] : ids;
  return {
    order,
    held: live !== null,
    press: gesture.press,
    carrying: gesture.carrying,
    cancel: gesture.cancel,
    slide: gesture.snapshot,
    endsDrag: gesture.endsDrag,
  };
}

type Latest = RefObject<{ ids: ColumnId[]; onDrop: ColumnDragOptions['onDrop']; live: ColumnId[] | null }>;

class Gesture {
  private table: RefObject<HTMLElement | null>;
  private latest: Latest;
  private setLive: (order: ColumnId[] | null) => void;
  private pressed: Press | null = null;
  private carried: Carried | null = null;
  private boxes: ColumnBox[] = [];
  private pointerX = 0;
  private holdTimer = 0;
  private restTimer = 0;
  // Where each column was painted before the order changed, for the slide.
  private slideFrom: Map<ColumnId, number> | null = null;
  private swallow = false;

  constructor(table: RefObject<HTMLElement | null>, latest: Latest, setLive: (order: ColumnId[] | null) => void) {
    this.table = table;
    this.latest = latest;
    this.setLive = setLive;
  }

  private cells(id?: ColumnId): HTMLElement[] {
    const sel = id ? `[data-cell="${id}"]` : '[data-cell]';
    return Array.from(this.table.current?.querySelectorAll<HTMLElement>(sel) ?? []);
  }

  /** The head row comes first in the table, so a column's first cell is its head. */
  private heads(): HTMLElement[] {
    const first = this.table.current?.querySelector<HTMLElement>('[data-cell]');
    return Array.from(first?.parentElement?.querySelectorAll<HTMLElement>(':scope > [data-cell]') ?? []);
  }

  private measure(): ColumnBox[] {
    return this.heads().map((h) => ({ id: h.dataset.cell as ColumnId, x: h.offsetLeft, w: h.offsetWidth }));
  }

  /** Where each head is painted, a slide in flight included. */
  private painted(): Map<ColumnId, number> {
    const out = new Map<ColumnId, number>();
    for (const h of this.heads()) {
      out.set(h.dataset.cell as ColumnId, h.offsetLeft + (parseFloat(getComputedStyle(h).translate) || 0));
    }
    return out;
  }

  private heldX(c: Carried, kept: boolean): number {
    const x = c.fromX + this.pointerX - c.downX;
    return kept ? Math.min(Math.max(x, c.minX), c.maxX) : x;
  }

  private place(c: Carried): void {
    const box = this.boxes.find((b) => b.id === c.id);
    if (box) this.table.current?.style.setProperty(TRAVEL, `${this.heldX(c, true) - box.x}px`);
  }

  press = (e: ReactPointerEvent<HTMLElement>, id: ColumnId): void => {
    if (e.button !== 0 || this.carried) return;
    this.cancel();
    const touch = e.pointerType === 'touch';
    this.pressed = { id, x: e.clientX, y: e.clientY, pointerId: e.pointerId, touch, armed: !touch };
    this.pointerX = e.clientX;
    this.swallow = false;
    if (touch) {
      this.holdTimer = window.setTimeout(() => {
        if (this.pressed) this.pressed.armed = true;
      }, HOLD_MS);
    }
    document.addEventListener('pointermove', this.onMove);
    document.addEventListener('pointerup', this.onUp);
    document.addEventListener('pointercancel', this.onCancel);
    document.addEventListener('keydown', this.onKey);
    // Bound only while a finger is down: a listener that may cancel makes
    // every scroll on the page wait for it.
    if (touch) document.addEventListener('touchmove', this.onTouchMove, { passive: false });
  };

  carrying = (): boolean => this.carried !== null;

  cancel = (): void => {
    if (this.carried) return;
    window.clearTimeout(this.holdTimer);
    this.pressed = null;
    this.unlisten();
  };

  snapshot = (): void => {
    this.slideFrom = this.painted();
  };

  endsDrag = (e: { detail: number }): boolean => {
    const ends = this.swallow && e.detail > 0;
    this.swallow = false;
    return ends;
  };

  stop(): void {
    window.clearTimeout(this.holdTimer);
    window.clearTimeout(this.restTimer);
    this.pressed = null;
    this.carried = null;
    this.unlisten();
  }

  private unlisten(): void {
    document.removeEventListener('pointermove', this.onMove);
    document.removeEventListener('pointerup', this.onUp);
    document.removeEventListener('pointercancel', this.onCancel);
    document.removeEventListener('keydown', this.onKey);
    document.removeEventListener('touchmove', this.onTouchMove);
  }

  private lift(p: Press): void {
    const el = this.table.current;
    this.boxes = this.measure();
    const from = this.boxes.find((b) => b.id === p.id);
    if (!el || !from) return;
    window.clearTimeout(this.restTimer);
    const left = Math.min(...this.boxes.map((b) => b.x));
    const right = Math.max(...this.boxes.map((b) => b.x + b.w));
    this.carried = {
      id: p.id,
      downX: p.x,
      fromX: from.x,
      w: from.w,
      minX: left,
      maxX: right - from.w,
      rtl: getComputedStyle(el).direction === 'rtl',
    };
    this.swallow = true;
    for (const cell of this.cells()) {
      if (cell.dataset.cell !== p.id) {
        cell.dataset.colDrag = 'shift';
        continue;
      }
      // A column still sliding from a step before carries its own translate,
      // which would outrank the travel.
      cell.dataset.colDrag = 'held';
      cell.dataset.held = 'true';
      cell.style.translate = '';
      cell.style.transition = '';
    }
    this.place(this.carried);
    this.setLive([...this.latest.current.ids]);
  }

  private onMove = (e: PointerEvent): void => {
    const p = this.pressed;
    if (!p || e.pointerId !== p.pointerId) return;
    this.pointerX = e.clientX;
    const c = this.carried;
    if (!c) {
      const dx = e.clientX - p.x;
      const dy = e.clientY - p.y;
      if (!p.armed) {
        if (Math.abs(dx) > HOLD_SLOP_PX || Math.abs(dy) > HOLD_SLOP_PX) this.cancel();
      } else if (pastThreshold(dx, 0)) {
        this.lift(p);
      }
      return;
    }
    const next = liveColumnOrder(this.boxes, c.id, this.heldX(c, true), c.rtl);
    const shown = this.latest.current.live;
    if (shown && !sameOrder(next, shown)) {
      this.slideFrom = this.painted();
      this.setLive(next);
    } else {
      this.place(c);
    }
  };

  private onUp = (e: PointerEvent): void => {
    if (e.pointerId !== this.pressed?.pointerId) return;
    const c = this.carried;
    if (!c) {
      this.cancel();
      return;
    }
    const { ids, live, onDrop } = this.latest.current;
    if (live && !sameOrder(live, ids)) onDrop(c.id, live);
    this.release(c);
  };

  private onCancel = (e: PointerEvent): void => {
    if (e.pointerId !== this.pressed?.pointerId) return;
    if (this.carried) this.putBack(this.carried);
    else this.cancel();
  };

  private onKey = (e: KeyboardEvent): void => {
    if (e.key !== 'Escape' || !this.carried) return;
    e.preventDefault();
    this.putBack(this.carried);
  };

  private onTouchMove = (e: TouchEvent): void => {
    if (this.pressed?.armed) e.preventDefault();
  };

  /** Escape, or the browser taking the pointer: the others go home while the column is still lifted, then it lands. */
  private putBack(c: Carried): void {
    const { ids, live } = this.latest.current;
    if (live && !sameOrder(live, ids)) {
      this.slideFrom = this.painted();
      flushSync(() => this.setLive([...ids]));
    }
    this.release(c);
  }

  private release(c: Carried): void {
    this.carried = null;
    this.pressed = null;
    this.unlisten();
    const cells = this.cells(c.id);
    for (const cell of cells) {
      cell.dataset.colDrag = 'settle';
      delete cell.dataset.held;
    }
    this.table.current?.style.setProperty(TRAVEL, '0px');
    this.restAfter(cells[0]);
  }

  /** Once the last slide has played, the cells lose their marks and the live order goes. */
  private restAfter(cell: HTMLElement | undefined): void {
    window.clearTimeout(this.restTimer);
    this.restTimer = window.setTimeout(
      () => {
        for (const c of this.cells()) {
          delete c.dataset.colDrag;
          c.style.translate = '';
          c.style.transition = '';
        }
        this.table.current?.style.removeProperty(TRAVEL);
        this.setLive(null);
      },
      cell ? settleMs(cell) : 0,
    );
  }

  /**
   * After every render of the list: a new live order has moved the columns,
   * so each one starts from where it was painted and slides to its new place,
   * and the carried one is placed under the pointer again. Rows drawn since
   * the last render take the marks too.
   */
  afterRender(): void {
    const c = this.carried;
    const from = this.slideFrom;
    if (!c && !from) return;
    if (c) {
      for (const cell of this.cells()) {
        if (cell.dataset.colDrag) continue;
        const held = cell.dataset.cell === c.id;
        cell.dataset.colDrag = held ? 'held' : 'shift';
        if (held) cell.dataset.held = 'true';
      }
      this.boxes = this.measure();
      this.place(c);
    }
    if (!from) return;
    this.slideFrom = null;
    const moved: HTMLElement[] = [];
    for (const head of this.heads()) {
      const id = head.dataset.cell as ColumnId;
      const was = from.get(id);
      if (was === undefined || id === c?.id) continue;
      const dx = was - head.offsetLeft;
      if (dx === 0) continue;
      for (const cell of this.cells(id)) {
        cell.dataset.colDrag = 'shift';
        cell.style.transition = 'none';
        cell.style.translate = `${dx}px 0px`;
        moved.push(cell);
      }
    }
    if (moved.length === 0) return;
    // Reading layout commits the jump back, so the slide starts from there.
    void this.table.current?.offsetWidth;
    for (const cell of moved) {
      cell.style.transition = '';
      cell.style.translate = '0px 0px';
    }
    if (!c) this.restAfter(moved[0]);
  }
}
