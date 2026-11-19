// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import type { Task } from './api';

let send: (type: string, data: unknown) => void = () => {};
let connections = 0;
vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  connectWS: (onMessage: (type: string, data: unknown) => void) => {
    send = onMessage;
    connections++;
    return () => {};
  },
}));

const { useTasks } = await import('./useTasks');

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;
let renders: Record<string, Task>[];

function Probe() {
  renders.push(useTasks(''));
  return null;
}

const task = (id: string, status = 'collected') => ({ id, status }) as Task;

// What GET /api/tasks answers. A test that leaves it alone answers an empty list.
let answer: () => Promise<Task[]>;
let gets = 0;

beforeEach(() => {
  vi.useFakeTimers();
  answer = async () => [];
  gets = 0;
  vi.stubGlobal('fetch', async () => {
    gets++;
    return new Response(JSON.stringify(await answer()), { headers: { 'Content-Type': 'application/json' } });
  });
  renders = [];
  connections = 0;
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
  act(() => root.render(<Probe />));
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const latest = () => renders[renders.length - 1];

it('applies a burst of task changes in one render', () => {
  act(() => send('snapshot', Array.from({ length: 300 }, (_, i) => task(`t${i}`))));
  const before = renders.length;
  // Each message arrives as an event of its own, as the socket delivers them.
  for (let i = 0; i < 300; i++) act(() => send('task', task(`t${i}`, 'queued')));
  expect(renders.length).toBe(before);
  act(() => vi.runAllTimers());
  expect(renders.length).toBe(before + 1);
  expect(Object.values(latest()).every((t) => t.status === 'queued')).toBe(true);
});

it('keeps the order of a change and a removal within one burst', () => {
  act(() => send('snapshot', [task('a'), task('b')]));
  act(() => {
    send('task', task('a', 'queued'));
    send('removed', { id: 'b' });
    send('task', task('c'));
  });
  act(() => vi.runAllTimers());
  expect(Object.keys(latest()).sort()).toEqual(['a', 'c']);
  expect(latest().a.status).toBe('queued');
});

it('lets a snapshot replace changes still waiting', () => {
  act(() => send('task', task('stale')));
  act(() => send('snapshot', [task('fresh')]));
  act(() => vi.runAllTimers());
  expect(Object.keys(latest())).toEqual(['fresh']);
});

it('shows a list that opens later what is already known, over the same connection', () => {
  act(() => send('snapshot', [task('a'), task('b')]));
  const later: Record<string, Task>[] = [];
  function Later() {
    later.push(useTasks(''));
    return null;
  }
  const other = document.createElement('div');
  const otherRoot = createRoot(other);
  act(() => otherRoot.render(<Later />));
  expect(Object.keys(later[0]).sort()).toEqual(['a', 'b']);
  expect(connections).toBe(1);
  act(() => otherRoot.unmount());
});

it('fills the list over HTTP while the socket has sent no snapshot', async () => {
  answer = async () => [task('a'), task('b')];
  await act(() => vi.advanceTimersByTimeAsync(2000));
  expect(Object.keys(latest()).sort()).toEqual(['a', 'b']);
});

it('asks over HTTP every two seconds until the snapshot arrives, then leaves it to the socket', async () => {
  await act(() => vi.advanceTimersByTimeAsync(4000));
  expect(gets).toBe(3);
  act(() => send('snapshot', [task('a')]));
  await act(() => vi.advanceTimersByTimeAsync(10000));
  expect(gets).toBe(3);
});

it('keeps the snapshot when an older answer over HTTP comes in after it', async () => {
  let reply: (l: Task[]) => void = () => {};
  answer = () => new Promise((resolve) => (reply = resolve));
  await act(() => vi.advanceTimersByTimeAsync(2000));
  act(() => send('snapshot', [task('fresh')]));
  await act(async () => reply([task('old')]));
  await act(() => vi.advanceTimersByTimeAsync(100));
  expect(Object.keys(latest())).toEqual(['fresh']);
});
