// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it } from 'vitest';

import type { Task } from '../lib/api';
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

const torrent = (over: Partial<Task> = {}): Task => ({
  id: 'bbb',
  url: 'magnet:?xt=urn:btih:dd8255ecdc7ca55fb0bbf81323d87062db1f6d1c',
  name: 'Big Buck Bunny',
  package: 'Big Buck Bunny',
  resolver: 'torrent',
  size: 1000,
  loaded: 1000,
  speed: 0,
  status: 'done',
  createdAt: '2026-10-10T12:00:00Z',
  priority: 0,
  position: 0,
  enabled: true,
  ...over,
});

const ctx: CellContext = { t: (key) => key, base: '/api', profile: 'downloads' } as CellContext;
const speed = COLUMN_BY_ID.get('speed');

function cell(task: Task) {
  act(() => root.render(<>{speed?.render(task, ctx)}</>));
  return host;
}

function header(items: Task[]) {
  act(() => root.render(<>{speed?.aggregate?.(items, ctx)}</>));
  return host;
}

// The status pill beside the cell already says the row seeds.
const uploadRate = (out: HTMLElement) => out.querySelector('[data-new="seed-rate"]');

it('shows what a seeding torrent uploads, with no glyph in front', () => {
  const out = cell(torrent({ seeding: true, uploadSpeed: 2000 }));
  expect(uploadRate(out)?.textContent).toBe('2.0 kB/s');
  expect(out.querySelector('svg')).toBeNull();
});

it('shows 0 B/s for a seed nobody takes from', () => {
  expect(cell(torrent({ seeding: true })).textContent).toContain('0 B/s');
});

it('shows nothing for a finished torrent that no longer seeds', () => {
  const out = cell(torrent({ seedingOver: true, uploadSpeed: 2000 }));
  expect(out.textContent).toBe('');
  expect(uploadRate(out)).toBeNull();
});

it('keeps the download speed for a torrent still downloading', () => {
  const out = cell(torrent({ status: 'running', loaded: 10, speed: 4000, uploadSpeed: 2000 }));
  expect(out.textContent).toContain('4.0 kB/s');
  expect(uploadRate(out)).toBeNull();
});

it('sums the uploads of a package that only seeds', () => {
  const out = header([
    torrent({ id: 'a', seeding: true, uploadSpeed: 1000 }),
    torrent({ id: 'b', seeding: true, uploadSpeed: 1000 }),
  ]);
  expect(uploadRate(out)?.textContent).toBe('2.0 kB/s');
});

it('sorts seeding rows by their upload', () => {
  const slow = torrent({ id: 'slow', seeding: true, uploadSpeed: 10 });
  const fast = torrent({ id: 'fast', seeding: true, uploadSpeed: 500 });
  expect(speed?.compare?.(slow, fast)).toBeLessThan(0);
});
