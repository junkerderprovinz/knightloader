// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { I18nProvider } from '../../../lib/i18n';
import { peekUIState, writeUIState } from '../../../lib/uistate';
import { ClipboardPopupRows } from './ClipboardPopup';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

async function render() {
  await act(async () =>
    root.render(
      <I18nProvider>
        <ClipboardPopupRows />
      </I18nProvider>,
    ),
  );
}

const button = (text: string) =>
  [...host.querySelectorAll('button')].find((b) => b.textContent?.trim() === text || b.getAttribute('aria-label') === text);

beforeEach(() => {
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => new Response('{}', { status: 200, headers: { 'Content-Type': 'application/json' } })),
  );
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  for (const field of ['clipboardWatchPopup', 'clipboardWatchPopupSeconds', 'clipboardWatchPopupPlace']) {
    writeUIState(field, undefined);
  }
  vi.unstubAllGlobals();
});

describe('the popup settings', () => {
  it('offer the time and the place while the popup is on, five seconds at the lower right at first', async () => {
    await render();
    expect(host.textContent).toContain('How long it stays');
    expect(host.textContent).toContain('Where it shows');
    expect(host.textContent).toContain('Bottom right');
    expect(button('5 sec')?.getAttribute('aria-pressed') ?? button('5 sec')?.getAttribute('aria-selected')).toBe('true');
  });

  it('keep the time picked, until clicked included', async () => {
    await render();
    await act(async () => button('Until clicked')!.click());
    expect(peekUIState('clipboardWatchPopupSeconds', 5)).toBe(0);
    await act(async () => button('15 sec')!.click());
    expect(peekUIState('clipboardWatchPopupSeconds', 5)).toBe(15);
  });

  it('keep the place picked', async () => {
    await render();
    await act(async () => host.querySelector<HTMLButtonElement>('[aria-label="Where it shows: Bottom right"]')!.click());
    const item = [...document.querySelectorAll<HTMLElement>('[role^="menuitem"]')].find((o) => o.textContent?.includes('Top left'));
    await act(async () => item!.click());
    expect(peekUIState('clipboardWatchPopupPlace', 'bottom-right')).toBe('top-left');
  });

  it('hide the time and the place with the popup switched off', async () => {
    writeUIState('clipboardWatchPopup', false);
    await render();
    expect(host.textContent).toContain('Pop up when links come in');
    expect(host.textContent).not.toContain('How long it stays');
    expect(host.textContent).not.toContain('Where it shows');
  });
});
