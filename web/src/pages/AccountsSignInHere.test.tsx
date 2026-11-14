// @vitest-environment jsdom
// Its own file, since whether a sign-in browser is there is asked once per page.
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import { I18nProvider } from '../lib/i18n';
import { ToastProvider } from '../lib/toast';
import { Accounts } from './Accounts';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

const reply = (body: unknown) =>
  new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } });

class QuietSocket {
  readyState = 0;
  send() {}
  close() {}
}

class OffScreen {
  observe() {}
  disconnect() {}
}

Object.assign(SVGSVGElement.prototype, { pauseAnimations() {}, unpauseAnimations() {}, setCurrentTime() {} });

const waiting = {
  host: 'katfile.com',
  username: 'fox',
  status: 'rejected',
  code: 'ownSignInHere',
  enabled: true,
  signInSite: 'katfile',
  signInHere: true,
};

beforeEach(() => {
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  vi.stubGlobal('WebSocket', QuietSocket);
  vi.stubGlobal('IntersectionObserver', OffScreen);
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string) => {
      if (url === '/api/hosterauth/logins') return reply([waiting]);
      if (url === '/api/signinbrowser') return reply({ available: true, browser: 'Chromium', site: '' });
      if (url === '/api/settings') return reply({ premiumOnly: false });
      if (url === '/api/features') return reply({ modules: [], pages: [] });
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
  vi.unstubAllGlobals();
});

it('says a hoster login waits for a sign-in on the site’s own page and offers one in its window', async () => {
  await act(async () =>
    root.render(
      <I18nProvider>
        <ToastProvider>
          <MemoryRouter>
            <Accounts />
          </MemoryRouter>
        </ToastProvider>
      </I18nProvider>,
    ),
  );

  const row = [...host.querySelectorAll('tr')].find((r) => r.textContent?.includes('katfile.com'))!;
  expect(row.textContent).toContain('Needs a sign-in');
  await act(async () => (row.querySelectorAll('td')[1] as HTMLElement).click());

  const dialog = document.querySelector('[role="dialog"]')!;
  expect(dialog.textContent).toContain('Does the site ask for a code when you sign in?');
  const here = [...dialog.querySelectorAll('button')].find((b) => b.textContent === 'Sign in here');
  expect(here?.disabled).toBe(false);
});
