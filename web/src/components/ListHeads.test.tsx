// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Task } from '../lib/api';
import type { ColumnFilters } from './TaskList';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};

let root: Root;
let host: HTMLDivElement;
let uistate: Record<string, unknown>;

beforeEach(() => {
  vi.resetModules();
  uistate = {};
  vi.stubGlobal(
    'fetch',
    vi.fn(
      async (url: string) =>
        new Response(url.includes('/api/uistate') ? JSON.stringify(uistate) : '[]', {
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

const task: Task = {
  id: 'a',
  url: 'https://files.example/a',
  name: 'a.zip',
  package: '',
  resolver: 'direct',
  size: 1000,
  loaded: 0,
  speed: 0,
  status: 'queued',
  createdAt: '2026-09-24T12:00:00Z',
  priority: 0,
  position: 0,
  enabled: true,
};

async function show(filters: ColumnFilters, onSearch?: () => void): Promise<void> {
  const { TaskListCard } = await import('./TaskList');
  // From the same fresh module graph as the card, or the two hold different contexts.
  const { I18nProvider } = await import('../lib/i18n');
  await act(async () =>
    root.render(
      <I18nProvider>
        <MemoryRouter>
          <TaskListCard
            groups={[['', [task]]]}
            base="/api"
            title="List"
            filters={filters}
            onSearch={onSearch}
            selection={{ ids: new Set(), set: () => {}, toggle: () => {} }}
          />
        </MemoryRouter>
      </I18nProvider>,
    ),
  );
}

const funnel = () => host.querySelector<HTMLButtonElement>('button[aria-haspopup="dialog"]');

describe('the column heads', () => {
  it('offer a ▾ only on a column that can be filtered, and open its window under it', async () => {
    await show({ status: { active: false, panel: <p>the status filter</p> } });
    expect(host.querySelectorAll('button[aria-haspopup="dialog"]')).toHaveLength(1);
    expect(funnel()!.getAttribute('aria-label')).toBe('Filter Status');
    await act(async () => funnel()!.click());
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('the status filter');
  });

  it('mark the ▾ of a column that narrows the list now', async () => {
    await show({ status: { active: true, panel: null } });
    expect(funnel()!.querySelector('.bg-accent')).not.toBeNull();
  });

  it('say what they do in a line of their own until somebody closes it, then only under the pointer', async () => {
    await show({});
    const lines = () => [...host.querySelectorAll('[role="note"]')].filter((n) => n.textContent?.includes('just type to search'));
    expect(lines()).toHaveLength(1);
    expect(lines()[0].querySelector('button')).not.toBeNull();
    await act(async () => lines()[0].querySelector('button')!.click());
    expect(lines()).toHaveLength(1);
    expect(lines()[0].querySelector('button')).toBeNull();
    expect(lines()[0].className).toContain('hidden');
  });

  it('open their menu on a long press on a touch screen, with the search in it', async () => {
    vi.useFakeTimers();
    const onSearch = vi.fn();
    await show({ status: { active: false, panel: null } }, onSearch);
    const head = host.querySelector<HTMLButtonElement>('[data-col="status"]')!;
    await act(async () => {
      head.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, pointerType: 'touch', clientX: 10, clientY: 10 }));
      vi.advanceTimersByTime(600);
    });
    vi.useRealTimers();
    const entries = [...document.querySelectorAll('[role="menuitem"]')].map((e) => e.textContent);
    expect(entries).toEqual(expect.arrayContaining(['Ascending', 'Descending', 'Filter', 'Search']));
    await act(async () => [...document.querySelectorAll<HTMLElement>('[role="menuitem"]')].find((e) => e.textContent === 'Search')!.click());
    expect(onSearch).toHaveBeenCalled();
  });
});
