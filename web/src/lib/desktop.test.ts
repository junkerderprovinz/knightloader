// @vitest-environment jsdom
// @vitest-environment-options {"url":"http://wails.localhost/"}
import { act, createElement } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { connectWS } from './api';
import { desktopOS, edgeAt, isDesktop, onUpdateReady, openNatively, openURL, trayWords, useEdgeResize } from './desktop';

type Host = {
  _wails?: { dispatchWailsEvent?: (ev: { name: string; data: unknown }) => void };
  chrome?: { webview?: { postMessage(message: string): void } };
};

// WebView2's message channel, which the window has from the start.
(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const said: string[] = [];
(window as unknown as Host).chrome = { webview: { postMessage: (m) => said.push(m) } };

type RuntimeBody = { object: number; method: number; args?: { methodName?: string; args?: unknown[] } & Record<string, unknown> };

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

// fakeRuntime stands in for the Wails runtime endpoint the page posts to. Each
// bound call is recorded as "<method> <args...>", and answer decides the reply.
function fakeRuntime(answer: (body: RuntimeBody) => Response = () => json(null)) {
  const calls: string[] = [];
  const bodies: RuntimeBody[] = [];
  const fetch = vi.fn(async (url: string, init: RequestInit) => {
    expect(url).toBe('/wails/runtime');
    const body = JSON.parse(String(init.body)) as RuntimeBody;
    bodies.push(body);
    if (body.object === 0) calls.push([body.args?.methodName, ...(body.args?.args ?? [])].join(' '));
    return answer(body);
  });
  vi.stubGlobal('fetch', fetch);
  const opens = () => bodies.filter((b) => b.args?.methodName === 'main.HubBridge.Open');
  const streamId = () => String(opens()[opens().length - 1]?.args?.args?.[2]);
  return { calls, bodies, opens, streamId };
}

function send(name: string, data?: unknown) {
  (window as unknown as Host)._wails?.dispatchWailsEvent?.({ name, data });
}

const flush = () => new Promise((r) => setTimeout(r, 0));

beforeEach(() => {
  vi.stubGlobal('WebSocket', vi.fn());
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe('the desktop window', () => {
  // Wails runs no event into the window until it hears this. First, because
  // the page says it once.
  it('tells Wails the page is ready, once', async () => {
    fakeRuntime();
    const close = connectWS(() => {});
    const again = connectWS(() => {});
    await flush();
    expect(said).toEqual(['wails:runtime:ready']);
    close();
    again();
  });

  it('is told apart by the origin Wails serves it from', () => {
    expect(isDesktop()).toBe(true);
  });
});

describe('the live stream in the desktop window', () => {
  it('opens over the bridge, never as a WebSocket', async () => {
    const w = fakeRuntime();
    const close = connectWS(() => {}, ['task']);
    await flush();
    expect(w.opens()).toHaveLength(1);
    expect(WebSocket).not.toHaveBeenCalled();
    close();
  });

  it('sends the subscription and visibility frames after the stream opens, in order', async () => {
    const w = fakeRuntime();
    const close = connectWS(() => {}, ['captcha'], true);
    await flush();
    await flush();
    const id = w.streamId();
    expect(w.calls.filter((c) => c.includes(id))).toEqual([
      expect.stringMatching(new RegExp(`^main\\.HubBridge\\.Open main \\S+ ${id.replace('.', '\\.')}$`)),
      `main.HubBridge.Send ${id} {"type":"subscribe","kinds":["captcha"]}`,
      `main.HubBridge.Send ${id} {"type":"visibility","visible":true}`,
    ]);
    close();
  });

  it('hands each event on as a parsed message', async () => {
    const w = fakeRuntime();
    const got: [string, unknown][] = [];
    const close = connectWS((type, data) => got.push([type, data]));
    await flush();
    send(`hub:${w.streamId()}`, '{"type":"snapshot","data":[]}');
    expect(got).toEqual([['snapshot', []]]);
    close();
  });

  it('closes the stream on the Go side and stops listening', async () => {
    const w = fakeRuntime();
    const got: unknown[] = [];
    const close = connectWS((type) => got.push(type));
    await flush();
    const id = w.streamId();
    close();
    await flush();
    expect(w.calls[w.calls.length - 1]).toBe(`main.HubBridge.Close ${id}`);
    send(`hub:${id}`, '{"type":"snapshot","data":[]}');
    expect(got).toEqual([]);
  });

  it('closes a stream that is still opening once it has opened', async () => {
    const w = fakeRuntime();
    const close = connectWS(() => {});
    close();
    await flush();
    await flush();
    const id = w.streamId();
    expect(w.calls.filter((c) => c.includes(id)).map((c) => c.split(' ')[0])).toEqual([
      'main.HubBridge.Open',
      'main.HubBridge.Close',
    ]);
  });

  it('names the tray window when the page runs there', async () => {
    const w = fakeRuntime();
    history.pushState(null, '', '/tray');
    const close = connectWS(() => {});
    await flush();
    history.pushState(null, '', '/');
    expect(w.opens()[0]?.args?.args?.[0]).toBe('tray');
    close();
  });

  it('reconnects when the hub drops the stream', async () => {
    vi.useFakeTimers();
    const w = fakeRuntime();
    const close = connectWS(() => {});
    await vi.advanceTimersByTimeAsync(0);
    const first = w.streamId();
    send(`hub:${first}:closed`);
    await vi.advanceTimersByTimeAsync(1500);
    expect(w.opens()).toHaveLength(2);
    expect(w.streamId()).not.toBe(first);
    close();
  });
});

describe('the calls into the desktop app', () => {
  it('names the bound method and gives each call an id of its own', async () => {
    const w = fakeRuntime();
    await openNatively('t1');
    await openNatively('t1');
    expect(w.calls).toEqual(['main.DesktopFiles.OpenNatively t1', 'main.DesktopFiles.OpenNatively t1']);
    const ids = w.bodies.map((b) => b.args?.['call-id']);
    expect(new Set(ids).size).toBe(2);
  });

  it("reject with the Go side's reason", async () => {
    fakeRuntime(() => json({ message: 'the file is gone', kind: 'RuntimeError' }, 500));
    await expect(openNatively('t1')).rejects.toThrow('the file is gone');
  });

  it('hand an address to the system through the runtime', async () => {
    const w = fakeRuntime();
    await openURL('https://github.com/junkerderprovinz/knightloader');
    expect(w.bodies).toEqual([{ object: 9, method: 0, args: { url: 'https://github.com/junkerderprovinz/knightloader' } }]);
  });

  it('read the system the app runs on', async () => {
    fakeRuntime(() => json({ OS: 'darwin', Arch: 'arm64' }));
    expect(await desktopOS()).toBe('darwin');
  });
});

describe('the news of a downloaded update', () => {
  it('reaches the page as the version', () => {
    const got: string[] = [];
    const stop = onUpdateReady((v) => got.push(v));
    send('updateReady', 'v1.4.0');
    stop();
    send('updateReady', 'v1.5.0');
    expect(got).toEqual(['v1.4.0']);
  });
});

describe('the tray words', () => {
  it('carry every label the menu shows, the queue entry in the queue bar’s words', () => {
    const words = trayWords((key) => `<${key}>`);
    expect(Object.keys(words).sort()).toEqual(
      [
        'captcha',
        'closeToTray',
        'hide',
        'minimiseToTray',
        'quit',
        'raiseFocus',
        'raiseFront',
        'raiseOff',
        'show',
        'startHidden',
        'startQueue',
        'stopQueue',
      ].sort(),
    );
    expect(words.stopQueue).toBe('<queue.stop>');
    expect(words.startQueue).toBe('<queue.start>');
  });
});

describe('resizing the frameless tray window', () => {
  it.each([
    [0, 100, 'w-resize'],
    [359, 100, 'e-resize'],
    [150, 0, 'n-resize'],
    [150, 479, 's-resize'],
    [2, 2, 'nw-resize'],
    [355, 478, 'se-resize'],
    [1, 470, 'sw-resize'],
    [358, 10, 'ne-resize'],
    [150, 200, ''],
  ] as const)('reads %i,%i in a 360 by 480 window as %s', (x, y, edge) => {
    expect(edgeAt(x, y, 360, 480)).toBe(edge);
  });

  it('hands a drag that starts at an edge to Wails', async () => {
    const host = document.createElement('div');
    document.body.append(host);
    const root = createRoot(host);
    function Resizable() {
      useEdgeResize();
      return null;
    }
    await act(async () => root.render(createElement(Resizable)));
    said.length = 0;
    const at = (type: string, x: number, y: number) =>
      window.dispatchEvent(new MouseEvent(type, { clientX: x, clientY: y, button: 0, bubbles: true }));
    at('mousemove', window.innerWidth - 1, window.innerHeight - 1);
    expect(document.body.style.cursor).toBe('nwse-resize');
    at('mousedown', window.innerWidth - 1, window.innerHeight - 1);
    at('mousemove', window.innerWidth - 20, window.innerHeight - 20);
    expect(said).toEqual(['wails:resize:se-resize']);
    at('mouseup', 0, 0);
    at('mousemove', 200, 200);
    expect(document.body.style.cursor).toBe('');
    act(() => root.unmount());
    host.remove();
  });
});
