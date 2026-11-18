// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import type { HosterLogin, SiteLogin } from '../lib/api';
import { I18nProvider } from '../lib/i18n';
import { ToastProvider } from '../lib/toast';
import { HosterLoginSection, type HosterLogins } from './HosterLoginSection';
import { SiteState } from './SiteLoginDialog';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

class OffScreen {
  observe() {}
  disconnect() {}
}

beforeEach(() => {
  vi.stubGlobal('IntersectionObserver', OffScreen);
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => new Response(JSON.stringify({ available: false, site: '' }), { headers: { 'Content-Type': 'application/json' } })),
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

const patreon: SiteLogin = {
  id: 'patreon',
  name: 'Patreon',
  host: 'patreon.com',
  loginUrl: 'https://www.patreon.com/login',
  domains: ['patreon.com'],
  state: 'signedOut',
  unreadable: [],
  readsBrowsers: true,
  added: true,
};

/** The colour class the state's words wear. */
function inkOf(node: React.ReactNode): string | undefined {
  act(() => root.render(<I18nProvider>{node}</I18nProvider>));
  return host.querySelector('span')?.className.split(/\s+/).find((c) => /^text-(status|carbon)/.test(c));
}

it('paints a site that is not signed in, or whose session ran out, in the danger colour', () => {
  expect(inkOf(<SiteState site={patreon} />)).toBe('text-statusFail');
  expect(inkOf(<SiteState site={{ ...patreon, state: 'expired' }} />)).toBe('text-statusFail');
});

it('paints a signed-in site in the success colour', () => {
  expect(inkOf(<SiteState site={{ ...patreon, state: 'signedIn', account: 'Jane' }} />)).toBe('text-statusOk');
  expect(inkOf(<SiteState site={{ ...patreon, state: 'unchecked' }} />)).toBe('text-statusOk');
});

it('paints a hoster login that waits for a sign-in or a code in the danger colour', () => {
  const login = (extra: Partial<HosterLogin>): HosterLogin => ({
    host: 'katfile.com',
    username: 'fox',
    status: 'rejected',
    enabled: true,
    ...extra,
  });
  const data: HosterLogins = {
    logins: [login({ signInHere: true }), login({ host: 'ddownload.com', askCode: true })],
    sites: [],
    ways: { readsBrowsers: false },
    hosts: [],
    load: async () => {},
    toggle: async () => {},
    toggleAll: async () => {},
    remove: async () => {},
    putSite: () => {},
    addSite: async () => undefined,
    removeSite: async () => {},
  };
  act(() =>
    root.render(
      <I18nProvider>
        <ToastProvider>
          <HosterLoginSection data={data} adding={false} onAddClose={() => {}} />
        </ToastProvider>
      </I18nProvider>,
    ),
  );
  const inks = [...host.querySelectorAll('tbody tr')].map(
    (tr) => tr.children[1].querySelector('span')?.className.split(/\s+/).find((c) => /^text-status/.test(c)),
  );
  expect(inks).toEqual(['text-statusFail', 'text-statusFail']);
});
