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

let root: Root;
let host: HTMLDivElement;

beforeEach(() => {
  vi.resetModules();
  // The interface state is an object, every list the card asks for an array.
  vi.stubGlobal(
    'fetch',
    vi.fn(
      async (url: string) =>
        new Response(url.includes('/api/uistate') ? '{}' : '[]', {
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

const linkMark = (id: string) => host.querySelector<HTMLButtonElement>(`[data-task-id="${id}"] > [role="checkbox"]`)!;
const packageMark = (name: string) =>
  host.querySelector<HTMLButtonElement>(`[data-package-row="${name}"] > [role="checkbox"]`)!;
const press = (el: HTMLElement, shiftKey = false) =>
  act(async () => {
    el.dispatchEvent(new MouseEvent('click', { bubbles: true, shiftKey }));
  });
const picked = () =>
  [...host.querySelectorAll('[data-task-id][aria-selected="true"]')].map((r) => r.getAttribute('data-task-id'));

describe("a list row's selection mark", () => {
  it('adds its row to the marking and takes it out again', async () => {
    await show();
    await press(linkMark('a'));
    await press(linkMark('c'));
    expect(picked()).toEqual(['a', 'c']);
    expect(linkMark('a').getAttribute('aria-checked')).toBe('true');
    expect(linkMark('b').getAttribute('aria-checked')).toBe('false');
    await press(linkMark('a'));
    expect(picked()).toEqual(['c']);
  });

  it('picks every row up to it on a Shift-click', async () => {
    await show();
    await press(linkMark('a'));
    await press(linkMark('c'), true);
    expect(picked()).toEqual(['a', 'b', 'c']);
  });

  it('picks all the links of a package from its header', async () => {
    await show();
    await press(packageMark('Films'));
    expect(picked()).toEqual(['a', 'b', 'c']);
    expect(packageMark('Films').getAttribute('aria-checked')).toBe('true');
    expect(packageMark('Music').getAttribute('aria-checked')).toBe('false');
  });

  it('shows on every row once something is picked', async () => {
    await show();
    const strip = host.querySelector('[role="tree"]')!;
    expect(strip.classList.contains('glim-selecting')).toBe(false);
    await press(linkMark('b'));
    expect(strip.classList.contains('glim-selecting')).toBe(true);
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
