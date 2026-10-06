// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it } from 'vitest';

import type { ExtractJob, Task } from '../lib/api';
import { extractionsByTask } from './Archives';
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

const row = (id: string, over: Partial<Task> = {}): Task => ({
  id,
  url: `https://files.example/${id}`,
  name: `${id}.rar`,
  package: 'Vier.minus.drei',
  resolver: 'direct',
  size: 1000,
  loaded: 1000,
  speed: 0,
  status: 'done',
  createdAt: '2026-09-27T12:00:00Z',
  priority: 0,
  position: 0,
  enabled: true,
  ...over,
});

const job = (status: string, parts: string[], more: Partial<ExtractJob> = {}): ExtractJob => ({
  id: 'j',
  taskId: parts[0],
  name: 'a1.rar',
  dir: '/downloads',
  status,
  files: 0,
  bytes: 0,
  volumes: parts.length,
  parts,
  queuedAt: '2026-09-27T12:00:00Z',
  ...more,
});

const ctx = (...jobs: ExtractJob[]): CellContext => ({
  t: (key) => key,
  base: '/api',
  profile: 'downloads',
  extractions: extractionsByTask(jobs),
});

const progress = COLUMN_BY_ID.get('progress');

function cell(task: Task, context: CellContext) {
  act(() => root.render(<>{progress?.render(task, context)}</>));
  return bar();
}

function header(items: Task[], context: CellContext) {
  act(() => root.render(<>{progress?.aggregate?.(items, context)}</>));
  return bar();
}

function bar() {
  const track = host.querySelector('[role="progressbar"]');
  const fill = track?.firstElementChild as HTMLElement | null;
  return {
    value: track?.getAttribute('aria-valuenow'),
    label: host.textContent,
    striped: fill?.classList.contains('kl-bar-stripes') ?? false,
    colour: fill?.style.backgroundColor,
  };
}

it('draws a part of an archive being unpacked at the unpacking, striped', () => {
  const got = cell(row('a2'), ctx(job('running', ['a1', 'a2'], { unpacked: 450, size: 1000 })));
  expect(got.value).toBe('45');
  expect(got.label).toBe('45%');
  expect(got.striped).toBe(true);
  expect(got.colour).toBe('var(--accent)');
});

it('draws a finished download as finished once its archive is unpacked', () => {
  const got = cell(row('a1'), ctx(job('done', ['a1'], { unpacked: 1000, size: 1000 })));
  expect(got.value).toBe('100');
  expect(got.striped).toBe(false);
  expect(got.colour).toBe('var(--status-ok-solid)');
});

it('keeps a failed unpacking where it stopped, in the failure colour', () => {
  const got = cell(row('a1'), ctx(job('error', ['a1'], { unpacked: 300, size: 1000, error: 'bad block header' })));
  expect(got.value).toBe('30');
  expect(got.striped).toBe(true);
  expect(got.colour).toBe('var(--status-fail-solid)');
});

it('loops a striped bar while an archive that gives no size unpacks', () => {
  const got = cell(row('a1', { status: 'extracting' }), ctx(job('running', ['a1'])));
  expect(got.value).toBeNull();
  expect(got.striped).toBe(true);
});

it('draws the package header at its archive being unpacked', () => {
  const items = [row('a1', { status: 'extracting' }), row('a2'), row('notes', { name: 'notes.nfo' })];
  const got = header(items, ctx(job('running', ['a1', 'a2'], { unpacked: 700, size: 1000 })));
  expect(got.value).toBe('70');
  expect(got.striped).toBe(true);
});

it('draws the package header at its downloads while one still runs', () => {
  const items = [row('a1', { status: 'extracting' }), row('b', { status: 'running', loaded: 0 })];
  const got = header(items, ctx(job('running', ['a1'], { unpacked: 700, size: 1000 })));
  expect(got.value).toBe('50');
  expect(got.striped).toBe(false);
});

it('draws a finished file at its release par2 check while it reads or rebuilds, striped', () => {
  const verifying = cell(row('a1', { repair: { stage: 'verifying', progress: 0.4 } }), ctx());
  expect(verifying.value).toBe('40');
  expect(verifying.striped).toBe(true);
  const pkg = header([row('a1'), row('a2', { repair: { stage: 'repairing', progress: 0.6 } })], ctx());
  expect(pkg.value).toBe('60');
});

it('draws a finished file as finished while its check waits for the other files', () => {
  const got = cell(row('a1', { repair: { stage: 'waiting' } }), ctx());
  expect(got.value).toBe('100');
  expect(got.striped).toBe(false);
});
