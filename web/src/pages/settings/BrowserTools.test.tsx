// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { buildBookmarklet } from '../../lib/browserTools';
import { BrowserTools } from './BrowserTools';
import { RELAY_RUN_COMMAND } from './pairing/RelayCard';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// jsdom lays nothing out and has no ResizeObserver, which the README buttons
// use to fit their words.
globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};

let root: Root;
let host: HTMLDivElement;
const writeText = vi.fn(() => Promise.resolve());

beforeEach(() => {
  // The versions and the build the cards show come from the server, which is
  // not here.
  vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})));
  // jsdom has no clipboard to copy the bookmarklet into.
  Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  Reflect.deleteProperty(navigator, 'clipboard');
  writeText.mockClear();
});

function bookmarkletLink() {
  return [...host.querySelectorAll('a')].find((a) => a.textContent === 'KnightLoader')!;
}

describe('BrowserTools', () => {
  it('renders the bookmarklet as a javascript: link that can be dragged to the bookmarks bar', async () => {
    await act(async () => root.render(<BrowserTools />));
    const href = bookmarkletLink().getAttribute('href');
    expect(href).toMatch(/^javascript:/);
    expect(href).toBe(buildBookmarklet(window.location.origin));
  });

  it('keeps the javascript: link when the page renders again', async () => {
    await act(async () => root.render(<BrowserTools />));
    await act(async () => root.render(<BrowserTools />));
    expect(bookmarkletLink().getAttribute('href')).toBe(buildBookmarklet(window.location.origin));
  });

  it('draws the bookmarklet logo without an image a drag could pick up instead of the link', async () => {
    await act(async () => root.render(<BrowserTools />));
    expect(bookmarkletLink().querySelector('img')).toBeNull();
    expect(bookmarkletLink().querySelector('svg')).not.toBeNull();
  });

  it('copies the same code with the segment beside the link', async () => {
    await act(async () => root.render(<BrowserTools />));
    const copy = [...host.querySelectorAll('button')].find((b) => b.textContent === 'Copy')!;
    await act(async () => copy.click());
    expect(writeText).toHaveBeenCalledWith(buildBookmarklet(window.location.origin));
  });

  it('opens the store listing for each Chromium browser, Edge and Firefox their own', async () => {
    const open = vi.fn();
    vi.stubGlobal('open', open);
    await act(async () => root.render(<BrowserTools />));
    const button = (name: string) => host.querySelector<HTMLButtonElement>(`button[aria-label="${name}"]`)!;
    for (const name of ['Chrome', 'Brave', 'Opera', 'Vivaldi']) {
      await act(async () => button(name).click());
      expect(open).toHaveBeenLastCalledWith(
        expect.stringContaining('chromewebstore.google.com/detail/knightloader/'),
        '_blank',
        'noopener,noreferrer',
      );
    }
    await act(async () => button('Edge').click());
    expect(open).toHaveBeenLastCalledWith(
      expect.stringContaining('microsoftedge.microsoft.com/addons/detail/knightloader/'),
      '_blank',
      'noopener,noreferrer',
    );
    await act(async () => button('Firefox').click());
    expect(open).toHaveBeenLastCalledWith(
      expect.stringContaining('addons.mozilla.org/addon/knightloader/'),
      '_blank',
      'noopener,noreferrer',
    );
    expect(open).toHaveBeenCalledTimes(6);
  });

  describe('in a container', () => {
    /** The page as a container at `version` draws it, and where its macOS button leads. */
    async function macOS(version: string): Promise<string> {
      vi.stubGlobal(
        'fetch',
        vi.fn((url: string) => {
          if (url.endsWith('/api/system/deployment')) return Promise.resolve(Response.json({ deployment: 'container' }));
          if (url.endsWith('/api/health')) return Promise.resolve(Response.json({ status: 'ok', version }));
          return new Promise(() => {});
        }),
      );
      await act(async () => root.render(<BrowserTools />));
      return host.querySelector<HTMLAnchorElement>('a[aria-label^="macOS"]')!.href;
    }

    it('gives the desktop app of the release it runs and names that release on the card', async () => {
      expect(await macOS('v1.8.4')).toContain('/releases/download/v1.8.4/knightloader-macos-universal.zip');
      const card = [...host.querySelectorAll('.glim-card')].find((c) => c.textContent?.includes('macOS'))!;
      expect(card.querySelector('a[href$="/releases/tag/v1.8.4"]')?.textContent).toBe('v1.8.4');
    });

    it('gives the newest desktop app where it runs no release, and names no number', async () => {
      expect(await macOS('v1.8.4-12-gabc123')).toContain('/releases/latest/download/knightloader-macos-universal.zip');
      const card = [...host.querySelectorAll('.glim-card')].find((c) => c.textContent?.includes('macOS'))!;
      expect(card.querySelector('a[href*="/releases/tag/"]')).toBeNull();
    });
  });

  describe('in the desktop app', () => {
    beforeEach(() => {
      vi.stubGlobal(
        'fetch',
        vi.fn((url: string) =>
          url.endsWith('/api/system/deployment')
            ? Promise.resolve(new Response(JSON.stringify({ deployment: 'desktop' })))
            : new Promise(() => {}),
        ),
      );
    });

    function relayButton() {
      return [...host.querySelectorAll('button')].find((b) => b.textContent?.startsWith('ParleyPort'));
    }

    it('orders the cards as the README orders its download rows', async () => {
      await act(async () => root.render(<BrowserTools />));
      const text = host.textContent!;
      const at = ['On a server', 'Phone app', 'Browser extension', 'Bookmarklet'].map((title) => text.indexOf(title));
      expect(at.every((i) => i >= 0)).toBe(true);
      expect(at).toEqual([...at].sort((a, b) => a - b));
    });

    it('offers ParleyPort on the server card', async () => {
      await act(async () => root.render(<BrowserTools />));
      expect(relayButton()?.textContent).toBe('ParleyPortOwn relay');
    });

    it('copies the command that starts the relay', async () => {
      await act(async () => root.render(<BrowserTools />));
      await act(async () => relayButton()!.click());
      expect(writeText).toHaveBeenCalledWith(RELAY_RUN_COMMAND);
      expect(relayButton()?.textContent).toBe('ParleyPortCopied');
    });
  });
});
