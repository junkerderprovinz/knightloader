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
let tab: { opener: unknown; closed: boolean; document: Document; location: { replace: (url: string) => void }; went: string[]; shown: string[] };
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
  listed: true,
};
const fromFirefox: SiteLogin = { ...patreon, state: 'signedIn', account: 'Jane Doe', source: 'browser', browser: 'Firefox' };
const extension: GroupApp = { id: 'm1', name: 'Firefox', deployment: 'extension', connected: true, approved: true, lastSeen: 0 };

const reply = (body: unknown) =>
  new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } });

let socket: QuietSocket | undefined;

class QuietSocket {
  readyState = 0;
  onmessage: ((ev: { data: unknown }) => void) | null = null;
  constructor() {
    socket = this;
  }
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
  socket = undefined;
  apps = [];
  listed = patreon;
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  vi.stubGlobal('WebSocket', QuietSocket);
  vi.stubGlobal('ResizeObserver', StillBox);
  vi.stubGlobal('IntersectionObserver', StillBox);
  tab = newTab();
  vi.stubGlobal('open', (url: string) => {
    opened.push(url);
    return tab;
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

// A tab as window.open hands it back: an empty page of this origin, which
// notes the links followed in it and the address it is sent to.
function newTab() {
  const doc = document.implementation.createHTMLDocument('');
  const t = { opener: window as unknown, closed: false, document: doc, went: [] as string[], shown: [] as string[], location: { replace: (url: string) => t.shown.push(url) } };
  doc.addEventListener('click', (e) => {
    const a = e.target as HTMLAnchorElement;
    t.went.push(`${a.href} ${a.rel}`);
    e.preventDefault();
  });
  return t;
}

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

const footerButtons = () => [...document.querySelectorAll<HTMLButtonElement>('.glim-modal-foot button')];
const doneButton = () => document.querySelector<HTMLButtonElement>('[data-signin-done]')!;

async function hear(msg: unknown) {
  await act(async () => socket!.onmessage!({ data: JSON.stringify(msg) }));
}

const browserButton = () =>
  [...document.querySelectorAll<HTMLButtonElement>('button')].find((b) => b.textContent === en['signInBrowser.openInBrowser']);

it('opens the site in the browser and closes once KnightLoader reads the sign-in from there', async () => {
  const onClose = vi.fn();
  await open(patreon, onClose);
  await act(async () => browserButton()!.click());
  expect(opened).toEqual(['']);
  expect(tab.went).toEqual(['https://www.patreon.com/login noreferrer']);
  expect(tab.opener).toBeNull();
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
  expect(tab.shown).toEqual([`${location.origin}/signedin?site=Patreon`]);
});

it('leaves a tab the user closed alone', async () => {
  await open(patreon);
  await act(async () => browserButton()!.click());
  tab.closed = true;
  listed = fromFirefox;
  await act(async () => {
    await vi.advanceTimersByTimeAsync(3000);
  });
  expect(tab.shown).toEqual([]);
});

it('offers no browser in a container, also with an extension that can hand the sign-in over', async () => {
  apps = [extension];
  await open({ ...patreon, readsBrowsers: false, handsOver: true });
  expect(browserButton()).toBeUndefined();
  expect(document.body.textContent).not.toContain('KnightLoader extension');
});

it('offers no browser in a container without an extension and names none', async () => {
  await open({ ...patreon, readsBrowsers: false });
  expect(browserButton()).toBeUndefined();
  expect(document.body.textContent).not.toContain('KnightLoader extension');
});

it('offers no browser on a hoster login, which only signs in on its own page', async () => {
  await open({ ...patreon, id: 'katfile', name: 'katfile.com', loginUrl: '', readsBrowsers: false });
  expect(browserButton()).toBeUndefined();
  expect(document.body.textContent).not.toContain('KnightLoader extension');
});

it('keeps Done shut until the site reports the sign-in, then says so over the page', async () => {
  await open(patreon);
  expect(doneButton().disabled).toBe(true);
  expect(document.body.textContent).not.toContain('You are signed in to Patreon');

  await hear({ type: 'login', login: { ...patreon, state: 'signedIn', account: 'Jane Doe' } });
  expect(doneButton().disabled).toBe(false);
  const note = document.querySelector('.kl-signed-in [role="status"]')!;
  expect(note.textContent).toContain('You are signed in to Patreon');
  expect(note.textContent).toContain('Signed in as Jane Doe');
  expect(note.textContent).toContain('Click Done below, and KnightLoader keeps the session.');

  await hear({ type: 'login', login: patreon });
  expect(doneButton().disabled).toBe(true);
  expect(document.querySelector('.kl-signed-in')).toBeNull();
});

it('keeps the sign-in note when a blocked page has its say', async () => {
  await open(patreon);
  await hear({ type: 'login', login: { ...patreon, state: 'signedIn' } });
  await hear({ type: 'blocked', host: 'ads.example' });
  expect(document.querySelector('.kl-signed-in')).not.toBeNull();
  expect(doneButton().disabled).toBe(false);
});

it('starts the row with Back in words and ends it with Done', async () => {
  await open(patreon);
  const row = footerButtons();
  expect(row[0].textContent).toBe(en['signInBrowser.back']);
  expect(row[row.length - 1]).toBe(doneButton());
});
