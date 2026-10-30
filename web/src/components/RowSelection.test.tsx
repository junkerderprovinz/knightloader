// @vitest-environment jsdom
import { act, useState } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Task } from '../lib/api';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};
// jsdom has no pointer capture, and a press that never travels holds none.
Element.prototype.hasPointerCapture ??= () => false;

let root: Root;
let host: HTMLDivElement;

// Downloads folds a package until it is opened; these rows are about picking.
const OPEN = JSON.stringify({ 'list.expanded.downloads': ['Films', 'Music'] });

beforeEach(() => {
  vi.resetModules();
  // The interface state is an object, every list the card asks for an array.
  vi.stubGlobal(
    'fetch',
    vi.fn(
      async (url: string) =>
        new Response(url.includes('/api/uistate') ? OPEN : '[]', {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
    ),
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

const task = (id: string, pkg: string): Task => ({
  id,
  url: `https://files.example/${id}`,
  name: `${id}.zip`,
  package: pkg,
  resolver: 'direct',
  size: 1000,
  loaded: 0,
  speed: 0,
  status: 'queued',
  createdAt: '2026-09-24T12:00:00Z',
  priority: 0,
  position: 0,
  enabled: true,
});

const FILMS = [task('a', 'Films'), task('b', 'Films'), task('c', 'Films')];
const MUSIC = [task('d', 'Music')];

async function show(): Promise<void> {
  const { TaskListCard } = await import('./TaskList');
  function List() {
    const [ids, setIds] = useState<Set<string>>(new Set());
    return (
      <TaskListCard
        groups={[
          ['Films', FILMS],
          ['Music', MUSIC],
        ]}
        base="/api"
        title="Downloads"
        selection={{ ids, set: setIds, toggle: () => {} }}
      />
    );
  }
  await act(async () => root.render(<List />));
}

const link = (id: string) => host.querySelector<HTMLElement>(`[data-task-id="${id}"]`)!;
const folder = (name: string) => host.querySelector<HTMLElement>(`[data-package-row="${name}"]`)!;

/** A press let go where it went down, by a mouse with the given keys held or by a finger. */
const press = (el: HTMLElement, init: PointerEventInit = {}) =>
  act(async () => {
    const at = { bubbles: true, button: 0, isPrimary: true, pointerId: 1, pointerType: 'mouse', ...init };
    el.dispatchEvent(new PointerEvent('pointerdown', at));
    el.dispatchEvent(new PointerEvent('pointerup', at));
  });
const tap = (el: HTMLElement) => press(el, { pointerType: 'touch' });
const key = (el: HTMLElement, name: string) =>
  act(async () => {
    el.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, key: name }));
  });
const picked = () =>
  [...host.querySelectorAll('[data-task-id][aria-selected="true"]')].map((r) => r.getAttribute('data-task-id'));

describe('picking rows in the list', () => {
  it('draws no checkbox on any row', async () => {
    await show();
    expect(host.querySelector('[role="checkbox"]')).toBeNull();
    expect(link('a').getAttribute('aria-selected')).toBe('false');
  });

  it('picks the clicked row alone and fills it', async () => {
    await show();
    await press(link('a'));
    await press(link('c'));
    expect(picked()).toEqual(['c']);
    expect(link('c').classList.contains('glim-row-selected')).toBe(true);
    expect(link('a').classList.contains('glim-row-selected')).toBe(false);
  });

  it('adds a row with Ctrl held and takes it out again', async () => {
    await show();
    await press(link('a'));
    await press(link('c'), { ctrlKey: true });
    expect(picked()).toEqual(['a', 'c']);
    await press(link('a'), { ctrlKey: true });
    expect(picked()).toEqual(['c']);
  });

  it('picks every row up to the clicked one with Shift held', async () => {
    await show();
    await press(link('a'));
    await press(link('c'), { shiftKey: true });
    expect(picked()).toEqual(['a', 'b', 'c']);
  });

  it('picks all the links of a package from its header', async () => {
    await show();
    await press(folder('Films'));
    expect(picked()).toEqual(['a', 'b', 'c']);
    expect(folder('Films').getAttribute('aria-selected')).toBe('true');
    expect(folder('Music').getAttribute('aria-selected')).toBe('false');
  });

  it('flips a tapped row in and out of the marking, so a finger can pick several', async () => {
    await show();
    await tap(link('a'));
    await tap(link('c'));
    expect(picked()).toEqual(['a', 'c']);
    await tap(link('a'));
    expect(picked()).toEqual(['c']);
  });

  it('flips the focused row with Space', async () => {
    await show();
    await key(link('b'), ' ');
    expect(picked()).toEqual(['b']);
    await key(link('d'), ' ');
    expect(picked()).toEqual(['b', 'd']);
    await key(link('b'), ' ');
    expect(picked()).toEqual(['d']);
  });
});

describe("a list row's buttons", () => {
  it('stand in the cell that shows while the row is pointed at', async () => {
    await show();
    const row = host.querySelector('[data-task-id="a"]')!;
    const remove = row.querySelector('button[aria-label="Remove"]')!;
    expect(row.classList.contains('glim-row')).toBe(true);
    expect(remove.closest('.glim-row-actions')).not.toBeNull();
  });
});
