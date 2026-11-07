// @vitest-environment jsdom
import { act, useRef, useState } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { dropColumn, liveColumnOrder, useColumnDrag, type ColumnBox } from './columnDrag';
import type { ColumnId } from './columns';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

describe('the live order while a column is carried', () => {
  // name 0..300, size 300..380, speed 380..480, eta 480..560
  const boxes: ColumnBox[] = [
    { id: 'name', x: 0, w: 300 },
    { id: 'size', x: 300, w: 80 },
    { id: 'speed', x: 380, w: 100 },
    { id: 'eta', x: 480, w: 80 },
  ];

  it('keeps a column in place until the leading edge of the carried one passes its middle', () => {
    expect(liveColumnOrder(boxes, 'size', 349, false)).toEqual(['name', 'size', 'speed', 'eta']);
    expect(liveColumnOrder(boxes, 'size', 351, false)).toEqual(['name', 'speed', 'size', 'eta']);
  });

  it('passes several columns at once when the pointer jumps', () => {
    expect(liveColumnOrder(boxes, 'size', 480, false)).toEqual(['name', 'speed', 'eta', 'size']);
    expect(liveColumnOrder(boxes, 'eta', 0, false)).toEqual(['eta', 'name', 'size', 'speed']);
  });

  it('brings a wide column to either end once it reaches the edge', () => {
    // name moved to the middle: size 0..80, speed 80..180, name 180..480, eta 480..560
    const middle: ColumnBox[] = [
      { id: 'size', x: 0, w: 80 },
      { id: 'speed', x: 80, w: 100 },
      { id: 'name', x: 180, w: 300 },
      { id: 'eta', x: 480, w: 80 },
    ];
    expect(liveColumnOrder(middle, 'name', 0, false)).toEqual(['name', 'size', 'speed', 'eta']);
    expect(liveColumnOrder(middle, 'name', 260, false)).toEqual(['size', 'speed', 'eta', 'name']);
  });

  it('reads the columns from the right in a right-to-left list', () => {
    // Laid out from the right: name 260..560, size 180..260, speed 80..180, eta 0..80.
    const rtl: ColumnBox[] = [
      { id: 'name', x: 260, w: 300 },
      { id: 'size', x: 180, w: 80 },
      { id: 'speed', x: 80, w: 100 },
      { id: 'eta', x: 0, w: 80 },
    ];
    expect(liveColumnOrder(rtl, 'size', 131, true)).toEqual(['name', 'size', 'speed', 'eta']);
    expect(liveColumnOrder(rtl, 'size', 129, true)).toEqual(['name', 'speed', 'size', 'eta']);
  });

  it('drops among the columns on show and leaves a hidden one where it was', () => {
    const order: ColumnId[] = ['name', 'size', 'added', 'speed', 'eta'];
    expect(dropColumn(order, ['name', 'speed', 'eta', 'size'], 'size')).toEqual(['name', 'added', 'speed', 'eta', 'size']);
    expect(dropColumn(order, ['speed', 'name', 'size', 'eta'], 'speed')).toEqual(['speed', 'name', 'size', 'added', 'eta']);
  });
});

// jsdom lays nothing out, so every cell is given the width of its column and
// stands after the cells before it in its row.
const WIDTH: Partial<Record<ColumnId, number>> = { name: 300, size: 80, speed: 100, eta: 80 };
const widthOf = (el: HTMLElement) => (el.dataset.cell ? (WIDTH[el.dataset.cell as ColumnId] ?? 0) : 0);

function cellsBefore(el: HTMLElement): HTMLElement[] {
  const all = Array.from(el.parentElement?.children ?? []) as HTMLElement[];
  return all.slice(0, all.indexOf(el)).filter((c) => c.dataset.cell);
}

let root: Root;
let host: HTMLDivElement;

beforeEach(() => {
  Object.defineProperty(HTMLElement.prototype, 'offsetWidth', {
    configurable: true,
    get(this: HTMLElement) {
      return widthOf(this);
    },
  });
  Object.defineProperty(HTMLElement.prototype, 'offsetLeft', {
    configurable: true,
    get(this: HTMLElement) {
      return cellsBefore(this).reduce((x, c) => x + widthOf(c), 0);
    },
  });
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  delete (HTMLElement.prototype as { offsetWidth?: number }).offsetWidth;
  delete (HTMLElement.prototype as { offsetLeft?: number }).offsetLeft;
  vi.useRealTimers();
});

