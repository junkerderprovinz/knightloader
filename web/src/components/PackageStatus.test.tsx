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
