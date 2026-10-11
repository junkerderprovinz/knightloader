// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import type { PieceMap, Task } from '../../lib/api';
import { I18nProvider } from '../../lib/i18n';
import { PiecesCard } from './PiecesCard';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;
let asked: string[];

/** What the server answers for a task's pieces. */
function serverKnows(map: PieceMap | null) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string) => {
      asked.push(url);
      return new Response(JSON.stringify(map), { status: 200, headers: { 'Content-Type': 'application/json' } });
    }),
  );
}

beforeEach(() => {
  asked = [];
  vi.stubGlobal('matchMedia', () => ({ matches: true, addEventListener() {}, removeEventListener() {} }));
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

const task = (over: Partial<Task>): Task =>
  ({
    id: 't1',
    url: 'https://host.example/film.mkv',
    name: 'film.mkv',
    package: '',
    resolver: 'direct',
    size: 1000,
    loaded: 400,
    speed: 10,
    status: 'running',
    createdAt: '2026-09-25T10:00:00Z',
    priority: 0,
    position: 0,
    enabled: true,
    ...over,
  }) as Task;

async function show(t: Task, base = '/api') {
  await act(async () => {
    root.render(
      <I18nProvider>
        <PiecesCard task={t} base={base} />
      </I18nProvider>,
    );
    await new Promise((r) => setTimeout(r, 0));
  });
}

const twoConnections: PieceMap = {
  unit: 'bytes',
  total: 1000,
  runs: [
    { state: 'done', len: 300 },
    { state: 'missing', len: 200 },
    { state: 'done', len: 100 },
    { state: 'missing', len: 400 },
  ],
  heads: [300, 600],
};

const card = () => host.querySelector('[data-new="piece-map"]');
const heads = () => host.querySelectorAll('[data-head]').length;

it('draws the pieces of a running download with a head for each connection', async () => {
  serverKnows(twoConnections);
  await show(task({}));
  expect(asked).toEqual(['/api/tasks/t1/pieces']);
  expect(card()).not.toBeNull();
  expect(heads()).toBe(2);
  expect(host.textContent).toContain('Connections writing: 2');
});

it('asks the instance the task is on', async () => {
  serverKnows(twoConnections);
  await show(task({}), '/api/instances/nas');
  expect(asked).toEqual(['/api/instances/nas/tasks/t1/pieces']);
});

it('draws nothing where the server knows no pieces', async () => {
  serverKnows(null);
  await show(task({}));
  expect(card()).toBeNull();
});

it('does not ask about a torrent, whose pieces stay inside its client', async () => {
  serverKnows(twoConnections);
  await show(task({ resolver: 'torrent' }));
  expect(asked).toEqual([]);
  expect(card()).toBeNull();
});

it('does not ask about a download that is neither loading nor paused', async () => {
  serverKnows(twoConnections);
  await show(task({ status: 'queued' }));
  await show(task({ status: 'done' }));
  expect(asked).toEqual([]);
});

it('keeps the band of a paused download and takes the heads off it', async () => {
  serverKnows(twoConnections);
  await show(task({}));
  await show(task({ status: 'paused' }));
  expect(card()).not.toBeNull();
  expect(heads()).toBe(0);
  expect(host.textContent).toContain('Connections writing: 0');
});

it('shows the blocks of a finished file while its release is repaired', async () => {
  serverKnows({ unit: 'blocks', total: 40, runs: [{ state: 'done', len: 30 }, { state: 'lost', len: 2 }, { state: 'done', len: 8 }] });
  await show(task({ status: 'done', resolver: 'nntp', repair: { stage: 'repairing', damaged: 2, recovery: 4 } }));
  expect(host.querySelectorAll('[data-piece="lost"]')).toHaveLength(1);
  expect(host.querySelector('[data-mending]')).not.toBeNull();
  expect(host.textContent).toContain('Damaged or missing: 2');
});

it('forgets the band of one task when another is shown', async () => {
  serverKnows(twoConnections);
  await show(task({}));
  serverKnows(null);
  await show(task({ id: 't2' }));
  expect(card()).toBeNull();
});
