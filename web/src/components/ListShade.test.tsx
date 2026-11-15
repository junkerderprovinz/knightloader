// @vitest-environment jsdom
import { readFileSync } from 'node:fs';
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Task, TorrentFileView } from '../lib/api';
import { rowBands, type ListRow } from './listRows';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};

let root: Root;
let host: HTMLDivElement;

const OPEN = JSON.stringify({ 'list.expanded.downloads': ['Films', 'Music'] });

beforeEach(() => {
  localStorage.clear();
  vi.resetModules();
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

async function show(groups: [string, Task[]][]): Promise<void> {
  const { TaskListCard } = await import('./TaskList');
  await act(async () => root.render(<TaskListCard groups={groups} base="/api" title="Downloads" />));
}

const FILMS = [task('a', 'Films'), task('b', 'Films'), task('c', 'Films')];
const MUSIC = [task('d', 'Music'), task('e', 'Music')];

const band = (el: Element | null) => el?.getAttribute('data-band');
const link = (id: string) => host.querySelector(`[data-task-id="${id}"]`);
const folder = (name: string) => host.querySelector(`[data-package-row="${name}"]`);
const tree = () => host.querySelector('[role="tree"]')!;

describe('the shaded list', () => {
  it('marks every package header as the head of its band', async () => {
    await show([
      ['Films', FILMS],
      ['Music', MUSIC],
    ]);
    expect(band(folder('Films'))).toBe('head');
    expect(band(folder('Music'))).toBe('head');
  });

  it('alternates the links of a package by their place in it', async () => {
    await show([['Films', FILMS]]);
    expect(['a', 'b', 'c'].map((id) => band(link(id)))).toEqual(['odd', 'even', 'odd']);
  });

  it('starts every package on the same shade, whatever the one above ended on', async () => {
    await show([
      ['Films', FILMS],
      ['Music', MUSIC],
    ]);
    expect(['d', 'e'].map((id) => band(link(id)))).toEqual(['odd', 'even']);
  });

  it('runs one package straight into the next, with no strip of ground between them', async () => {
    await show([
      ['Films', FILMS],
      ['Music', MUSIC],
    ]);
    expect(folder('Music')!.className).not.toContain('kl-band-seam');
  });

  it('shades the list until the switch is turned off, and again once it is on', async () => {
    await show([['Films', FILMS]]);
    expect(tree().hasAttribute('data-shade')).toBe(true);
    const { setListShade } = await import('../lib/listShade');
    await act(async () => setListShade(false));
    expect(tree().hasAttribute('data-shade')).toBe(false);
    await act(async () => setListShade(true));
    expect(tree().hasAttribute('data-shade')).toBe(true);
  });
});

describe('the shades', () => {
  // Vitest runs from web/.
  const css = readFileSync('src/index.css', 'utf8');
  /** How much of the text colour a band mixes in, in percent. A clear band mixes none. */
  const share = (band: string) => {
    const value = new RegExp(`--list-band-${band}:\\s*([^;]+);`).exec(css)?.[1].trim();
    if (value === 'transparent') return 0;
    const pct = /var\(--carbon-text\)\s+([\d.]+)%/.exec(value ?? '')?.[1];
    if (pct === undefined) throw new Error(`--list-band-${band} is no mix of the text colour: ${value}`);
    return Number(pct);
  };

  it('alternates the files barely, closer to a quiet zebra than to bands', () => {
    expect(share('even') - share('odd')).toBeGreaterThan(0);
    expect(share('even') - share('odd')).toBeLessThanOrEqual(2.5);
  });

  it('sets the package header only a hair above its files', () => {
    expect(share('head')).toBeGreaterThan(share('even'));
    expect(share('head') - share('even')).toBeLessThanOrEqual(2.5);
  });

  it('keeps the hover above every shade', () => {
    expect(share('hover')).toBeGreaterThan(share('head'));
  });

  it('leaves no gap between packages', () => {
    expect(css).not.toContain('--list-band-gap');
    expect(css).not.toContain('kl-band-seam');
  });
});

describe('rowBands', () => {
  const head = (name: string): ListRow => ({
    kind: 'package',
    key: `package:${name}`,
    name,
    items: [],
    collapsed: false,
    divider: false,
    level: 1,
    posinset: 1,
    setsize: 1,
  });
  const linkRow = (id: string, at: number): ListRow => ({
    kind: 'task',
    key: `task:${id}`,
    task: task(id, 'P'),
    index: at,
    level: 2,
    posinset: at,
    setsize: 9,
  });
  const fileRow = (id: string, at: number): ListRow => ({
    kind: 'file',
    key: `file:${id}:${at}`,
    task: task(id, 'P'),
    file: { path: `f${at}`, size: 1, selected: true } as TorrentFileView,
    index: 0,
    level: 3,
    posinset: at,
    setsize: 2,
  });

  it('counts a torrent file as a row of its package, so no two neighbours share a shade', () => {
    const rows = [head('P'), linkRow('a', 1), fileRow('a', 1), fileRow('a', 2), linkRow('b', 2)];
    expect(rowBands(rows)).toEqual(['head', 'odd', 'even', 'odd', 'even']);
  });

  it('goes by the drawn rows, so a link filtered out between two leaves them apart', () => {
    const rows = [head('P'), linkRow('a', 1), linkRow('c', 3)];
    expect(rowBands(rows)).toEqual(['head', 'odd', 'even']);
  });
});
