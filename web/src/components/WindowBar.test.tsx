// @vitest-environment jsdom
// @vitest-environment-options {"url":"http://wails.localhost/"}
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { I18nProvider } from '../lib/i18n';
import { WindowBar } from './WindowBar';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const said: string[] = [];
(window as unknown as { chrome: unknown }).chrome = { webview: { postMessage: (m: string) => said.push(m) } };

// The window methods of the Wails runtime, as messageprocessor_window.go numbers them.
const WINDOW = 6;
const METHOD = { 2: 'close', 14: 'isMaximised', 17: 'minimise', 39: 'toggleMaximise' } as Record<number, string>;

let root: Root;
let host: HTMLDivElement;
let asked: string[];
let maximised: boolean;

beforeEach(() => {
  said.length = 0;
  asked = [];
  maximised = false;
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url === '/wails/runtime') {
        const { object, method } = JSON.parse(String(init?.body)) as { object: number; method: number };
        if (object === WINDOW) asked.push(METHOD[method] ?? String(method));
        const answer = object === WINDOW && method === 14 ? maximised : {};
        return new Response(JSON.stringify(answer), { headers: { 'Content-Type': 'application/json' } });
      }
      return new Response('{}', { headers: { 'Content-Type': 'application/json' } });
    }),
  );
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

async function show() {
  await act(async () =>
    root.render(
      <I18nProvider>
        <WindowBar />
      </I18nProvider>,
    ),
  );
}

const button = (name: string) => host.querySelector<HTMLButtonElement>(`button[aria-label="${name}"]`);
const bar = () => host.querySelector<HTMLElement>('.glim-window-bar')!;

describe('the desktop window bar', () => {
  it('shows the name and the three window buttons', async () => {
    await show();
    expect(bar().textContent).toContain('KnightLoader');
    expect(button('Minimise')).not.toBeNull();
    expect(button('Maximise')).not.toBeNull();
    expect(button('Close')).not.toBeNull();
  });

  it('minimises, maximises and closes through the window runtime', async () => {
    await show();
    asked.length = 0;
    await act(async () => button('Minimise')!.click());
    await act(async () => button('Maximise')!.click());
    await act(async () => button('Close')!.click());
    expect(asked).toEqual(['minimise', 'toggleMaximise', 'close']);
  });

  it('offers to restore a maximised window', async () => {
    maximised = true;
    await show();
    expect(button('Restore')).not.toBeNull();
    expect(button('Maximise')).toBeNull();
  });

  it('follows the window once a resize has settled', async () => {
    vi.useFakeTimers();
    await show();
    expect(button('Maximise')).not.toBeNull();
    maximised = true;
    await act(async () => {
      window.dispatchEvent(new Event('resize'));
      await vi.advanceTimersByTimeAsync(150);
    });
    expect(button('Restore')).not.toBeNull();
  });

  it('moves the window when a press on the bar starts to move', async () => {
    await show();
    act(() => {
      bar().dispatchEvent(new MouseEvent('mousedown', { bubbles: true, button: 0, buttons: 1 }));
    });
    expect(said).not.toContain('wails:drag');
    act(() => {
      bar().dispatchEvent(new MouseEvent('mousemove', { bubbles: true, buttons: 1, clientX: 300, clientY: 20 }));
    });
    expect(said.filter((m) => m === 'wails:drag')).toHaveLength(1);
  });

  it('leaves a press on a window button to the button', async () => {
    await show();
    const close = button('Close')!;
    act(() => {
      close.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, button: 0, buttons: 1 }));
      close.dispatchEvent(new MouseEvent('mousemove', { bubbles: true, buttons: 1, clientX: 300, clientY: 20 }));
    });
    expect(said).not.toContain('wails:drag');
  });

  it('maximises on a double click on the bar, not on a button', async () => {
    await show();
    asked.length = 0;
    await act(async () => {
      bar().dispatchEvent(new MouseEvent('dblclick', { bubbles: true }));
      button('Minimise')!.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }));
    });
    expect(asked).toEqual(['toggleMaximise']);
  });

  it('stays out of the tray window', async () => {
    window.history.pushState(null, '', '/tray');
    try {
      await show();
      expect(host.querySelector('.glim-window-bar')).toBeNull();
    } finally {
      window.history.pushState(null, '', '/');
    }
  });

  it('leaves macOS its native title bar', async () => {
    vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)');
    try {
      await show();
      expect(host.querySelector('.glim-window-bar')).toBeNull();
    } finally {
      vi.restoreAllMocks();
    }
  });
});
