import { afterEach, describe, expect, it, vi } from 'vitest';

import type { Category, Task } from '../lib/api';
import { categoryMenuItem } from './ListToolbar';

const task = (id: string, category?: string): Task => ({
  id,
  url: `https://files.example/${id}`,
  name: `${id}.mkv`,
  package: 'Open Movies',
  resolver: 'direct',
  size: 1000,
  loaded: 0,
  speed: 0,
  status: 'collected',
  createdAt: '2026-09-24T12:00:00Z',
  priority: 0,
  position: 0,
  enabled: true,
  category,
});

const drawers: Category[] = [
  { id: 'serien', name: 'TV Series' },
  { id: 'filme', name: 'Movies' },
];

function entry(chosen: Task[], categories = drawers) {
  return categoryMenuItem({
    chosen,
    base: '/api',
    t: (key) => key,
    fail: () => {},
    categories,
  });
}

const values = (chosen: Task[]) => entry(chosen)?.submenu?.[0].items ?? [];

afterEach(() => vi.unstubAllGlobals());

describe('the category entry', () => {
  it('is left out while there are no categories', () => {
    expect(entry([task('a')], [])).toBeNull();
  });

  it('offers no category and every category by its name', () => {
    expect(values([task('a')]).map((x) => x.label)).toEqual(['menu.categoryNone', 'TV Series', 'Movies']);
  });

  it('ticks the category the whole selection shares', () => {
    const ticked = (chosen: Task[]) => values(chosen).filter((x) => x.checked).map((x) => x.id);
    expect(ticked([task('a', 'serien'), task('b', 'serien')])).toEqual(['serien']);
    expect(ticked([task('a'), task('b')])).toEqual(['']);
    expect(ticked([task('a', 'serien'), task('b', 'filme')])).toEqual([]);
  });

  it('files the selection under the picked category', async () => {
    const fetch = vi.fn(async () => new Response(JSON.stringify({ ids: ['a', 'b'], count: 2 })));
    vi.stubGlobal('fetch', fetch);
    values([task('a'), task('b')])
      .find((x) => x.id === 'filme')
      ?.onSelect?.();
    await vi.waitFor(() => expect(fetch).toHaveBeenCalled());
    const [url, init] = fetch.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('/api/tasks/category');
    expect(JSON.parse(String(init.body))).toEqual({ ids: ['a', 'b'], category: 'filme' });
  });
});
