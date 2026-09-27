// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';

import { connectWS } from './api';
import { isDesktop, onUpdateReady } from './desktop';

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('a page in a browser', () => {
  it('is not taken for the desktop window', () => {
    expect(isDesktop()).toBe(false);
  });

  it('opens its live stream as a WebSocket to /api/ws', () => {
    const sockets: string[] = [];
    vi.stubGlobal(
      'WebSocket',
      vi.fn(function (this: { close(): void }, url: string) {
        sockets.push(url);
        this.close = () => {};
      }),
    );
    const fetch = vi.fn();
    vi.stubGlobal('fetch', fetch);
    const close = connectWS(() => {});
    expect(sockets).toEqual([`ws://${location.host}/api/ws`]);
    expect(fetch).not.toHaveBeenCalled();
    close();
  });

  it('sets up nothing for the news of a desktop update', () => {
    onUpdateReady(() => {});
    expect((window as { _wails?: unknown })._wails).toBeUndefined();
  });
});
