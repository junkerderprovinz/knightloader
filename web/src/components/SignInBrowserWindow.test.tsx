// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import type { GroupApp, SiteLogin } from '../lib/api';
import { I18nProvider } from '../lib/i18n';
import { en } from '../lib/locales/en';
import { ToastProvider } from '../lib/toast';
import { SignInBrowserWindow } from './SignInBrowserWindow';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;
let calls: string[];
let opened: string[];
let apps: GroupApp[];
let listed: SiteLogin;

const patreon: SiteLogin = {
  id: 'patreon',
  name: 'Patreon',
  host: 'patreon.com',
  loginUrl: 'https://www.patreon.com/login',
  domains: ['patreon.com'],
  state: 'signedOut',
  unreadable: [],
  readsBrowsers: true,
  disconnected: false,
};
const fromFirefox: SiteLogin = { ...patreon, state: 'signedIn', account: 'Jane Doe', source: 'browser', browser: 'Firefox' };
const extension: GroupApp = { id: 'm1', name: 'Firefox', deployment: 'extension', connected: true, approved: true, lastSeen: 0 };

const reply = (body: unknown) =>
  new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } });

class QuietSocket {
  readyState = 0;
  send() {}
  close() {}
}

class StillBox {
  observe() {}
  disconnect() {}
}

beforeEach(() => {
  vi.useFakeTimers();
  calls = [];
  opened = [];
  apps = [];
  listed = patreon;
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  vi.stubGlobal('WebSocket', QuietSocket);
  vi.stubGlobal('ResizeObserver', StillBox);
  vi.stubGlobal('IntersectionObserver', StillBox);
  vi.stubGlobal('open', (url: string) => {
    opened.push(url);
    return null;
  });
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string) => {
      calls.push(url);
      if (url === '/api/connect') return reply({ active: true, apps });
      if (url === '/api/sitelogins') return reply([listed]);
      if (url === '/api/signinbrowser/close') return reply(listed);
      return reply(patreon);
    }),
  );
  host = document.createElement('div');
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  document.body.replaceChildren();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

async function open(site: SiteLogin, onClose = () => {}) {
  await act(async () => {
    root.render(
      <I18nProvider>
        <ToastProvider>
          <SignInBrowserWindow site={site} onClose={onClose} />
        </ToastProvider>
      </I18nProvider>,
    );
  });
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0);
  });
}

const browserButton = () =>
  [...document.querySelectorAll<HTMLButtonElement>('button')].find((b) => b.textContent === en['signInBrowser.openInBrowser']);

it('opens the site in the browser and closes once KnightLoader reads the sign-in from there', async () => {
  const onClose = vi.fn();
  await open(patreon, onClose);
  await act(async () => browserButton()!.click());
  expect(opened).toEqual(['https://www.patreon.com/login']);
  expect(calls).toContain('/api/sitelogins/patreon/begin');
  expect(document.body.textContent).toContain('Sign in to Patreon in your browser. KnightLoader picks it up by itself.');

  await act(async () => {
    await vi.advanceTimersByTimeAsync(3000);
  });
  expect(onClose).not.toHaveBeenCalled();

  listed = fromFirefox;
  await act(async () => {
    await vi.advanceTimersByTimeAsync(3000);
  });
  expect(calls).toContain('/api/signinbrowser/close');
  expect(onClose).toHaveBeenCalled();
});

it('offers the browser where the extension of the group can hand the sign-in over', async () => {
  apps = [extension];
  await open({ ...patreon, readsBrowsers: false });
  expect(browserButton()).toBeDefined();
});

it('offers no browser where nothing could pick the sign-in up from there', async () => {
  await open({ ...patreon, readsBrowsers: false });
  expect(browserButton()).toBeUndefined();
});

it('offers no browser on a hoster login, which only signs in on its own page', async () => {
  await open({ ...patreon, id: 'katfile', name: 'katfile.com', loginUrl: '' });
  expect(browserButton()).toBeUndefined();
});
