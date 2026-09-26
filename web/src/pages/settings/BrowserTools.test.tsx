// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { buildBookmarklet } from '../../lib/browserTools';
import { BrowserTools } from './BrowserTools';

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
  // jsdom has no clipboard, and the copy button is only offered with one.
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
  return [...host.querySelectorAll('a')].find((a) => a.textContent === 'Add to KnightLoader')!;
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

  it('copies the same code with the button beside the link', async () => {
    await act(async () => root.render(<BrowserTools />));
    const copy = [...host.querySelectorAll('button')].find((b) => b.textContent === 'Copy the code instead')!;
    await act(async () => copy.click());
    expect(writeText).toHaveBeenCalledWith(buildBookmarklet(window.location.origin));
  });
});
