// @vitest-environment jsdom
import { act, useState } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { GroupApp, SiteLogin, Task } from '../lib/api';
import { I18nProvider } from '../lib/i18n';
import { en } from '../lib/locales/en';
import { ToastProvider } from '../lib/toast';
import { FailureAdvice } from './FailureAdvice';
import { HosterLoginSection, useHosterLogins } from './HosterLoginSection';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;
let calls: { url: string; method: string; body?: unknown }[];
let opened: string[];
let sites: SiteLogin[];
let apps: GroupApp[];
let shown: string[];

// A tab as window.open hands it back: an empty page whose followed links land
// in opened and whose address the page sets lands in shown.
function newTab() {
  const doc = document.implementation.createHTMLDocument('');
  doc.addEventListener('click', (e) => {
    opened.push((e.target as HTMLAnchorElement).href);
    e.preventDefault();
  });
  return { opener: window, closed: false, document: doc, location: { replace: (u: string) => shown.push(u) } };
}

const patreon: SiteLogin = {
  id: 'patreon',
  name: 'Patreon',
  host: 'patreon.com',
  loginUrl: 'https://www.patreon.com/login',
  domains: ['patreon.com'],
  state: 'signedIn',
  account: 'Jane Doe',
  source: 'browser',
  browser: 'Firefox (default-release)',
  unreadable: [],
  readsBrowsers: true,
  listed: true,
};
const youtube: SiteLogin = {
  id: 'youtube',
  name: 'YouTube',
  host: 'youtube.com',
  loginUrl: 'https://accounts.google.com/ServiceLogin',
  domains: ['youtube.com'],
  links: ['youtu.be'],
  state: 'expired',
  source: 'extension',
  unreadable: [],
  readsBrowsers: true,
  listed: true,
};
const instagram: SiteLogin = {
  id: 'instagram',
  name: 'Instagram',
  host: 'instagram.com',
  loginUrl: 'https://www.instagram.com/accounts/login/',
  domains: ['instagram.com'],
  links: ['instagr.am'],
  state: 'signedOut',
  unreadable: [],
  readsBrowsers: true,
  listed: false,
};
const signedOut = (s: SiteLogin): SiteLogin => ({ ...s, state: 'signedOut', account: undefined, source: '', browser: '' });
const notAdded = (s: SiteLogin): SiteLogin => ({ ...signedOut(s), listed: false });
let hosterRows: unknown[];
const browserExtension: GroupApp = { id: 'm1', name: 'Firefox', deployment: 'extension', connected: false, approved: true, lastSeen: 0 };

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

/** What the (i) beside a row's state says. */
const hint = (label: string) => row(label)?.querySelector('[role="note"]')?.getAttribute('aria-label') ?? '';

/** The picker's tile for a brand. */
const tile = (name: string) =>
  [...document.querySelectorAll<HTMLLIElement>('.glim-readme-btn-unit')].find(
    (li) => li.querySelector('.glim-readme-btn-name')?.textContent === name,
  );

