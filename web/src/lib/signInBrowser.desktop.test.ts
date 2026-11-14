// @vitest-environment jsdom
// @vitest-environment-options {"url":"http://wails.localhost/"}
import { afterEach, expect, it, vi } from 'vitest';
import { openPageStream } from './signInBrowser';

type Host = {
  _wails?: { dispatchWailsEvent?: (ev: { name: string; data: unknown }) => void };
  chrome?: { webview?: { postMessage(message: string): void } };
};

(window as unknown as Host).chrome = { webview: { postMessage: () => {} } };

type RuntimeBody = { object: number; args?: { methodName?: string; args?: unknown[] } };

// The bound calls the page makes, as "<method> <args...>".
function fakeRuntime() {
  const calls: string[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (_url: string, init: RequestInit) => {
      const body = JSON.parse(String(init.body)) as RuntimeBody;
      if (body.object === 0) calls.push([body.args?.methodName, ...(body.args?.args ?? [])].join(' '));
      return new Response('null', { headers: { 'Content-Type': 'application/json' } });
    }),
  );
  return calls;
}

const emit = (name: string, data?: unknown) => (window as unknown as Host)._wails?.dispatchWailsEvent?.({ name, data });
const flush = () => new Promise((r) => setTimeout(r, 0));

afterEach(() => vi.unstubAllGlobals());

it('carries the sign-in browser over Wails events in the desktop app', async () => {
  const calls = fakeRuntime();
  const frames: Blob[] = [];
  const messages: string[] = [];
  let closed = 0;
  const stream = openPageStream({ frame: (b) => frames.push(b), message: (m) => messages.push(m), closed: () => closed++ });
  await flush();

  const watch = calls.find((c) => c.startsWith('main.SignInBridge.Watch'));
  expect(watch).toMatch(/^main\.SignInBridge\.Watch main \S+$/);
  const id = watch!.split(' ')[2];

  emit(`signin:${id}:frame`, btoa(String.fromCharCode(0xff, 0xd8, 0xff)));
  emit(`signin:${id}:msg`, '{"type":"page","host":"www.patreon.com"}');
  expect(frames).toHaveLength(1);
  expect(frames[0].type).toBe('image/jpeg');
  expect([...new Uint8Array(await frames[0].arrayBuffer())]).toEqual([0xff, 0xd8, 0xff]);
  expect(messages).toEqual(['{"type":"page","host":"www.patreon.com"}']);

  stream.send({ type: 'keydown', key: 'ä' });
  stream.close();
  await flush();
  await flush();
  expect(calls).toContain('main.SignInBridge.Input {"type":"keydown","key":"ä"}');
  expect(calls).toContain(`main.SignInBridge.Stop ${id}`);

  // Closed by the page, so the bridge's end is not reported back to it.
  emit(`signin:${id}:closed`);
  expect(closed).toBe(0);
});

it('reports a stream the bridge ended', async () => {
  const calls = fakeRuntime();
  let closed = 0;
  openPageStream({ frame: () => {}, message: () => {}, closed: () => closed++ });
  await flush();
  const id = calls.find((c) => c.startsWith('main.SignInBridge.Watch'))!.split(' ')[2];
  emit(`signin:${id}:closed`);
  expect(closed).toBe(1);
});
