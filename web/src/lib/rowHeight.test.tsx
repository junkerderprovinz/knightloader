// @vitest-environment jsdom
import { act, useRef } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it } from 'vitest';

import type { Task } from './api';
import { ROW_ESTIMATES, ROW_HEIGHTS, ROW_METRICS, type RowHeight } from './rowHeight';
import { useRowWindow, type ListRow, type RowWindow } from '../components/listRows';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

beforeEach(() => {
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

it('makes Compact the new height, Comfortable the old one and Medium the one between', () => {
  const row = (h: RowHeight) => ROW_METRICS[h].control + 2 * ROW_METRICS[h].pad;
  expect(row('compact')).toBe(28);
  expect(row('comfortable')).toBe(48);
  expect(row('medium')).toBe((row('compact') + row('comfortable')) / 2);
});

// Enough rows for the window to switch on, none of them ever measured: jsdom
// lays nothing out, so every offset is the estimate the step handed in.
const rows: ListRow[] = Array.from({ length: 400 }, (_, i) => ({
  kind: 'task',
  key: `task:${i}`,
  task: { id: String(i) } as Task,
  index: i,
  level: 2,
  posinset: i + 1,
  setsize: 400,
}));

function Probe({ step, out }: { step: RowHeight; out: { win?: RowWindow } }) {
  const strip = useRef<HTMLDivElement>(null);
  out.win = useRowWindow(rows, strip, ROW_ESTIMATES[step]);
  return <div ref={strip} />;
}

it.each(ROW_HEIGHTS)('places the rows %s is not yet measured at its own estimate', (step) => {
  const out: { win?: RowWindow } = {};
  act(() => root.render(<Probe step={step} out={out} />));
  const { control, pad } = ROW_METRICS[step];
  expect(out.win?.heightOf(10)).toBe(control + 2 * pad);
  expect(out.win?.topOf(100)).toBe(100 * (control + 2 * pad));
});

it('starts over with the new estimate when the step changes', () => {
  const out: { win?: RowWindow } = {};
  act(() => root.render(<Probe step="compact" out={out} />));
  act(() => root.render(<Probe step="comfortable" out={out} />));
  expect(out.win?.topOf(10)).toBe(10 * ROW_ESTIMATES.comfortable.task);
});