beforeEach(() => {
  calls = [];
  opened = [];
  sites = [patreon, youtube];
  hosterRows = [];
  apps = [browserExtension];
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  vi.stubGlobal('IntersectionObserver', OffScreen);
  shown = [];
  vi.stubGlobal('open', newTab);
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? 'GET';
      calls.push({ url, method, body: init?.body ? JSON.parse(String(init.body)) : undefined });
      const site = sites.find((s) => url.includes(`/${s.id}/`)) ?? patreon;
      if (url === '/api/sitelogins') return reply(sites);
      if (url === '/api/connect') return reply({ active: true, apps });
      if (url === '/api/hosterauth/hosts') return reply([{ id: 'rapidgator.net', label: 'rapidgator.net' }]);
      if (url === '/api/hosterauth/logins') return reply(hosterRows);
      if (url.endsWith('/remove')) {
        sites = sites.map((s) => (s.id === site.id ? notAdded(s) : s));
        return reply(notAdded(site));
      }
      if (url.endsWith('/add') || url.endsWith('/begin')) {
        sites = sites.map((s) => (s.id === site.id ? { ...s, listed: true } : s));
        return reply({ ...site, listed: true });
      }
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

it('lists a signed-in site among the hoster accounts, saying who it signs in as and where from', async () => {
  await renderCard();
  expect(row('Patreon')?.textContent).toContain('Signed in as Jane Doe');
  expect(hint('Patreon')).toContain('from Firefox (default-release)');
  expect(row('Patreon')?.textContent).not.toContain('from Firefox');
  expect(row('Patreon')?.querySelector('.text-statusOk')).not.toBeNull();
});

it('says when the browser extension passed a session on', async () => {
  sites = [{ ...youtube, state: 'signedIn' }];
  await renderCard();
  expect(hint('YouTube')).toContain('from the browser extension');
});

it('marks the row of a site whose sign-in ran out in red and says how to sign in again', async () => {
  await renderCard();
  const expired = row('YouTube')!;
  expect(expired.textContent).toContain('Sign-in ran out');
  expect(hint('YouTube')).toContain('KnightLoader picks it up by itself.');
  expect(expired.querySelector('.text-statusFail')).not.toBeNull();
});

it('lists a site nobody signed in to with the one step that signs it in', async () => {
  sites = [signedOut(patreon)];
  await renderCard();
  expect(row('Patreon')?.textContent).toContain('Not signed in');
  expect(hint('Patreon')).toContain('Sign in to Patreon in your browser. KnightLoader picks it up by itself.');
  expect(row('Patreon')?.textContent).not.toContain('Sign in to Patreon');
});

it('points at the sign-in window on an instance that reads no browser, with or without an extension in the group', async () => {
  sites = [{ ...signedOut(patreon), readsBrowsers: false }];
  apps = [browserExtension];
  await renderCard();
  expect(hint('Patreon')).toContain('in the window here');
  expect(hint('Patreon')).not.toContain('extension');
  expect(hint('Patreon')).not.toContain('picks it up by itself');
});

it('names the browsers KnightLoader cannot read once, under the list', async () => {
  sites = [
    { ...patreon, unreadable: ['Chrome'] },
    { ...signedOut(youtube), unreadable: ['Chrome'] },
  ];
  await renderCard();
  expect((host.textContent ?? '').split('cannot read sign-ins from Chrome').length).toBe(2);
});

it('shows a row only for a site that was added', async () => {
  sites = [patreon, notAdded(youtube), instagram];
  await renderCard();
  expect(row('Patreon')).toBeDefined();
  expect(row('YouTube')).toBeUndefined();
  expect(row('Instagram')).toBeUndefined();
});

it('lists the sites and the hosters in one order, by name', async () => {
  sites = [{ ...youtube, state: 'signedIn' }, patreon];
  hosterRows = [{ host: 'rapidgator.net', enabled: true, status: 'active' }];
  await renderCard();
  const labels = [...host.querySelectorAll<HTMLTableRowElement>('tbody tr')].map((tr) =>
    ['Patreon', 'rapidgator.net', 'YouTube'].find((name) => tr.textContent?.includes(name)),
  );
  expect(labels).toEqual(['Patreon', 'rapidgator.net', 'YouTube']);
});

it('offers the sites not added in the picker, with their domain beneath, and opens the window of one picked', async () => {
  sites = [patreon, notAdded(youtube), instagram];
  await renderCard();
  await act(async () => button('Add')!.click());
  expect(tile('rapidgator.net')).toBeDefined();
  expect(tile('Patreon')).toBeUndefined();
  expect(tile('YouTube')?.querySelector('.glim-readme-btn-sub')?.textContent).toBe('youtube.com');
  expect(tile('Instagram')?.querySelector('.glim-readme-btn-sub')?.textContent).toBe('instagram.com');
  await act(async () => tile('Instagram')!.querySelector('button')!.click());
  expect(calls.some((c) => c.url === '/api/sitelogins/instagram/add' && c.method === 'POST')).toBe(true);
  expect(document.querySelector('[role="dialog"]')?.textContent).toContain('Sign in to Instagram');
  expect(row('Instagram')).toBeDefined();
});

it('opens the sign-in browser straight away for a site picked on an instance that reads no browser', async () => {
  sites = [patreon, notAdded(instagram)].map((s) => ({ ...s, readsBrowsers: false }));
  await renderCard();
  await act(async () => button('Add')!.click());
  await act(async () => tile('Instagram')!.querySelector('button')!.click());
  expect(calls.some((c) => c.url === '/api/sitelogins/instagram/add' && c.method === 'POST')).toBe(true);
  expect(calls.some((c) => c.url === '/api/signinbrowser/instagram/open')).toBe(true);
  expect(document.querySelector('[role="dialog"]')?.textContent).not.toContain('Status');
});

it('opens the site from its window and offers no way to paste cookies', async () => {
  sites = [signedOut(patreon)];
  await renderCard();
  await act(async () => row('Patreon')!.click());
  expect(document.querySelector('textarea')).toBeNull();
  expect(document.body.textContent).not.toContain('cookies.txt');
  await act(async () => button('Open Patreon')!.click());
  expect(opened).toEqual(['https://www.patreon.com/login']);
  expect(calls.some((c) => c.method === 'POST')).toBe(false);
});

it('tells the tab the site opened in that it can go once the sign-in arrives', async () => {
  vi.useFakeTimers();
  try {
    sites = [signedOut(patreon)];
    await renderCard();
    await act(async () => row('Patreon')!.click());
    await act(async () => button('Open Patreon')!.click());
    expect(opened).toEqual(['https://www.patreon.com/login']);
    sites = [patreon];
    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000);
    });
    expect(shown).toEqual([`${location.origin}/signedin?site=Patreon`]);
  } finally {
    vi.useRealTimers();
  }
});

