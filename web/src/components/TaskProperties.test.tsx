// @vitest-environment jsdom
import { act } from 'react';
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
/** Every body the panel posted, by path. */
let posted: Record<string, unknown[]>;

const reply = (body: unknown, status = 200) =>
  new Response(status === 204 ? null : JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });

beforeEach(() => {
  vi.resetModules();
  posted = {};
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      const path = new URL(url, 'http://kl.test').pathname;
      if (init?.method === 'POST') (posted[path] ??= []).push(JSON.parse(String(init.body)));
      switch (path) {
        case '/api/tasks/backends':
          return reply([{ id: 'torbox', label: 'TorBox' }, { id: 'debridlink', label: 'Debrid-Link' }, { id: 'jd' }]);
        case '/api/tasks/options':
          return reply(null, 204);
        default:
          return reply([]);
      }
    }),
  );
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

const task: Task = {
  id: 't1',
  url: 'https://rapidgator.net/file/abc',
  name: 'Cloud.Atlas.part1.rar',
  package: 'Cloud Atlas',
  resolver: 'jd',
  mode: 'free',
  size: 0,
  loaded: 0,
  speed: 0,
  status: 'paused',
  createdAt: '2026-09-03T18:00:00Z',
  priority: 0,
  position: 0,
  enabled: true,
};

async function openPanel(tasks: Task[]) {
  const { TaskProperties } = await import('./TaskList');
  await act(async () => root.render(<TaskProperties ids={tasks.map((x) => x.id)} tasks={tasks} base="/api" />));
}

const backendTrigger = () =>
  [...host.querySelectorAll<HTMLButtonElement>('button')].find((b) => b.getAttribute('aria-label')?.startsWith('Backend:'))!;

describe('TaskProperties', () => {
  it('pins the selection to a backend the server offers', async () => {
    await openPanel([task]);
    expect(backendTrigger().getAttribute('aria-label')).toBe('Backend: Automatic');

    await act(async () => backendTrigger().click());
    const item = [...document.querySelectorAll<HTMLElement>('[role="menuitemradio"]')].find(
      (el) => el.textContent === 'Debrid-Link',
    )!;
    expect(item).toBeDefined();
    await act(async () => item.click());
    expect(backendTrigger().getAttribute('aria-label')).toBe('Backend: Debrid-Link');

    const save = [...host.querySelectorAll('button')].find((b) => b.textContent === 'Save')!;
    await act(async () => save.click());
    expect(posted['/api/tasks/backends']).toEqual([{ ids: ['t1'] }]);
    expect(posted['/api/tasks/options']).toEqual([{ ids: ['t1'], resolver: 'debridlink' }]);
  });

  it('sends no pin when the dropdown was left alone', async () => {
    await openPanel([{ ...task, resolverPin: 'torbox' }]);
    expect(backendTrigger().getAttribute('aria-label')).toBe('Backend: TorBox');

    const comment = host.querySelector('textarea')!;
    const setValue = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!;
    await act(async () => {
      setValue.call(comment, 'mirror of the other part');
      comment.dispatchEvent(new Event('input', { bubbles: true }));
    });
    const save = [...host.querySelectorAll('button')].find((b) => b.textContent === 'Save')!;
    await act(async () => save.click());
    expect(posted['/api/tasks/options']).toEqual([{ ids: ['t1'], comment: 'mirror of the other part' }]);
  });

  it('shows rows pinned differently as several values', async () => {
    await openPanel([
      { ...task, resolverPin: 'torbox' },
      { ...task, id: 't2', resolverPin: '' },
    ]);
    expect(backendTrigger().getAttribute('aria-label')).toBe('Backend: Several values');
  });
});
