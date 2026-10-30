// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import type { Task } from './api';

let send: (type: string, data: unknown) => void = () => {};
vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  connectWS: (onMessage: (type: string, data: unknown) => void) => {
    send = onMessage;
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

beforeEach(() => {
  vi.useFakeTimers();
  renders = [];
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
  act(() => root.render(<Probe />));
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.useRealTimers();
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
