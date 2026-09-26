// @vitest-environment jsdom
import { act, createElement, useRef } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { nextFold, useRowFit } from './rowFit';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

describe('nextFold', () => {
  it('folds one more step while the content is wider than the row', () => {
    expect(nextFold(0, 3, 1200, 944)).toBe(1);
    expect(nextFold(2, 3, 960, 944, 1300)).toBe(3);
  });

  it('stays at the last step when even that does not fit', () => {
    expect(nextFold(3, 3, 1000, 400, 1000)).toBe(3);
  });

  it('unfolds once the content the step below asked for fits again', () => {
    expect(nextFold(2, 3, 880, 1600, 1310)).toBe(1);
    expect(nextFold(1, 3, 1310, 1600, 1623)).toBe(1);
  });

  it('keeps a step whose way back has not been measured', () => {
    expect(nextFold(1, 3, 800, 2000)).toBe(1);
  });
});

// jsdom lays nothing out, so each element reports the width in its data-w and
// the row the room in its data-room. The observer calls back only for what it
// watches, as a browser's does.
const watched = new Map<Element, Set<() => void>>();

class FakeResizeObserver {
  private readonly targets = new Set<Element>();
  constructor(private readonly callback: () => void) {}
  observe(el: Element) {
    this.targets.add(el);
    if (!watched.has(el)) watched.set(el, new Set());
    watched.get(el)!.add(this.callback);
  }
  disconnect() {
    for (const el of this.targets) watched.get(el)?.delete(this.callback);
    this.targets.clear();
  }
}

function resized(el: Element) {
  for (const cb of [...(watched.get(el) ?? [])]) cb();
}

function Row({ content }: { content: string }) {
  const ref = useRef<HTMLDivElement>(null);
  const level = useRowFit(ref, 1, content);
  return createElement(
    'div',
    { ref, 'data-room': '500', 'data-level': level },
    createElement('span', { id: 'grows', 'data-w': '300' }),
    createElement('span', { 'data-w': '100' }),
  );
}

let root: Root;
let host: HTMLDivElement;

beforeEach(() => {
  vi.stubGlobal('ResizeObserver', FakeResizeObserver);
  vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockImplementation(function (this: HTMLElement) {
    return Number(this.dataset.w ?? 0);
  });
  vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockImplementation(function (this: HTMLElement) {
    return Number(this.dataset.room ?? 0);
  });
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  watched.clear();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('useRowFit', () => {
  it('folds when a child grows while the row keeps its size', () => {
    act(() => root.render(createElement(Row, { content: 'a' })));
    const row = host.firstElementChild as HTMLElement;
    expect(row.dataset.level).toBe('0');

    const grows = host.querySelector<HTMLElement>('#grows')!;
    grows.dataset.w = '450';
    act(() => resized(grows));

    expect(row.dataset.level).toBe('1');
  });
});
