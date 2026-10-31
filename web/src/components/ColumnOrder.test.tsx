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

beforeEach(() => {
  vi.resetModules();
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

const task: Task = {
  id: 'a',
  url: 'https://files.example/a',
  name: 'a.zip',
  package: '',
  resolver: 'direct',
  size: 1000,
  loaded: 0,
  speed: 0,
  status: 'collected',
  createdAt: '2026-09-24T12:00:00Z',
  priority: 0,
  position: 0,
  enabled: true,
};

async function show(profile: 'downloads' | 'collector'): Promise<void> {
  const { TaskListCard } = await import('./TaskList');
  await act(async () =>
    root.render(
      <TaskListCard
        groups={[['', [task]]]}
        base="/api"
        title="List"
        profile={profile}
        selection={{ ids: new Set(), set: () => {}, toggle: () => {} }}
      />,
    ),
  );
}

const heads = () => [...host.querySelectorAll<HTMLButtonElement>('[data-col]')].map((b) => b.dataset.col);
const head = (id: string) => host.querySelector<HTMLButtonElement>(`[data-col="${id}"]`)!;

describe('the order of the columns', () => {
  it('puts the size last in the collector', async () => {
    await show('collector');
    expect(heads().slice(-1)).toEqual(['size']);
  });

  it('puts the size just before the progress bar in the download list', async () => {
    await show('downloads');
    expect(heads().slice(-2)).toEqual(['size', 'progress']);
  });

  it('moves the focused column one place with Alt and an arrow key, and keeps the focus on it', async () => {
    await show('collector');
    const before = heads();
    const at = before.indexOf('size');
    head('size').focus();
    await act(async () => {
      head('size').dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, key: 'ArrowLeft', altKey: true }));
    });
    const after = heads();
    expect(after.indexOf('size')).toBe(at - 1);
    expect(document.activeElement).toBe(head('size'));
  });

  it('leaves the order alone for an arrow key without Alt', async () => {
    await show('collector');
    const before = heads();
    await act(async () => {
      head('size').dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, key: 'ArrowLeft' }));
    });
    expect(heads()).toEqual(before);
  });
});
