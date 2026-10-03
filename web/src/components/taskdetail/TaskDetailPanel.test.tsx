// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import type { Task } from '../../lib/api';
import { I18nProvider } from '../../lib/i18n';
import { en } from '../../lib/locales/en';
import { TaskDetailPanel } from './TaskDetailPanel';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
HTMLMediaElement.prototype.canPlayType = () => 'maybe';

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
  vi.unstubAllGlobals();
});

function task(loaded: number): Task {
  return {
    id: 't1',
    url: 'https://host.example/film.mkv',
    name: 'film.mkv',
    package: 'Film',
    resolver: 'direct',
    size: 64,
    loaded,
    speed: 0,
    status: 'running',
    createdAt: '2026-09-25T10:00:00Z',
    priority: 0,
    position: 0,
    enabled: true,
  };
}

async function render(t: Task): Promise<string> {
  await act(async () => {
    root.render(
      <MemoryRouter>
        <I18nProvider>
          <TaskDetailPanel task={t} base="/api" />
        </I18nProvider>
      </MemoryRouter>,
    );
    await new Promise((r) => setTimeout(r, 0));
  });
  return host.innerHTML;
}

it('asks for the file again once a running download has its first bytes', async () => {
  let streaming = false;
  let heads = 0;
  vi.stubGlobal(
    'fetch',
    vi.fn((_url: string, init?: RequestInit) => {
      if (init?.method !== 'HEAD') return new Promise(() => {});
      heads++;
      return Promise.resolve(
        streaming
          ? new Response(null, { status: 200, headers: { 'Content-Type': 'video/x-matroska', 'Content-Length': '64' } })
          : new Response(null, { status: 404 }),
      );
    }),
  );

  // Running, but not yet streamed by the server.
  expect(await render(task(0))).toContain(en['detail.playNoFile']);

  streaming = true;
  const html = await render(task(16));
  expect(html).not.toContain(en['detail.playNoFile']);
  expect(html).toContain(en['detail.playLive']);

  await render(task(32));
  expect(heads).toBe(2);
});