it('offers no Open button in a container, also where the extension may read the site', async () => {
  sites = [{ ...signedOut(patreon), readsBrowsers: false, handsOver: true }];
  await renderCard();
  await act(async () => row('Patreon')!.click());
  expect(button('Open Patreon')).toBeUndefined();
});

it('forgets a session the extension passed on from its row, after asking', async () => {
  await renderCard();
  await act(async () => row('YouTube')!.querySelector<HTMLButtonElement>('[aria-label="Remove"]')!.click());
  expect(calls.some((c) => c.url.endsWith('/remove'))).toBe(false);
  await act(async () => button('Remove')!.click());
  expect(calls.some((c) => c.url === '/api/sitelogins/youtube/remove' && c.method === 'POST')).toBe(true);
});

it('removes a session read from the browser from its row too, after asking, and the row goes', async () => {
  await renderCard();
  await act(async () => row('Patreon')!.querySelector<HTMLButtonElement>('[aria-label="Remove"]')!.click());
  await act(async () => button('Remove')!.click());
  expect(calls.some((c) => c.url === '/api/sitelogins/patreon/remove' && c.method === 'POST')).toBe(true);
  expect(row('Patreon')).toBeUndefined();
  expect(document.body.textContent).toContain('Removed.');
});

it('removes a site nobody is signed in to from its row as well', async () => {
  sites = [signedOut(patreon)];
  await renderCard();
  const remove = row('Patreon')!.querySelector<HTMLButtonElement>('[aria-label="Remove"]');
  expect(remove).not.toBeNull();
  await act(async () => remove!.click());
  await act(async () => button('Remove')!.click());
  expect(calls.some((c) => c.url === '/api/sitelogins/patreon/remove' && c.method === 'POST')).toBe(true);
});

