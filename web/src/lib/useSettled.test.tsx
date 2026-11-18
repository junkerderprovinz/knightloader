// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { useSettled } from './useSettled';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;
let shown: number[] = [];

function Probe({ value }: { value: number }) {
  const settled = useSettled(value, 300);
  shown.push(settled);
  return null;
}

beforeEach(() => {
  vi.useFakeTimers();
  shown = [];
  host = document.createElement('div');
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  vi.useRealTimers();
});

it('shows a value that keeps changing only once it has stopped', () => {
  act(() => root.render(<Probe value={26} />));
  for (const v of [20, 14, 8, 2, 0]) {
    act(() => root.render(<Probe value={v} />));
    act(() => vi.advanceTimersByTime(100));
  }
  expect(new Set(shown)).toEqual(new Set([26]));
  act(() => vi.advanceTimersByTime(300));
  expect(shown[shown.length - 1]).toBe(0);
  expect(new Set(shown)).toEqual(new Set([26, 0]));
});

it('shows the first value at once', () => {
  act(() => root.render(<Probe value={5} />));
  expect(shown[0]).toBe(5);
});
