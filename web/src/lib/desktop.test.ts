// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { connectWS } from './api';

type Listener = (...data: unknown[]) => void;

// fakeWindow stands in for what Wails injects: the bound HubBridge and the
// runtime's event registry.
function fakeWindow() {
  const listeners = new Map<string, Listener>();
  const calls: string[] = [];
  const bridge = {
    Open: vi.fn(async (_page: string, id: string) => void calls.push(`open ${id}`)),
    Send: vi.fn(async (id: string, frame: string) => void calls.push(`send ${id} ${frame}`)),
    Close: vi.fn(async (id: string) => void calls.push(`close ${id}`)),
  };
  const runtime = {
    EventsOn: (name: string, cb: Listener) => {
      listeners.set(name, cb);
      return () => listeners.delete(name);
    },
  };
  Object.assign(window, { go: { main: { HubBridge: bridge } }, runtime });
  const emit = (name: string, ...data: unknown[]) => listeners.get(name)?.(...data);
  const streamId = () => bridge.Open.mock.calls[bridge.Open.mock.calls.length - 1][1];
  return { bridge, calls, listeners, emit, streamId };
}

const flush = () => new Promise((r) => setTimeout(r, 0));

beforeEach(() => {
  vi.stubGlobal('WebSocket', vi.fn());
});

afterEach(() => {
  delete (window as { go?: unknown }).go;
  delete (window as { runtime?: unknown }).runtime;
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe('the live stream in the desktop window', () => {
  it('opens over the bridge, never as a WebSocket', async () => {
    const w = fakeWindow();
    const close = connectWS(() => {}, ['task']);
    await flush();
    expect(w.bridge.Open).toHaveBeenCalledTimes(1);
    expect(WebSocket).not.toHaveBeenCalled();
    close();
  });

  it('sends the subscription and visibility frames after the stream opens, in order', async () => {
    const w = fakeWindow();
    const close = connectWS(() => {}, ['captcha'], true);
    await flush();
    const id = w.streamId();
    expect(w.calls).toEqual([
      `open ${id}`,
      `send ${id} {"type":"subscribe","kinds":["captcha"]}`,
      `send ${id} {"type":"visibility","visible":true}`,
    ]);
    close();
  });

  it('hands each event on as a parsed message', async () => {
    const w = fakeWindow();
    const got: [string, unknown][] = [];
    const close = connectWS((type, data) => got.push([type, data]));
    await flush();
    w.emit(`hub:${w.streamId()}`, '{"type":"snapshot","data":[]}');
    expect(got).toEqual([['snapshot', []]]);
    close();
  });

  it('closes the stream on the Go side and stops listening', async () => {
    const w = fakeWindow();
    const close = connectWS(() => {});
    await flush();
    const id = w.streamId();
    close();
    await flush();
    expect(w.calls[w.calls.length - 1]).toBe(`close ${id}`);
    expect(w.listeners.size).toBe(0);
  });

  it('closes a stream that is still opening once it has opened', async () => {
    const w = fakeWindow();
    const close = connectWS(() => {});
    close();
    await flush();
    const id = w.streamId();
    expect(w.calls).toEqual([`open ${id}`, `close ${id}`]);
  });

  it('reconnects when the hub drops the stream', async () => {
    vi.useFakeTimers();
    const w = fakeWindow();
    const close = connectWS(() => {});
    await vi.advanceTimersByTimeAsync(0);
    const first = w.streamId();
    w.emit(`hub:${first}:closed`);
    await vi.advanceTimersByTimeAsync(1500);
    expect(w.bridge.Open).toHaveBeenCalledTimes(2);
    expect(w.streamId()).not.toBe(first);
    close();
  });
});

describe('the live stream in a browser', () => {
  it('opens a WebSocket to /api/ws', () => {
    const sockets: string[] = [];
    vi.stubGlobal(
      'WebSocket',
      vi.fn(function (this: { close(): void }, url: string) {
        sockets.push(url);
        this.close = () => {};
      }),
    );
    const close = connectWS(() => {});
    expect(sockets).toEqual([`ws://${location.host}/api/ws`]);
    close();
  });
});
