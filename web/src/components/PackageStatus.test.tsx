// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it } from 'vitest';

import type { ExtractJob, Task } from '../lib/api';
import { I18nProvider } from '../lib/i18n';
import { en } from '../lib/locales/en';
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
  createdAt: '2026-09-24T12:00:00Z',
  priority: 0,
  position: 0,
  enabled: true,
  ...over,
});

const ctx: CellContext = {
  t: (key, vars) => Object.entries(vars ?? {}).reduce<string>((s, [k, v]) => s.replaceAll(`{${k}}`, String(v)), en[key]),
  base: '/api',
  profile: 'downloads',
};

function header(items: Task[], context: CellContext = ctx): string {
  const status = COLUMN_BY_ID.get('status');
  act(() => root.render(<I18nProvider>{status?.aggregate?.(items, context)}</I18nProvider>));
  return host.textContent ?? '';
}

const flagged = (n: number) => host.querySelector(`[aria-label="${en['task.packageFailed'].replace('{n}', String(n))}"]`);

it('shows a package that is still downloading as running, with its failure flagged', () => {
  const text = header([row('a', { status: 'running', loaded: 10 }), row('b', { status: 'error', error: 'x' })]);
  expect(text).toContain(en['status.running']);
  expect(flagged(1)).not.toBeNull();
});

it('shows a package whose archive did not unpack as failed, without a second flag', () => {
  const text = header([
    row('a1', { unpack: 'error', archivePart: 1, error: 'extract: a1.rar: rardecode: bad block header' }),
    row('a2', { unpack: 'error', archivePart: 2 }),
  ]);
  expect(text).toContain(en['archive.failed']);
  expect(flagged(1)).toBeNull();
});

it('shows a package with a dead link and nothing left to do as an error', () => {
  const text = header([row('a'), row('b', { status: 'error', error: 'x' })]);
  expect(text).toContain(en['status.error']);
  expect(text).not.toContain(en['status.done']);
});

it('flags an archive that failed while another download in the package still runs', () => {
  header([row('a1', { unpack: 'error', archivePart: 1, error: 'x' }), row('b', { status: 'running', loaded: 1 })]);
  expect(flagged(1)).not.toBeNull();
});

it('shows a package still unpacking as unpacking, with an archive that failed flagged', () => {
  const job = (id: string, status: string, taskId: string): ExtractJob => ({
    id,
    taskId,
    name: `${taskId}.rar`,
    dir: '/downloads',
    status,
    files: 0,
    bytes: 0,
    volumes: 1,
    parts: [taskId],
    queuedAt: '2026-09-24T12:00:00Z',
  });
  const text = header([row('movie', { status: 'extracting' }), row('subs', { unpack: 'error', error: 'extract: x' })], {
    ...ctx,
    extractions: extractionsByTask([job('j1', 'running', 'movie'), job('j2', 'error', 'subs')]),
  });
  expect(text).toContain(en['status.extracting']);
  expect(text).not.toContain(en['archive.failed']);
  expect(flagged(1)).not.toBeNull();
});

const torrent = (id: string, over: Partial<Task> = {}) =>
  row(id, { resolver: 'torrent', seeding: true, uploaded: 5000, ratio: 5, ...over });

function cell(task: Task): string {
  const status = COLUMN_BY_ID.get('status');
  act(() => root.render(<I18nProvider>{status?.render(task, ctx)}</I18nProvider>));
  return host.textContent ?? '';
}

it('shows a finished torrent that still uploads as seeding', () => {
  const text = cell(torrent('t'));
  expect(text).toContain(en['status.seeding']);
  expect(text).not.toContain(en['status.done']);
});

it('shows a torrent still downloading as leeching and a plain download as downloading', () => {
  const leeching = cell(torrent('t', { status: 'running', seeding: false, loaded: 10 }));
  expect(leeching).toContain(en['status.leeching']);
  expect(leeching).not.toContain(en['status.running']);
  expect(cell(row('d', { status: 'running', loaded: 10 }))).toContain(en['status.running']);
});

it('shows a torrent that stopped seeding as done', () => {
  expect(cell(torrent('t', { seeding: false }))).toContain(en['status.done']);
});

it('says seeding over an archive the torrent already unpacked', () => {
  expect(cell(torrent('t', { unpack: 'done' }))).toContain(en['status.seeding']);
});

it('still names an archive that failed to unpack while the torrent seeds', () => {
  const text = cell(torrent('t', { unpack: 'error', error: 'extract: t.rar: bad block header' }));
  expect(text).toContain(en['archive.failed']);
});

it('counts a seeding torrent as seeding in the package header, not as done', () => {
  const text = header([row('a'), torrent('b')]);
  expect(text).toContain(en['status.seeding']);
  expect(text).not.toContain(en['status.done']);
});

it('keeps a package of a seeding torrent and a dead link on seeding, with the failure flagged', () => {
  const text = header([torrent('a'), row('b', { status: 'error', error: 'x' })]);
  expect(text).toContain(en['status.seeding']);
  expect(flagged(1)).not.toBeNull();
});

it('shows a finished file whose release is under its par2 check as verifying, with how far it got', () => {
  const text = cell(row('a', { repair: { stage: 'verifying', progress: 0.4 } }));
  expect(text).toContain(en['repair.verifyingAt'].replace('{percent}', '40%'));
  expect(text).not.toContain(en['status.done']);
  expect(cell(row('a', { repair: { stage: 'waiting' } }))).toContain(en['status.verifying']);
});

it('shows a release fetching recovery files or rebuilding blocks as repairing', () => {
  expect(cell(row('a', { repair: { stage: 'fetching', damaged: 3, recovery: 4 } }))).toContain(en['status.repairing']);
  const text = cell(row('a', { repair: { stage: 'repairing', progress: 0.25, damaged: 3 } }));
  expect(text).toContain(en['repair.repairingAt'].replace('{percent}', '25%'));
});

it('keeps a package on verifying while one of its files waits for the check', () => {
  const text = header([row('a'), row('b', { repair: { stage: 'waiting' } })]);
  expect(text).toContain(en['status.verifying']);
  expect(text).not.toContain(en['status.done']);
});
