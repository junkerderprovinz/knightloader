// @vitest-environment jsdom
import { act, useState } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import type { SiteLogin, Task } from '../lib/api';
import { I18nProvider } from '../lib/i18n';
import { ToastProvider } from '../lib/toast';
import { FailureAdvice } from './FailureAdvice';
import { HosterLoginSection, useHosterLogins } from './HosterLoginSection';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;
let calls: { url: string; method: string; body?: unknown }[];
let opened: string[];
let sites: SiteLogin[];

const patreon: SiteLogin = {
  id: 'patreon',
  name: 'Patreon',
  host: 'patreon.com',
  loginUrl: 'https://www.patreon.com/login',
  domains: ['patreon.com'],
  state: 'signedIn',
  account: 'Jane Doe',
  waiting: false,
};
const youtube: SiteLogin = {
  id: 'youtube',
  name: 'YouTube',
  host: 'youtube.com',
  loginUrl: 'https://accounts.google.com/ServiceLogin',
  domains: ['youtube.com'],
  state: 'expired',
  waiting: false,
};

const reply = (body: unknown) =>
  new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } });

class OffScreen {
  observe() {}
  disconnect() {}
}

const button = (text: string) =>
  [...document.querySelectorAll<HTMLButtonElement>('button')].reverse().find((b) => b.textContent === text);

const row = (label: string) =>
  [...host.querySelectorAll<HTMLTableRowElement>('tbody tr')].find((tr) => tr.textContent?.includes(label));

/** The picker's tile for a brand. */
const tile = (name: string) =>
  [...document.querySelectorAll<HTMLLIElement>('.glim-readme-btn-unit')].find(
    (li) => li.querySelector('.glim-readme-btn-name')?.textContent === name,
  );

beforeEach(() => {
  calls = [];
  opened = [];
  sites = [patreon, youtube];
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  vi.stubGlobal('IntersectionObserver', OffScreen);
  vi.stubGlobal('open', (url: string) => {
    opened.push(url);
    return null;
  });
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? 'GET';
      calls.push({ url, method, body: init?.body ? JSON.parse(String(init.body)) : undefined });
      const site = sites.find((s) => url.includes(`/${s.id}/`)) ?? patreon;
      if (url === '/api/sitelogins') return reply(sites);
      if (url === '/api/hosterauth/hosts') return reply([{ id: 'rapidgator.net', label: 'rapidgator.net' }]);
      if (url.endsWith('/begin')) return reply({ ...site, waiting: true });
      if (url.endsWith('/cookies')) return reply({ ...site, state: 'signedIn', account: 'Jane Doe' });
      if (url.endsWith('/remove')) return reply({ ...site, state: 'signedOut', account: undefined });
      if (url.endsWith('/check')) return reply(site);
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
  document.body.replaceChildren();
  vi.unstubAllGlobals();
});

function HosterCard() {
  const data = useHosterLogins();
  const [adding, setAdding] = useState(false);
  return (
    <>
      <button type="button" onClick={() => setAdding(true)}>
        Add
      </button>
      <HosterLoginSection data={data} adding={adding} onAddClose={() => setAdding(false)} />
    </>
  );
}

async function renderCard() {
  await act(async () => {
    root.render(
      <I18nProvider>
        <ToastProvider>
          <HosterCard />
        </ToastProvider>
      </I18nProvider>,
    );
  });
}

async function pick(name: string) {
  await act(async () => button('Add')!.click());
  await act(async () => tile(name)!.querySelector('button')!.click());
}

it('lists a signed-in site among the hoster accounts, saying who it signs in as', async () => {
  await renderCard();
  expect(row('Patreon')?.textContent).toContain('Signed in as Jane Doe');
  expect(row('Patreon')?.querySelector('.text-statusWarn')).toBeNull();
});

it('warns on the row of a site whose sign-in ran out', async () => {
  await renderCard();
  const expired = row('YouTube')!;
  expect(expired.textContent).toContain('Sign-in ran out');
  expect(expired.querySelector('.text-statusWarn')).not.toBeNull();
});

it('leaves a site nobody signed in to out of the list', async () => {
  sites = [{ ...patreon, state: 'signedOut', account: undefined }];
  await renderCard();
  expect(row('Patreon')).toBeUndefined();
});