function Table({ onDrop }: { onDrop: (held: ColumnId, live: ColumnId[]) => void }) {
  const [ids, setIds] = useState<ColumnId[]>(['name', 'size', 'speed', 'eta']);
  const table = useRef<HTMLDivElement>(null);
  const drag = useColumnDrag({
    ids,
    table,
    onDrop: (held, live) => {
      onDrop(held, live);
      setIds(live);
    },
  });
  return (
    <div ref={table}>
      <div data-row="head">
        {drag.order.map((id) => (
          <button key={id} data-cell={id} onPointerDown={(e) => drag.press(e, id)}>
            {id}
          </button>
        ))}
      </div>
      <div data-row="cells">
        {drag.order.map((id) => (
          <span key={id} data-cell={id} />
        ))}
      </div>
    </div>
  );
}

const order = (row: string) =>
  Array.from(host.querySelectorAll<HTMLElement>(`[data-row="${row}"] > [data-cell]`)).map((c) => c.dataset.cell);

const pointer = (type: string, x: number, target: EventTarget = document) =>
  act(async () => {
    target.dispatchEvent(new PointerEvent(type, { bubbles: true, clientX: x, clientY: 10, button: 0, pointerType: 'mouse' }));
  });

async function carrySize(to: number): Promise<void> {
  const head = host.querySelector<HTMLElement>('[data-row="head"] [data-cell="size"]')!;
  await pointer('pointerdown', 340, head);
  await pointer('pointermove', 350);
  await pointer('pointermove', to);
}

describe('carrying a column by its head', () => {
  it('moves head and cells live as the pointer passes a middle, and writes once on the drop', async () => {
    vi.useFakeTimers();
    const onDrop = vi.fn();
    await act(async () => root.render(<Table onDrop={onDrop} />));

    // The size column's centre starts at 340; past speed's middle at 430 it
    // takes speed's place, past eta's at 520 the last one.
    await carrySize(440);
    expect(order('head')).toEqual(['name', 'speed', 'size', 'eta']);
    expect(order('cells')).toEqual(['name', 'speed', 'size', 'eta']);
    await pointer('pointermove', 540);
    expect(order('head')).toEqual(['name', 'speed', 'eta', 'size']);
    expect(order('cells')).toEqual(['name', 'speed', 'eta', 'size']);
    expect(onDrop).not.toHaveBeenCalled();

    await pointer('pointerup', 540);
    await act(async () => vi.runAllTimers());
    expect(onDrop).toHaveBeenCalledTimes(1);
    expect(onDrop).toHaveBeenCalledWith('size', ['name', 'speed', 'eta', 'size']);
    expect(order('head')).toEqual(['name', 'speed', 'eta', 'size']);
    expect(host.querySelector('[data-col-drag]')).toBeNull();
  });

  it('puts every column back on Escape and writes nothing', async () => {
    vi.useFakeTimers();
    const onDrop = vi.fn();
    await act(async () => root.render(<Table onDrop={onDrop} />));

    await carrySize(540);
    expect(order('head')).toEqual(['name', 'speed', 'eta', 'size']);
    await act(async () => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    });
    expect(order('head')).toEqual(['name', 'size', 'speed', 'eta']);
    expect(order('cells')).toEqual(['name', 'size', 'speed', 'eta']);

    await pointer('pointerup', 540);
    await act(async () => vi.runAllTimers());
    expect(onDrop).not.toHaveBeenCalled();
    expect(order('head')).toEqual(['name', 'size', 'speed', 'eta']);
  });

  it('writes nothing for a drop where the column started', async () => {
    const onDrop = vi.fn();
    await act(async () => root.render(<Table onDrop={onDrop} />));
    await carrySize(370);
    await pointer('pointerup', 370);
    expect(onDrop).not.toHaveBeenCalled();
  });

  it('leaves a press that never travels to the click', async () => {
    const onDrop = vi.fn();
    await act(async () => root.render(<Table onDrop={onDrop} />));
    const head = host.querySelector<HTMLElement>('[data-row="head"] [data-cell="size"]')!;
    await pointer('pointerdown', 340, head);
    await pointer('pointermove', 342);
    await pointer('pointerup', 342);
    expect(host.querySelector('[data-col-drag]')).toBeNull();
    expect(onDrop).not.toHaveBeenCalled();
  });
});
