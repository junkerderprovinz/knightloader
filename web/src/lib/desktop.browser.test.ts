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

  it('lets a socket that is still connecting open before closing it', () => {
    class FakeSocket {
      static CONNECTING = 0;
      static OPEN = 1;
      readyState = FakeSocket.CONNECTING;
      onopen: (() => void) | null = null;
      onmessage: ((e: { data: string }) => void) | null = null;
      onclose: (() => void) | null = null;
      sent: string[] = [];
      close = vi.fn();
      send(frame: string) {
        this.sent.push(frame);
      }
    }
    const sockets: FakeSocket[] = [];
    vi.stubGlobal(
      'WebSocket',
      Object.assign(
        function () {
          const ws = new FakeSocket();
          sockets.push(ws);
          return ws;
        },
        { CONNECTING: 0, OPEN: 1 },
      ),
    );
    const onMessage = vi.fn();
    const close = connectWS(onMessage, ['task']);
    close();
    const [ws] = sockets;
    expect(ws.close).not.toHaveBeenCalled();

    ws.readyState = FakeSocket.OPEN;
    ws.onopen?.();
    ws.onmessage?.({ data: '{"type":"task","data":{}}' });
    expect(ws.close).toHaveBeenCalledOnce();
    expect(ws.sent).toEqual([]);
    expect(onMessage).not.toHaveBeenCalled();
  });

  it('sets up nothing for the news of a desktop update', () => {
    onUpdateReady(() => {});
    expect((window as { _wails?: unknown })._wails).toBeUndefined();
  });
});
