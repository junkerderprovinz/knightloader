// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { Access, TokensSection } from './Access';
import { SettingsProvider, type SettingsDraft } from './context';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// jsdom has no ResizeObserver, which the rights presets' tab strip measures with.
globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};

let root: Root;
let host: HTMLDivElement;

beforeEach(() => {
  // No tokens yet, which is all the card asks the server for.
  vi.stubGlobal(
    'fetch',
    vi.fn(() => Promise.resolve(new Response('[]', { headers: { 'Content-Type': 'application/json' } }))),
  );
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

describe('TokensSection', () => {
  it('labels the new token window’s name field Name, not with the card title', async () => {
    // The card's module switch finds no module and draws nothing.
    const features = { features: { modules: [], pages: [] }, toggle: async () => {} };
    await act(async () =>
      root.render(
        <SettingsProvider draft={{} as SettingsDraft} features={features}>
          <TokensSection />
        </SettingsProvider>,
      ),
    );
    const open = [...host.querySelectorAll('button')].find((b) => b.textContent === 'New token')!;
    await act(async () => open.click());
    // The window is a portal on the body.
    const input = document.body.querySelector<HTMLInputElement>('input[placeholder="e.g. Sonarr"]')!;
    const caption = input.closest('label')!.querySelector('[data-glim-label]')!;
    expect(caption.getAttribute('data-glim-label')).toBe('Name');
  });
});

describe('Access', () => {
  async function renderWithAuth(auth: object, trustedProxies: string[] = []) {
    vi.stubGlobal(
      'fetch',
      vi.fn((url: string) => {
        const body = url === '/api/auth' ? auth : url === '/api/remote-access' ? { exposed: false } : [];
        return Promise.resolve(new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } }));
      }),
    );
    const features = { features: { modules: [], pages: [] }, toggle: async () => {} };
    const draft = {
      cfg: { trustedProxies },
      saved: { trustedProxies },
      patch: () => {},
      fieldError: () => undefined,
      showRefusalsAt: () => () => {},
    } as unknown as SettingsDraft;
    await act(async () =>
      root.render(
        <MemoryRouter>
          <SettingsProvider draft={draft} features={features}>
            <Access />
          </SettingsProvider>
        </MemoryRouter>,
      ),
    );
  }

  it('names the app key without a warning when there is none', async () => {
    await renderWithAuth({ enabled: true, authenticated: true, appKey: false });
    const line = host.querySelector('[data-new="app-key"]')!;
    expect(line.textContent).toContain('App key');
    expect(line.className).not.toMatch(/statusWarn/);
    expect(line.querySelector('a[href*="#app-key"]')).not.toBeNull();
  });

  it('says nothing about old copies once the key has been in use long enough', async () => {
    await renderWithAuth({ enabled: true, authenticated: true, appKey: true });
    expect(host.textContent).not.toContain('from before the app key');
  });

  it('says when the copies from before the app key go', async () => {
    await renderWithAuth({ enabled: true, authenticated: true, appKey: true, appKeyCopiesUntil: '2026-10-18T12:00:00Z' });
    expect(host.textContent).toContain('A copy of the files from before the app key is still in the data folder.');
  });

  it('leaves the trusted proxies out while no request comes through a proxy', async () => {
    await renderWithAuth({ enabled: true, authenticated: true, appKey: true, proxied: false });
    expect(host.textContent).not.toContain('Trusted proxies');
  });

  it('offers the trusted proxies once requests come through a proxy', async () => {
    await renderWithAuth({ enabled: true, authenticated: true, appKey: true, proxied: true });
    expect(host.textContent).toContain('Trusted proxies');
  });

  it('keeps the trusted proxies in view while the list has entries', async () => {
    await renderWithAuth({ enabled: true, authenticated: true, appKey: true, proxied: false }, ['10.0.0.2']);
    expect(host.textContent).toContain('Trusted proxies');
  });
});
