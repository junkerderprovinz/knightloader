// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import { I18nProvider } from '../lib/i18n';
import { setHidden } from '../lib/sidebarPrefs';
import { ToastProvider } from '../lib/toast';
import { Sidebar } from './Sidebar';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};

let root: Root;
let host: HTMLDivElement;
let locked: boolean;
let hide: { accounts: boolean; instances: boolean; overview?: boolean };

const reply = (body: unknown) =>
  new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } });

beforeEach(() => {
  locked = false;
  hide = { accounts: false, instances: false };
  // A phone's width, where the rail is the bar.
  vi.stubGlobal('matchMedia', () => ({ matches: true, addEventListener() {}, removeEventListener() {} }));
  vi.stubGlobal('WebSocket', class {
    close() {}
    send() {}
  });
  Object.defineProperty(document, 'fonts', { configurable: true, value: { ready: Promise.resolve() } });
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string) => {
      if (url === '/api/auth') return reply({ enabled: locked });
      if (url === '/api/settings')
        return reply({
          hideAccountsFromSidebar: hide.accounts,
          hideInstancesFromSidebar: hide.instances,
          hideOverviewFromSidebar: hide.overview ?? false,
        });
      if (url.startsWith('/api/uistate')) return reply({});
      return reply([]);
    }),
  );
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  setHidden('accounts', false);
  setHidden('instances', false);
  setHidden('overview', false);
  vi.unstubAllGlobals();
});

async function show(): Promise<string[]> {
  await act(async () => {
    root.render(
      <I18nProvider>
        <ToastProvider>
          <MemoryRouter>
            <Sidebar />
          </MemoryRouter>
        </ToastProvider>
      </I18nProvider>,
    );
  });
  return slots();
}

/** The words of the bar's segments, in their order. */
const slots = () => [...host.querySelectorAll('nav > a, nav > button, nav > div > button')].map((s) => s.textContent ?? '');

it('keeps four places and puts pinned pages behind More', async () => {
  expect(await show()).toEqual(['Overview', 'Downloads', 'Link collector', 'Settings', 'More']);
});

it('lists what stands behind More in a sheet', async () => {
  await show();
  await act(async () => (host.querySelector('nav > button') as HTMLButtonElement).click());
  const rows = [...document.querySelectorAll('[role="dialog"] .glim-row')].map((r) => r.textContent);
  expect(rows).toEqual(['Instances', 'Accounts', 'Events']);
});

it('gives the last slot to the bell where nothing else is left over', async () => {
  hide = { accounts: true, instances: true };
  expect(await show()).toEqual(['Overview', 'Downloads', 'Link collector', 'Settings', 'Events']);
});

it('offers Sign out in the sheet once the instance has a password', async () => {
  hide = { accounts: true, instances: true };
  locked = true;
  expect(await show()).toContain('More');
  await act(async () => (host.querySelector('nav > button') as HTMLButtonElement).click());
  const buttons = [...document.querySelectorAll('[role="dialog"] button')].map((b) => b.textContent);
  expect(buttons).toContain('Sign out');
});

it('leaves the overview out once it is switched off', async () => {
  hide = { accounts: false, instances: false, overview: true };
  expect(await show()).toEqual(['Downloads', 'Link collector', 'Settings', 'More']);
});
