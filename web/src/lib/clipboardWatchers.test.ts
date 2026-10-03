// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { deviceLabel, startLease, watcherId } from './clipboardWatchers';

describe('deviceLabel', () => {
  it('names the browser and the system', () => {
    expect(deviceLabel('Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:140.0) Gecko/20100101 Firefox/140.0')).toBe(
      'Firefox, Windows',
    );
    expect(
      deviceLabel(
        'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Safari/537.36 Edg/140.0',
      ),
    ).toBe('Edge, Windows');
    expect(
      deviceLabel('Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Safari/537.36'),
    ).toBe('Chrome, Linux');
    expect(
      deviceLabel(
        'Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15',
      ),
    ).toBe('Safari, macOS');
  });

  it('falls back to the browser alone', () => {
    expect(deviceLabel('curl/8.0')).toBe('Browser');
  });
});

describe('watcherId', () => {
  it('stays the same for the browser', () => {
    expect(watcherId()).toBe(watcherId());
    expect(watcherId()).toMatch(/^web-[0-9a-f]{24}$/);
  });
});

describe('startLease', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  function stubFetch(stop: boolean) {
    const calls: { url: string; method: string }[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string, init?: RequestInit) => {
        calls.push({ url, method: init?.method ?? 'GET' });
        return new Response(init?.method === 'PUT' ? JSON.stringify({ stop }) : null, {
          status: init?.method === 'PUT' ? 200 : 204,
        });
      }),
    );
    return calls;
  }

  it('renews every minute and leaves when it ends', async () => {
    vi.useFakeTimers();
    const calls = stubFetch(false);
    const stopped = vi.fn();
    const end = startLease('', stopped);
    await vi.advanceTimersByTimeAsync(60_000);
    expect(calls.filter((c) => c.method === 'PUT')).toHaveLength(2);
    end();
    expect(calls[calls.length - 1]).toEqual({ url: `/api/clipboard-watchers/${watcherId()}`, method: 'DELETE' });
    expect(stopped).not.toHaveBeenCalled();
  });

  it('renews and leaves at the instance the watch sends to', async () => {
    const calls = stubFetch(false);
    const end = startLease('nas', () => {});
    await vi.waitFor(() => expect(calls).toHaveLength(1));
    end();
    const path = `/api/instances/nas/clipboard-watchers/${watcherId()}`;
    expect(calls).toEqual([
      { url: path, method: 'PUT' },
      { url: path, method: 'DELETE' },
    ]);
  });

  it('leaves when the tab closes', async () => {
    const calls = stubFetch(false);
    const end = startLease('', () => {});
    await vi.waitFor(() => expect(calls).toHaveLength(1));
    dispatchEvent(new Event('pagehide'));
    expect(calls[1]).toEqual({ url: `/api/clipboard-watchers/${watcherId()}`, method: 'DELETE' });
    end();
  });

  it('renews soon after another tab of this browser closed', async () => {
    const calls = stubFetch(false);
    const end = startLease('', () => {});
    await vi.waitFor(() => expect(calls).toHaveLength(1));
    const closing = new BroadcastChannel('knightloader.clipboardWatcher');
    closing.postMessage('left');
    await vi.waitFor(() => expect(calls.filter((c) => c.method === 'PUT')).toHaveLength(2), { timeout: 3_000 });
    closing.close();
    end();
  });

  it('reports a stop asked from another device', async () => {
    stubFetch(true);
    const stopped = vi.fn();
    const end = startLease('', stopped);
    await vi.waitFor(() => expect(stopped).toHaveBeenCalledOnce());
    end();
  });
});
