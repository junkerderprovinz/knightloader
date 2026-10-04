// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

// WATCH_SUPPORTED is read when the module loads, so the clipboard has to be
// there before the import.
vi.hoisted(() => {
  Object.defineProperty(navigator, 'clipboard', { value: { readText: async () => '' }, configurable: true });
});

import { useClipboardWatch } from './useClipboardWatch';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;
let fetches: string[];
let watch: [boolean, (on: boolean) => void];

function Probe() {
  watch = useClipboardWatch();
  return null;
}

beforeEach(() => {
  localStorage.clear();
  fetches = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      fetches.push(`${init?.method ?? 'GET'} ${url}`);
      return new Response('{}', { status: 200 });
    }),
  );
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
  act(() => root.render(<Probe />));
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

it('keeps the switch in this browser, not in the state every browser of the instance shares', async () => {
  vi.useFakeTimers();
  act(() => watch[1](true));
  expect(watch[0]).toBe(true);
  await vi.advanceTimersByTimeAsync(5_000);
  expect(fetches.filter((f) => f.includes('/api/uistate'))).toEqual([]);
});

it('follows a switch flipped in another tab of this browser', () => {
  act(() => watch[1](true));
  act(() => {
    // The other tab heard the stop and wrote the switch off.
    localStorage.setItem('knightloader.clipboardWatch', 'off');
    window.dispatchEvent(new StorageEvent('storage', { key: 'knightloader.clipboardWatch' }));
  });
  expect(watch[0]).toBe(false);
});
