// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ConnectionsCard } from './Connections';
import { SettingsProvider, type SettingsDraft } from './context';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// jsdom has no ResizeObserver, which a selector in the window measures with.
globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};

let root: Root;
let host: HTMLDivElement;

const proxy = { id: 'a', type: 'http', host: 'proxy.example.org', port: 8080, enabled: true, order: 0 };

beforeEach(() => {
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

async function mount() {
  const draft = {
    cfg: { connections: [proxy] },
    patch: () => {},
    fieldError: () => undefined,
    showRefusalsAt: () => () => {},
  } as unknown as SettingsDraft;
  // The card's module switch finds no module and draws nothing.
  const features = { features: { modules: [], pages: [] }, toggle: async () => {} };
  await act(async () =>
    root.render(
      <SettingsProvider draft={draft} features={features}>
        <ConnectionsCard hue={0} />
      </SettingsProvider>,
    ),
  );
}

const row = () => host.querySelector<HTMLButtonElement>('button[aria-haspopup="dialog"]')!;
const sheet = () => host.querySelector<HTMLElement>('[role="dialog"]');
const named = (text: string) => [...host.querySelectorAll('button')].find((b) => b.textContent === text)!;

describe('a connection row', () => {
  it('opens its settings in a window over the page instead of unfolding', async () => {
    await mount();
    expect(sheet()).toBeNull();
    expect(row().hasAttribute('aria-expanded')).toBe(false);

    await act(async () => row().click());
    const win = sheet()!;
    expect(win.getAttribute('aria-modal')).toBe('true');
    // Named after the row it belongs to, with the row's fields in it.
    expect(win.querySelector('h2')!.textContent).toBe('proxy.example.org:8080');
    expect(win.querySelector<HTMLInputElement>('input[placeholder="proxy.example.org"]')!.value).toBe('proxy.example.org');

    await act(async () => named('Close').click());
    expect(sheet()).toBeNull();
  });

  it('shows what the test came to on the test button, with the reason above it', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          new Response(JSON.stringify({ ok: false, stage: 'dial', detail: 'connection refused', millis: 12 }), {
            headers: { 'Content-Type': 'application/json' },
          }),
        ),
      ),
    );
    await mount();
    await act(async () => row().click());
    const test = named('Test');
    await act(async () => test.click());

    expect(test.textContent).toBe('Not connected');
    expect(test.className).toContain('bg-statusFailSolid');
    const reason = [...sheet()!.querySelectorAll('p')].find((p) => p.textContent === 'connection refused')!;
    expect(reason.className).not.toMatch(/text-status/);
    // The reason stands before the button in the window.
    expect(reason.compareDocumentPosition(test) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });
});