it('offers the sites among the hosters and signs in through the browser instead of a password', async () => {
  sites = [{ ...patreon, state: 'signedOut', account: undefined }, youtube];
  await renderCard();
  await act(async () => button('Add')!.click());
  expect(tile('rapidgator.net')).toBeDefined();
  expect(tile('Patreon')).toBeDefined();
  expect(tile('YouTube')).toBeUndefined();
  await act(async () => tile('Patreon')!.querySelector('button')!.click());
  expect(document.body.textContent).not.toContain('Username');
  await act(async () => button('Sign in in the browser')!.click());
  expect(opened).toEqual(['https://www.patreon.com/login']);
  expect(calls.some((c) => c.url === '/api/sitelogins/patreon/begin' && c.method === 'POST')).toBe(true);
  expect(document.body.textContent).toContain('click the KnightLoader button in your browser toolbar');
});

it('takes a pasted cookies.txt for a picked site', async () => {
  sites = [{ ...patreon, state: 'signedOut', account: undefined }];
  await renderCard();
  await pick('Patreon');
  await act(async () => button('Paste cookies.txt')!.click());
  const box = document.querySelector('textarea')!;
  await act(async () => {
    const set = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!;
    set.call(box, '# Netscape HTTP Cookie File\n.patreon.com\tTRUE\t/\tTRUE\t0\tsession_id\tx\n');
    box.dispatchEvent(new Event('input', { bubbles: true }));
  });
  await act(async () => button('Save')!.click());
  const paste = calls.find((c) => c.url === '/api/sitelogins/patreon/cookies');
  expect(paste?.body).toEqual({ text: '# Netscape HTTP Cookie File\n.patreon.com\tTRUE\t/\tTRUE\t0\tsession_id\tx\n' });
  expect(document.body.textContent).toContain('Signed in to Patreon.');
});

it('signs in again from the row menu of a site', async () => {
  await renderCard();
  await act(async () => row('YouTube')!.querySelector<HTMLButtonElement>('[aria-label="Account actions"]')!.click());
  await act(async () => button('Sign in again')!.click());
  await act(async () => button('Sign in again')!.click());
  expect(opened).toEqual(['https://accounts.google.com/ServiceLogin']);
});

it('removes a site from the row menu after asking', async () => {
  await renderCard();
  await act(async () => row('Patreon')!.querySelector<HTMLButtonElement>('[aria-label="Account actions"]')!.click());
  await act(async () => button('Remove')!.click());
  expect(calls.some((c) => c.url.endsWith('/remove'))).toBe(false);
  await act(async () => button('Remove')!.click());
  expect(calls.some((c) => c.url === '/api/sitelogins/patreon/remove' && c.method === 'POST')).toBe(true);
});

const failed: Task = {
  id: 't1',
  url: 'https://www.patreon.com/posts/a-post-1',
  name: 'a-post-1',
  package: '',
  resolver: 'ytdlp',
  size: 0,
  loaded: 0,
  speed: 0,
  status: 'error',
  createdAt: '2026-11-13T12:00:00Z',
  priority: 0,
  position: 0,
  enabled: true,
  error: 'ERROR: [patreon] 1: You do not have access to this post',
  reason: 'membersOnly',
};

async function renderAdvice(task: Task, base = '/api') {
  await act(async () => {
    root.render(
      <I18nProvider>
        <ToastProvider>
          <FailureAdvice task={task} base={base} reasonLabel="failure.membersOnly.line" onClose={() => {}} />
        </ToastProvider>
      </I18nProvider>,
    );
  });
}

it('asks to sign in again when a members-only download fails on a site with a stored session', async () => {
  await renderAdvice(failed);
  expect(document.body.textContent).toContain('your Patreon sign-in did not get in');
  expect(button('Store cookies for this site')).toBeUndefined();
  await act(async () => button('Sign in again')!.click());
  expect(document.body.textContent).toContain('Sign in to Patreon');
});

it('keeps the plain cookie advice for a site without a sign-in', async () => {
  await renderAdvice({ ...failed, url: 'https://vimeo.com/1' });
  expect(button('Store cookies for this site')).toBeDefined();
  expect(button('Sign in again')).toBeUndefined();
});

it('keeps the plain advice for a row of another instance, whose sign-ins are its own', async () => {
  await renderAdvice(failed, '/api/instances/attic');
  expect(calls.some((c) => c.url === '/api/sitelogins')).toBe(false);
  expect(button('Sign in again')).toBeUndefined();
});
