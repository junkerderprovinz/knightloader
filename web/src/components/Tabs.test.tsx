// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { Tabs } from './Tabs';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

beforeEach(() => {
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      disconnect() {}
    },
  );
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

const items = [
  { id: 'running', label: 'Downloading', badge: 3 },
  { id: 'finished', label: 'Finished', badge: 5 },
];

function draw(onSelect: (id: string) => void, onClear: () => void) {
  act(() =>
    root.render(
      <Tabs
        select="many"
        size="sm"
        label="Quick filters"
        active={new Set(['finished'])}
        onSelect={onSelect}
        items={items}
        folded={{ icon: null, more: [{ id: 'clear', label: 'Show everything', onSelect: onClear }] }}
      />,
    ),
  );
}

function open() {
  const chip = [...host.querySelectorAll('button')].find((b) => b.textContent?.includes('Quick filters'))!;
  act(() => chip.click());
}

describe('a folded chip strip', () => {
  it('is one chip that opens its chips as switches, lit ones checked', () => {
    draw(() => {}, () => {});
    expect(host.querySelectorAll('button')).toHaveLength(1);
    open();
    const entries = [...document.querySelectorAll('[role=menuitemcheckbox]')];
    expect(entries.map((e) => [e.textContent, e.getAttribute('aria-checked')])).toEqual([
      ['Downloading3', 'false'],
      ['Finished5', 'true'],
    ]);
  });

  it('switches a chip from its menu and offers what stood beside the strip', () => {
    const onSelect = vi.fn();
    const onClear = vi.fn();
    draw(onSelect, onClear);
    open();
    const running = [...document.querySelectorAll<HTMLButtonElement>('[role=menuitemcheckbox]')][0];
    act(() => running.click());
    expect(onSelect).toHaveBeenCalledWith('running');

    open();
    const clear = [...document.querySelectorAll<HTMLButtonElement>('[role=menuitem]')].find(
      (e) => e.textContent === 'Show everything',
    )!;
    act(() => clear.click());
    expect(onClear).toHaveBeenCalled();
  });
});
