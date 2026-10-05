import { afterEach, describe, expect, it, vi } from 'vitest';
import { ownWatcherIds, watcherId } from './clipboardWatchers';
import { isDesktop } from './desktop';

vi.mock('./desktop', async (original) => ({
  ...(await original<typeof import('./desktop')>()),
  isDesktop: vi.fn(() => false),
}));

describe('ownWatcherIds', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.mocked(isDesktop).mockReturnValue(false);
  });

  it('is the browser alone in a browser', async () => {
    const fetch = vi.fn();
    vi.stubGlobal('fetch', fetch);
    expect(await ownWatcherIds()).toEqual([watcherId()]);
    expect(fetch).not.toHaveBeenCalled();
  });

  it('takes in the lease the desktop app holds for this instance', async () => {
    vi.mocked(isDesktop).mockReturnValue(true);
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response(JSON.stringify({ instanceId: 'abc123' }), { status: 200 })),
    );
    expect(await ownWatcherIds()).toEqual([watcherId(), 'desktop-abc123']);
  });
});
