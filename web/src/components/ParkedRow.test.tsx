// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Task } from '../lib/api';
import { COLUMN_BY_ID, PARKED, type CellContext } from './columns';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

beforeEach(() => {
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => new Response('[]', { headers: { 'Content-Type': 'application/json' } })),
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

const task = (over: Partial<Task> = {}): Task => ({
  id: 'a',
  url: 'https://files.example/a',
  name: 'Big.Buck.Bunny.mkv',
  package: 'Open Movies',
  resolver: 'direct',
  size: 1000,
  loaded: 400,
  speed: 0,
  status: 'queued',
  createdAt: '2026-09-24T12:00:00Z',
  priority: 0,
  position: 0,
  enabled: true,
  ...over,
});

const ctx: CellContext = { t: (key) => key, base: '/api', profile: 'downloads' };

function nameCell(t: Task) {
  act(() => root.render(<>{COLUMN_BY_ID.get('name')?.render(t, ctx)}</>));
  return {
    parked: [...host.querySelectorAll(`.${PARKED}`)],
    mark: host.querySelector('[aria-label="task.waiting.disabled"]'),
  };
}

describe('a parked row', () => {
  it('greys the name of a disabled link and leaves its power mark at full strength', () => {
    const { parked, mark } = nameCell(task({ enabled: false }));
    expect(parked.map((el) => el.textContent).join()).toContain('Big.Buck.Bunny.mkv');
    expect(mark).not.toBeNull();
    expect(parked.some((el) => el.contains(mark))).toBe(false);
  });

  it('greys the failure line of a disabled link as well', () => {
    const { parked } = nameCell(task({ enabled: false, status: 'error', reason: 'gone', error: 'not found' }));
    expect(parked).toHaveLength(2);
  });

  it('leaves an enabled link in its own ink', () => {
    expect(nameCell(task()).parked).toHaveLength(0);
  });
});
