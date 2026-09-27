// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Task, TorrentFileView } from '../lib/api';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};

let root: Root;
let host: HTMLDivElement;
let files: TorrentFileView[];
/** Every body posted to the file route. */
let chosen: unknown[];

const reply = (body: unknown) =>
  new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } });

beforeEach(() => {
  vi.resetModules();
  chosen = [];
  files = [
    { path: 'Season 1/e01.mkv', size: 700, selected: true, done: 350 },
    { path: 'Season 1/e02.mkv', size: 700, selected: false },
    { path: 'sample.mkv', size: 30, selected: false },
  ];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      const path = new URL(url, 'http://kl.test').pathname;
      if (path === '/api/tasks/t1/torrent-files') {
        if (init?.method === 'POST') {
          const want = (JSON.parse(String(init.body)) as { selectedPaths: string[] }).selectedPaths;
          chosen.push(want);
          files = files.map((f) => ({ ...f, selected: want.includes(f.path) }));
        }
        return reply(files);
      }
      return reply([]);
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

const torrent: Task = {
  id: 't1',
  url: 'magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567',
  name: 'Show',
  package: 'Show',
  resolver: 'torrent',
  size: 700,
  loaded: 350,
  speed: 0,
  status: 'paused',
  createdAt: '2026-09-03T18:00:00Z',
  priority: 0,
  position: 0,
  enabled: true,
  torrentFileCount: 3,
};

async function show(task: Task) {
  const { TaskListCard } = await import('./TaskList');
  await act(async () =>
    root.render(<TaskListCard groups={[['Show', [task]]]} base="/api" title="Downloads" profile="downloads" />),
  );
}

const fileRows = () => [...host.querySelectorAll<HTMLElement>('[data-row-kind="file"]')];
const twisty = () =>
  host.querySelector<HTMLButtonElement>('[data-task-id="t1"] button[aria-expanded]')!;

describe('a torrent row', () => {
  it('opens onto one row per file, each with its path and switch', async () => {
    await show(torrent);
    expect(fileRows()).toHaveLength(0);
    await act(async () => twisty().click());
    const rows = fileRows();
    expect(rows).toHaveLength(3);
    expect(rows[0].textContent).toContain('Season 1/e01.mkv');
    expect(rows[0].getAttribute('aria-level')).toBe('3');
    expect(rows.map((r) => r.querySelector('[role="switch"]')?.getAttribute('aria-checked'))).toEqual([
      'true',
      'false',
      'false',
    ]);
    // A file is not a task: nothing a right-click or a remove looks for.
    expect(rows[0].closest('[data-task-id]')).toBeNull();
  });

  it('sends the whole new choice when a file is switched on', async () => {
    await show(torrent);
    await act(async () => twisty().click());
    await act(async () => fileRows()[2].querySelector<HTMLButtonElement>('[role="switch"]')!.click());
    expect(chosen).toEqual([['Season 1/e01.mkv', 'sample.mkv']]);
    expect(fileRows()[2].querySelector('[role="switch"]')?.getAttribute('aria-checked')).toBe('true');
  });

  it('keeps the last file on without asking the server', async () => {
    await show(torrent);
    await act(async () => twisty().click());
    await act(async () => fileRows()[0].querySelector<HTMLButtonElement>('[role="switch"]')!.click());
    expect(chosen).toEqual([]);
  });

  it('shows a finished torrent\'s files without switches', async () => {
    await show({ ...torrent, status: 'done' });
    await act(async () => twisty().click());
    expect(fileRows()).toHaveLength(3);
    expect(host.querySelector('[data-row-kind="file"] [role="switch"]')).toBeNull();
  });
});
