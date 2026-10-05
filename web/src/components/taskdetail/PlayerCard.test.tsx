// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it } from 'vitest';

import type { Task, TaskFileHead } from '../../lib/api';
import { I18nProvider } from '../../lib/i18n';
import { en } from '../../lib/locales/en';
import { PlayerCard } from './PlayerCard';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
// jsdom plays nothing, and every case here is about a type a browser can play.
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
});

function task(over: Partial<Task>): Task {
  return {
    id: 't1',
    url: 'https://host.example/film.mkv',
    name: 'film.mkv',
    package: 'Film',
    resolver: 'direct',
    size: 64,
    loaded: 32,
    speed: 0,
    status: 'running',
    createdAt: '2026-09-25T10:00:00Z',
    priority: 0,
    position: 0,
    enabled: true,
    ...over,
  };
}

function head(over: Partial<TaskFileHead>): TaskFileHead {
  return { ok: true, status: 200, bytes: 64, contentType: 'video/x-matroska', ranges: true, ...over };
}

function render(t: Task, h: TaskFileHead | null) {
  act(() =>
    root.render(
      <I18nProvider>
        <PlayerCard task={t} base="/api" head={h} />
      </I18nProvider>,
    ),
  );
  return host.innerHTML;
}

it('shows the player for a torrent of several files when the server found media in it', () => {
  const season = task({ url: 'magnet:?xt=urn:btih:abc', name: 'Season 1', torrentFileCount: 12, torrentMedia: 'video' });
  expect(render(season, head({}))).toContain(en['detail.play']);
});

it('shows no player for a torrent of several files without audio or video', () => {
  const pack = task({ url: 'magnet:?xt=urn:btih:abc', name: 'Software.mkv', torrentFileCount: 3 });
  expect(render(pack, head({}))).toBe('');
});

it('says a download whose missing part is being fetched again plays once it is back', () => {
  const html = render(task({}), head({ ok: false, status: 503 }));
  expect(html).toContain(en['detail.playMending']);
  expect(html).not.toContain(en['detail.playStopped']);
});
