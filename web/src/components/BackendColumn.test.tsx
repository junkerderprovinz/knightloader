// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { renderToStaticMarkup } from 'react-dom/server';
import { afterEach, beforeEach, expect, it } from 'vitest';

import type { Task } from '../lib/api';
import { IconMagnet } from '../lib/icons';
import { COLUMN_BY_ID, type CellContext } from './columns';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

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

const row = (over: Partial<Task> = {}): Task => ({
  id: 'one',
  url: 'https://rapidgator.net/file/abc/show.mkv.html',
  name: 'show.mkv',
  package: 'Show',
  resolver: 'alldebrid',
  size: 1000,
  loaded: 10,
  speed: 0,
  status: 'running',
  createdAt: '2026-11-10T12:00:00Z',
  priority: 0,
  position: 0,
  enabled: true,
  ...over,
});

const ctx: CellContext = { t: (key) => key, base: '/api', profile: 'downloads' } as CellContext;

function cell(task: Task) {
  act(() => root.render(<>{COLUMN_BY_ID.get('resolver')?.render(task, ctx)}</>));
  return host;
}

const pathOf = (markup: string) => markup.match(/ d="([^"]+)"/)?.[1];

it('shows a debrid service by its mark and its name', () => {
  const out = cell(row());
  expect(out.querySelector('img')?.getAttribute('src')).toContain('/hosters/alldebrid.com.png');
  expect(out.textContent).toBe('AllDebrid');
});

it('names the service of an account slot, with the account', () => {
  const out = cell(row({ resolver: 'alldebrid#work' }));
  expect(out.querySelector('img')?.getAttribute('src')).toContain('/hosters/alldebrid.com.png');
  expect(out.textContent).toBe('AllDebrid (work)');
});

it('shows the service that fetched the files of a Usenet job', () => {
  const out = cell(row({ resolver: 'usenet', url: 'usenet://torbox/42/7/show.mkv' }));
  expect(out.querySelector('img')?.getAttribute('src')).toContain('/hosters/torbox.app.png');
  expect(out.textContent).toBe('TorBox');
});

it('shows the built-in torrent client by the magnet glyph', () => {
  const out = cell(row({ resolver: 'torrent', url: 'magnet:?xt=urn:btih:dd8255ecdc7ca55fb0bbf81323d87062db1f6d1c' }));
  expect(out.querySelector('img')).toBeNull();
  expect(out.querySelector('path')?.getAttribute('d')).toBe(pathOf(renderToStaticMarkup(<IconMagnet />)));
  expect(out.textContent).toBe('Built-in torrent client');
});

it('gives every built-in backend a glyph', () => {
  for (const resolver of ['jd', 'ytdlp', 'direct', 'http', 'nntp', 'mega', 'rapidgator', 'remotefs']) {
    const out = cell(row({ resolver }));
    expect(out.querySelector('svg'), resolver).not.toBeNull();
  }
});

it('keeps the mode of a hoster after its name', () => {
  expect(cell(row({ resolver: 'rapidgator', mode: 'premium' })).textContent).toBe('Rapidgator · Premium');
});

it('leaves a row no backend has taken yet blank', () => {
  const out = cell(row({ resolver: '', status: 'queued' }));
  expect(out.textContent).toBe('');
  expect(out.querySelector('svg, img')).toBeNull();
});
