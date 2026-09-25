// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import type { Task } from '../lib/api';
import { RowMarks, type CellContext } from './columns';

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

const task = (id: string, over: Partial<Task> = {}): Task => ({
  id,
  url: `https://files.example/${id}`,
  name: `${id}.mkv`,
  package: 'Open Movies',
  resolver: 'direct',
  size: 1000,
  loaded: 0,
  speed: 0,
  status: 'queued',
  createdAt: '2026-09-24T12:00:00Z',
  priority: 0,
  position: 0,
  enabled: true,
  ...over,
});

const ctx = (over: Partial<CellContext> = {}): CellContext => ({
  t: (key) => key,
  base: '/api',
  profile: 'downloads',
  ...over,
});

/** The accessible names of the marks drawn for these rows. */
function marks(items: Task[], context = ctx()): string[] {
  act(() => root.render(<RowMarks items={items} ctx={context} />));
  return [...host.querySelectorAll('[role="img"]')].map((el) => el.getAttribute('aria-label') ?? '');
}

describe('RowMarks', () => {
  it('names every state the right-click menu leaves on a row', () => {
    expect(marks([task('a', { forced: true, hold: true, enabled: false })], ctx({ stopMark: 'a' }))).toEqual([
      'queue.stopMarkOn',
      'task.forced',
      'task.held',
      'task.waiting.disabled',
    ]);
  });

  it('draws nothing on a row nobody marked', () => {
    expect(marks([task('a')])).toEqual([]);
  });

  it('marks a package only when every link in it carries the state', () => {
    expect(marks([task('a', { hold: true }), task('b')])).toEqual([]);
    expect(marks([task('a', { hold: true }), task('b', { hold: true })])).toEqual(['task.held']);
  });

  it('shows the stop mark on the package that holds the marked link', () => {
    expect(marks([task('a'), task('b')], ctx({ stopMark: 'b' }))).toEqual(['queue.stopMarkOn']);
  });

  it('drops the stop mark once its download has finished', () => {
    expect(marks([task('a', { status: 'done' })], ctx({ stopMark: 'a' }))).toEqual([]);
  });

  it('leaves a switched-off row to the Enabled column where that column is drawn', () => {
    const off = [task('a', { enabled: false })];
    expect(marks(off, ctx({ profile: 'collector', switchShown: true }))).toEqual([]);
    expect(marks(off, ctx({ profile: 'collector', switchShown: false }))).toEqual(['task.waiting.disabled']);
  });
});
