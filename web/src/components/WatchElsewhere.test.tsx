// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import { useWatchSwitch } from './WatchElsewhere';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;
let flip: (on: boolean) => void;

function Probe() {
  const s = useWatchSwitch();
  flip = s.flip;
  return <>{s.dialog}</>;
}

beforeEach(() => {
  localStorage.clear();
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
  act(() => root.render(<Probe />));
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

it('asks every listed device to switch off at once', async () => {
  const asked: string[] = [];
  const answers: (() => void)[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string, init?: RequestInit) => {
      if (init?.method === 'POST') {
        asked.push(url);
        return new Promise<Response>((resolve) => answers.push(() => resolve(new Response(null, { status: 204 }))));
      }
      return Promise.resolve(
        new Response(
          JSON.stringify([
            { id: 'desk', name: 'Workshop laptop', kind: 'desktop' },
            { id: 'ext', name: 'Firefox, Windows', kind: 'extension' },
          ]),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        ),
      );
    }),
  );

  await act(async () => flip(true));
  const stop = [...document.querySelectorAll('button')].find((b) => b.textContent === 'Switch off there');
  expect(stop).toBeDefined();
  await act(async () => stop!.click());

  expect(asked).toEqual(['/api/clipboard-watchers/desk/stop', '/api/clipboard-watchers/ext/stop']);
  await act(async () => answers.forEach((answer) => answer()));
});