describe("a site's window", () => {
  const dialog = () => document.querySelector('[role="dialog"]')!;
  const foot = () => [...dialog().querySelectorAll<HTMLButtonElement>('.glim-modal-foot button')];
  const primary = () => foot().filter((b) => b.className.split(' ').includes('bg-accent'));

  it('ends its footer on the one primary action while nobody is signed in', async () => {
    sites = [signedOut(patreon)];
    await renderCard();
    await act(async () => row('Patreon')!.click());
    expect(primary().map((b) => b.textContent)).toEqual(['Open Patreon']);
    expect(foot()[foot().length - 1]?.textContent).toBe('Open Patreon');
  });

  it('offers nothing to press ahead once a sign-in is there, and leaves removing to the row', async () => {
    await renderCard();
    await act(async () => row('Patreon')!.click());
    const words = foot().map((b) => b.textContent);
    expect(words).not.toContain('Sign out');
    expect(words).not.toContain('Remove');
    expect(words).not.toContain('Open Patreon');
    expect(words).not.toContain('Sign in here');
    expect(primary()).toEqual([]);
  });

  it('offers a new sign-in alone for a session that ran out, which its row menu forgets', async () => {
    sites = [youtube];
    await renderCard();
    await act(async () => row('YouTube')!.click());
    expect(foot().map((b) => b.textContent)).not.toContain('Sign out');
    expect(primary().map((b) => b.textContent)).toEqual(['Open YouTube']);
  });

  it('says how the sign-in stands and what to do at the top, and keeps every button in the footer', async () => {
    sites = [signedOut(patreon)];
    await renderCard();
    await act(async () => row('Patreon')!.click());
    expect(dialog().textContent).toContain('Not signed in');
    expect(dialog().textContent).toContain('Sign in to Patreon in your browser.');
    expect([...dialog().querySelectorAll('button')].filter((b) => !b.closest('.glim-modal-foot'))).toEqual([]);
  });
});

it('names the browser whose session the site turned down', async () => {
  sites = [{ ...patreon, state: 'expired', account: undefined }];
  await renderCard();
  expect(hint('Patreon')).toContain('Patreon turned down the sign-in from Firefox (default-release)');
  expect(hint('Patreon')).toContain('KnightLoader picks it up by itself.');
});

it("says when KnightLoader's own sign-in browser passed a session on", async () => {
  sites = [{ ...youtube, state: 'signedIn', source: 'kl-browser' }];
  await renderCard();
  expect(hint('YouTube')).toContain("from KnightLoader's sign-in browser");
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

it('adds a site a failed download asks to sign in to before it opens the site', async () => {
  sites = [notAdded(youtube)];
  await renderAdvice({ ...failed, url: 'https://youtu.be/abc', reason: 'botCheck' });
  await act(async () => button('Sign in to YouTube')!.click());
  await act(async () => button('Open YouTube')!.click());
  expect(calls.some((c) => c.url === '/api/sitelogins/youtube/begin' && c.method === 'POST')).toBe(true);
  expect(opened).toEqual(['https://accounts.google.com/ServiceLogin']);
});

it('asks to sign in again when a members-only download fails on a site with a stored session', async () => {
  await renderAdvice(failed);
  expect(document.body.textContent).toContain('your Patreon sign-in did not get in');
  await act(async () => button('Sign in again')!.click());
  expect(button('Open Patreon')).toBeDefined();
});

it('starts the download again once the sign-in arrives', async () => {
  vi.useFakeTimers();
  try {
    sites = [signedOut(patreon)];
    await renderAdvice(failed);
    await act(async () => button('Sign in to Patreon')!.click());
    sites = [patreon];
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000);
    });
    expect(calls.some((c) => c.url === '/api/tasks/restart' && c.method === 'POST')).toBe(true);
    expect(document.body.textContent).toContain('Signed in to Patreon.');
  } finally {
    vi.useRealTimers();
  }
});

it('offers signing in for a bot check on a site KnightLoader signs in to', async () => {
  sites = [signedOut(youtube)];
  await renderAdvice({ ...failed, url: 'https://youtu.be/abc', reason: 'botCheck' });
  expect(button('Sign in to YouTube')).toBeDefined();
});

it('offers signing in for a video that wants a signed-in account', async () => {
  sites = [signedOut(youtube)];
  await renderAdvice({ ...failed, url: 'https://www.youtube.com/watch?v=abc', reason: 'signInRequired' });
  expect(document.body.textContent).toContain('only to a signed-in account');
  expect(button('Sign in to YouTube')).toBeDefined();
});

it('offers no cookies for a site KnightLoader cannot sign in to', async () => {
  await renderAdvice({ ...failed, url: 'https://www.dailymotion.com/video/1' });
  expect(document.body.textContent).not.toMatch(/cookie/i);
  expect(button('Sign in again')).toBeUndefined();
});

it('keeps the plain advice for a row of another instance, whose sign-ins are its own', async () => {
  await renderAdvice(failed, '/api/instances/attic');
  expect(calls.some((c) => c.url === '/api/sitelogins')).toBe(false);
  expect(button('Sign in again')).toBeUndefined();
});
