// @vitest-environment jsdom
import { act, useRef, useState, type ReactNode } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Task } from '../lib/api';
import type { QuickFilterId } from './ListToolbar';
import type { SearchQuery } from './SearchField';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};

let root: Root;
let host: HTMLDivElement;

// Downloads folds a package until it is opened; the rows have to show here.
const OPEN = JSON.stringify({ 'list.expanded.downloads': ['Films'] });

beforeEach(() => {
  // A fresh module graph, so one test's stored sort is not the next one's.
  vi.resetModules();
  // The interface state is an object, every list the card asks for an array.
  vi.stubGlobal(
    'fetch',
    vi.fn(
      async (url: string) =>
        new Response(url.includes('/api/uistate') ? OPEN : '[]', {
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

const task = (id: string, size: number, status: Task['status']): Task => ({
  id,
  url: `https://files.example/${id}`,
  name: `${id}.zip`,
  package: 'Films',
  resolver: 'direct',
  size,
  loaded: 0,
  speed: 0,
  status,
  createdAt: '2026-09-24T12:00:00Z',
  priority: 0,
  position: 0,
  enabled: true,
});

const TASKS = [task('small', 10, 'queued'), task('large', 30, 'paused'), task('middle', 20, 'queued')];

/**
 * The bar over a list of three links, with the page's share of the state.
 * `facets` is what a page narrows by beyond the quick filters, which then
 * stand aside so the window holds the page's part alone.
 */
async function show(facets?: { panel: ReactNode; active: boolean }): Promise<void> {
  const { ListBar } = await import('./ListBar');
  const { TaskListCard, useListSort } = await import('./TaskList');
  const { DOWNLOAD_FILTERS, matchesQuickFilters, offeredQuickFilters } = await import('./ListToolbar');
  const { EMPTY_SEARCH, matchesSearch } = await import('./SearchField');

  function Page() {
    const row = useRef<HTMLDivElement>(null);
    const [filters, setFilters] = useState<Set<QuickFilterId>>(new Set());
    const [search, setSearch] = useState<SearchQuery>(EMPTY_SEARCH);
    const sort = useListSort('Downloads');
    const shown = TASKS.filter((x) => matchesQuickFilters(x, filters) && matchesSearch(x, search));
    return (
      <>
        <ListBar
          rowRef={row}
          glyphs={false}
          scrolls={false}
          filters={facets ? [] : offeredQuickFilters(DOWNLOAD_FILTERS, TASKS, filters)}
          facets={facets}
          active={filters}
          onToggleFilter={(id) => {
            const next = new Set(filters);
            if (!next.delete(id)) next.add(id);
            setFilters(next);
          }}
          onClearFilters={() => setFilters(new Set())}
          sorts={[sort]}
          search={search}
          onSearch={setSearch}
        >
          <span data-spacer />
        </ListBar>
        <TaskListCard groups={[['Films', shown]]} base="/api" title="Downloads" />
      </>
    );
  }
  await act(async () => root.render(<Page />));
}

const button = (name: string) =>
  [...document.querySelectorAll<HTMLButtonElement>('button')].find((b) => b.textContent?.trim().startsWith(name))!;
const press = (el: HTMLElement) => act(async () => el.click());
const rows = () => [...host.querySelectorAll('[data-row-kind="task"]')].map((r) => r.getAttribute('data-task-id'));
const marked = (name: string) => button(name).parentElement!.querySelector('[data-marked]') !== null;

describe('the Filter button', () => {
  it('opens the quick filters, each with the number of rows it matches', async () => {
    await show();
    expect(document.querySelector('[role="dialog"]')).toBeNull();
    await press(button('Filter'));
    const chips = [...document.querySelectorAll('[role="dialog"] [role="group"] button')].map((b) => b.textContent);
    expect(chips).toEqual(['Queued2', 'Paused1']);
  });

  it('narrows the list to a picked filter and wears a dot while one is on', async () => {
    await show();
    await press(button('Filter'));
    expect(marked('Filter')).toBe(false);
    await press(button('Paused'));
    expect(rows()).toEqual(['large']);
    expect(marked('Filter')).toBe(true);
    await press(button('Show everything'));
    expect(rows()).toEqual(['small', 'large', 'middle']);
    expect(marked('Filter')).toBe(false);
  });

  it('closes on a press outside it', async () => {
    await show();
    await press(button('Filter'));
    await act(async () => {
      document.body.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    });
    expect(document.querySelector('[role="dialog"]')).toBeNull();
  });
});

describe('what a page adds to the Filter window', () => {
  it('stands in the window, which opens without a quick filter on offer', async () => {
    await show({ panel: <p>by host</p>, active: false });
    expect(button('Filter').disabled).toBe(false);
    expect(marked('Filter')).toBe(false);
    await press(button('Filter'));
    expect(document.querySelector('[role="dialog"]')?.textContent).toBe('by host');
  });

  it('puts the dot on the button while any of it is set', async () => {
    await show({ panel: <p>by host</p>, active: true });
    expect(marked('Filter')).toBe(true);
  });
});

describe('the Sort button', () => {
  const sortedBy = () => host.querySelector('[aria-sort]')?.textContent ?? null;
  const direction = () => host.querySelector('[aria-sort]')?.getAttribute('aria-sort') ?? null;
  const entry = (name: string) =>
    [...document.querySelectorAll<HTMLButtonElement>('[role="menu"] button')].find((b) => b.textContent?.trim() === name)!;

  it('sorts the list by a column, as a click on its header does', async () => {
    await show();
    await press(button('Sort'));
    await press(entry('Size'));
    expect(sortedBy()).toBe('Size');
    expect(direction()).toBe('ascending');
    expect(rows()).toEqual(['small', 'middle', 'large']);
    expect(marked('Sort')).toBe(true);
  });

  it('offers both directions and the way back to the queue order', async () => {
    await show();
    await press(button('Sort'));
    expect(entry('Descending').disabled).toBe(true);
    await press(entry('Size'));
    await press(button('Sort'));
    await press(entry('Descending'));
    expect(direction()).toBe('descending');
    expect(rows()).toEqual(['large', 'middle', 'small']);
    await press(button('Sort'));
    await press(entry('Queue order'));
    expect(sortedBy()).toBeNull();
    expect(rows()).toEqual(['small', 'large', 'middle']);
    expect(marked('Sort')).toBe(false);
  });
});

describe('the Search button', () => {
  const field = () => host.querySelector<HTMLInputElement>('input[type="search"]');
  const type = (text: string) =>
    act(async () => {
      const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!;
      set.call(field(), text);
      field()!.dispatchEvent(new Event('input', { bubbles: true }));
    });

  it('ends the row and turns into the field, with the caret in it', async () => {
    await show();
    expect(field()).toBeNull();
    await press(button('Search'));
    expect(document.activeElement).toBe(field());
    expect(button('Search')).toBeUndefined();
  });

  it('narrows the list and keeps its text after a press outside', async () => {
    await show();
    await press(button('Search'));
    await type('large');
    expect(rows()).toEqual(['large']);
    await act(async () => {
      document.body.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    });
    expect(field()?.value).toBe('large');
  });
});
