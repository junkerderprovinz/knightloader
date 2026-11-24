// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { Access } from './Access';
import { SettingsProvider, type SettingsDraft } from './context';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// jsdom has no ResizeObserver, which GlimStone's selectors measure with.
globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};

let root: Root;
let host: HTMLDivElement;

/** What the kit says about this browser: an open instance without a password. */
const ME = { role: 'admin', via: 'open', owner: true, password: false, twoFactor: false, recoveryLeft: 0, fresh: true, askPassword: true };

function serve(access: object) {
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) => {
      const path = new URL(String(input), location.href).pathname;
      const body =
        path === '/api/system/access'
          ? access
          : path.endsWith('/api/signin/v1/me')
            ? ME
            : path.endsWith('/api/signin/v1/options')
              ? { open: true, owner: true, minPassword: 12, signedIn: false, passkeys: false, services: [] }
              : path.endsWith('/api/signin/v1/settings')
                ? { askPassword: true }
                : path.endsWith('/api/signin/v1/roles')
                  ? { roles: ['admin'], serviceRole: 'admin' }
                  : [];
      return Promise.resolve(new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } }));
    }),
  );
}

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

describe('Access', () => {
  async function render(access: object, trustedProxies: string[] = []) {
    serve(access);
    const features = { features: { modules: [], pages: [] }, toggle: async () => {} };
    const draft = {
      cfg: { trustedProxies },
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

  it('shows the sign-in tile from the kit', async () => {
    await render({ behindProxy: false });
    expect(host.querySelector('[data-signin-card="password"]')).not.toBeNull();
  });

  it('names the encryption key without a warning', async () => {
    await render({ behindProxy: false });
    const line = host.querySelector('[data-testid="app-key"]')!;
    expect(line.textContent).toContain('Encryption key');
    expect(line.className).not.toMatch(/statusWarn/);
    expect(line.querySelector('a[href*="#encryption-key"]')).not.toBeNull();
  });

  it('says nothing about old copies once the key has been in use long enough', async () => {
    await render({ behindProxy: false });
    expect(host.textContent).not.toContain('from before the encryption key');
  });

  it('says when the copies from before the encryption key go', async () => {
    await render({ behindProxy: false, appKeyCopiesUntil: '2026-10-18T12:00:00Z' });
    expect(host.textContent).toContain('A copy of the files from before the encryption key is still in the data folder.');
  });

  it('leaves the trusted proxies out while no request comes through a proxy', async () => {
    await render({ behindProxy: false });
    expect(host.textContent).not.toContain('Trusted proxies');
  });

  it('offers the trusted proxies once requests come through a proxy', async () => {
    await render({ behindProxy: true });
    expect(host.textContent).toContain('Trusted proxies');
  });

  it('keeps the trusted proxies in view while the list has entries', async () => {
    await render({ behindProxy: false }, ['10.0.0.2']);
    expect(host.textContent).toContain('Trusted proxies');
  });
});
