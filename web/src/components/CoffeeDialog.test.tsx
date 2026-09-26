// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { About } from '../pages/settings/Help';

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

beforeEach(() => {
  // The settings behind the card come from the server, which is not here.
  vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})));
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

/** The button by its accessible name: Buy Me a Coffee's shows its artwork, not words. */
function button(name: string) {
  return [...host.querySelectorAll('button')].find((b) => b.getAttribute('aria-label') === name)!;
}

describe('CoffeeDialog', () => {
  it('frames the widget page only once the window opens', async () => {
    await act(async () => root.render(<About hue={0} />));
    expect(document.querySelector('iframe')).toBeNull();

    await act(async () => button('Buy me a coffee').click());
    const frame = document.querySelector('iframe')!;
    expect(frame.getAttribute('src')).toBe(
      'https://buymeacoffee.com/widget/page/junkerderprovinz?description=&color=%23FFDD00',
    );
    expect(frame.getAttribute('allow')).toBe('payment');
    expect(document.querySelector('[role="dialog"]')!.textContent).toContain(
      'The payment runs through Buy Me a Coffee. You do not need an account.',
    );
  });

  it('closes on Escape and takes the frame with it', async () => {
    await act(async () => root.render(<About hue={0} />));
    await act(async () => button('Buy me a coffee').click());
    await act(async () => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    });
    expect(document.querySelector('iframe')).toBeNull();
  });
});
