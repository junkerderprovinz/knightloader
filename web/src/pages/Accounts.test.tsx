// @vitest-environment jsdom
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
let patched: unknown[];

const reply = (body: unknown) =>
  new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } });

/** Nothing listens, so the settings broadcast never comes. */
class QuietSocket {
  readyState = 0;
  send() {}
  close() {}
}

/** Rows are never scrolled into view, so no hoster icon is asked for. */
class OffScreen {
  observe() {}
  disconnect() {}
}

/** The last button reading `text`, since a window's button comes after the page's. */
const button = (text: string) =>
  [...document.querySelectorAll<HTMLButtonElement>('button')].reverse().find((b) => b.textContent === text)!;

/** addKind presses the floating action and picks a kind from its list. */
async function addKind(kind: string) {
  await act(async () => button('Add an account').click());
  await act(async () => button(kind).click());
}

beforeEach(() => {
  patched = [];
  // A desktop width, where the floating action shows its words.
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  vi.stubGlobal('WebSocket', QuietSocket);
  vi.stubGlobal('IntersectionObserver', OffScreen);
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url === '/api/settings' && init?.method === 'PATCH') {
        const body = JSON.parse(String(init.body));
        patched.push(body);
        return reply(body);
      }
      if (url === '/api/settings') return reply({ premiumOnly: false });
      // The accounts, the catalogue, the hoster logins and the routing cards
      // all read as empty.
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

it('lists a stored captcha account in its own section, apart from the debrid accounts', async () => {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string) => {
      if (url === '/api/accounts/catalogue') {
        return reply([
          { id: 'torbox', label: 'TorBox', kind: 'apiKey', group: 'debrid', whereUrl: 'https://torbox.app/settings' },
          {
            id: 'capsolver',
            label: 'CapSolver',
            kind: 'apiKey',
            group: 'captchaSolver',
            whereUrl: 'https://dashboard.capsolver.com/',
          },
        ]);
      }
      if (url === '/api/accounts') {
        return reply([
          { id: 'capsolver', service: 'capsolver', account: '', label: '', enabled: true, configured: true, fromEnv: false, ok: false, detail: '', hosts: 0, tier: 'unknown' },
        ]);
      }
      if (url === '/api/settings') return reply({ premiumOnly: false });
      return reply([]);
    }),
  );
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
  const solvers = host.querySelector('[aria-label="Captcha accounts"]');
  expect(solvers?.textContent).toContain('CapSolver');
  const debrid = host.querySelector('[aria-label="Debrid accounts"]');
  expect(debrid).toBeNull();
  expect(host.textContent).toContain('No debrid accounts yet');
});

it('offers Allow free downloads on the Accounts page itself, not only on its settings tab', async () => {
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
  const toggle = host.querySelector<HTMLButtonElement>('[role="switch"][aria-label="Allow free downloads"]');
  expect(toggle).not.toBeNull();
  expect(toggle!.getAttribute('aria-checked')).toBe('true');

  // Switching free downloads off is premium only on, the setting as stored.
  await act(async () => toggle!.click());
  expect(patched).toEqual([{ premiumOnly: true }]);
  expect(toggle!.getAttribute('aria-checked')).toBe('false');
});

it('lists a multihoster login kept for JDownloader with the hoster accounts, not the debrid ones', async () => {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string) => {
      if (url === '/api/accounts/catalogue') {
        return reply([
          { id: 'mydebrid', label: 'MyDebrid', kind: 'usernamePassword', group: 'debrid', whereUrl: 'https://mydebrid.com/login' },
        ]);
      }
      if (url === '/api/hosterauth/logins') {
        // An older server still sends the multihoster mark, which must not
        // move the row.
        return reply([{ host: 'leechall.io', username: 'knight', status: 'active', enabled: true, multihoster: true }]);
      }
      if (url === '/api/settings') return reply({ premiumOnly: false });
      return reply([]);
    }),
  );
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
  expect(host.querySelector('[aria-label="Hoster accounts"]')?.textContent).toContain('leechall.io');
  expect(host.querySelector('[aria-label="Debrid accounts"]')).toBeNull();
  expect(host.textContent).not.toContain('through JDownloader');
});

it('adds the login for an own server under its hostname, cut from a pasted link', async () => {
  const posted: unknown[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url === '/api/accounts/catalogue') {
        return reply([
          {
            id: 'remotefs',
            label: 'Own server (FTP, SFTP, WebDAV)',
            kind: 'usernamePassword',
            group: 'remoteServer',
            whereUrl: 'https://junkerderprovinz.github.io/knightloader/getting-links-in/',
          },
        ]);
      }
      if (url === '/api/accounts' && init?.method === 'POST') {
        posted.push(JSON.parse(String(init.body)));
        return new Response(null, { status: 204 });
      }
      if (url === '/api/accounts') {
        return reply([
          { id: 'remotefs:nas.lan', service: 'remotefs', account: 'nas.lan', label: 'nas.lan', enabled: true, configured: true, fromEnv: false, ok: false, detail: '', hosts: 0, tier: 'unknown' },
        ]);
      }
      if (url === '/api/settings') return reply({ premiumOnly: false });
      return reply([]);
    }),
  );
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
  expect(host.querySelector('[aria-label="Own servers"]')?.textContent).toContain('nas.lan');

  const field = (caption: string) =>
    [...document.querySelectorAll('label')].find((l) => l.textContent?.startsWith(caption))!.querySelector('input')!;
  const type = (input: HTMLInputElement, text: string) => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, text);
    act(() => input.dispatchEvent(new Event('input', { bubbles: true })));
  };

  await addKind('Own server');
  type(field('Hostname'), 'ftp://Seedbox.Example.net:2121/files/');
  type(field('Username'), 'knight');
  type(field('Password'), 'hunter2');
  await act(async () => button('Save').click());

  expect(posted).toEqual([{ service: 'remotefs', account: 'seedbox.example.net', username: 'knight', password: 'hunter2' }]);
});

it('saves an own server only under a name a host can have', async () => {
  const posted: unknown[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url === '/api/accounts/catalogue') {
        return reply([
          {
            id: 'remotefs',
            label: 'Own server (FTP, SFTP, WebDAV)',
            kind: 'usernamePassword',
            group: 'remoteServer',
            whereUrl: 'https://junkerderprovinz.github.io/knightloader/getting-links-in/',
          },
        ]);
      }
      if (url === '/api/accounts' && init?.method === 'POST') {
        posted.push(JSON.parse(String(init.body)));
        return new Response(null, { status: 204 });
      }
      if (url === '/api/settings') return reply({ premiumOnly: false });
      return reply([]);
    }),
  );
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
  const field = (caption: string) =>
    [...document.querySelectorAll('label')].find((l) => l.textContent?.startsWith(caption))!.querySelector('input')!;
  const type = (input: HTMLInputElement, text: string) => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, text);
    act(() => input.dispatchEvent(new Event('input', { bubbles: true })));
  };

  await addKind('Own server');
  type(field('Username'), 'knight');
  type(field('Password'), 'hunter2');
  for (const name of ['nas!box', 'nas"box', 'nas{box}', 'two words']) {
    type(field('Hostname'), name);
    expect(button('Save').disabled, name).toBe(true);
  }
  type(field('Hostname'), 'sftp://[FE80::1]:22/');
  await act(async () => button('Save').click());

  expect(posted).toEqual([{ service: 'remotefs', account: 'fe80::1', username: 'knight', password: 'hunter2' }]);
});

it('lists the own Usenet servers in their own section and saves a switch without the stored password', async () => {
  const posted: unknown[] = [];
  const server = {
    id: 'news.example.com',
    host: 'news.example.com',
    port: 563,
    tls: true,
    connections: 8,
    level: 1,
    retentionDays: 0,
    optional: false,
    enabled: true,
    username: 'reader',
    hasPassword: true,
  };
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url === '/api/usenet/servers' && init?.method === 'POST') {
        posted.push(JSON.parse(String(init.body)));
        return reply([{ ...server, enabled: false }]);
      }
      if (url === '/api/usenet/servers') return reply([server]);
      if (url === '/api/settings') return reply({ premiumOnly: false });
      return reply([]);
    }),
  );
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
  const table = host.querySelector('[aria-label="Usenet servers"]');
  expect(table?.textContent).toContain('news.example.com · Level 1');
  const toggle = table!.querySelector<HTMLButtonElement>('[role="switch"]');
  await act(async () => toggle!.click());
  expect(posted).toEqual([
    expect.objectContaining({ id: 'news.example.com', enabled: false, username: 'reader', password: '********' }),
  ]);
  expect(posted[0]).not.toHaveProperty('hasPassword');
});

const page = (
  <I18nProvider>
    <ToastProvider>
      <MemoryRouter>
        <Accounts />
      </MemoryRouter>
    </ToastProvider>
  </I18nProvider>
);

const services = [
  { id: 'torbox', label: 'TorBox', kind: 'apiKey', group: 'debrid', whereUrl: 'https://torbox.app/settings' },
  { id: 'remotefs', label: 'Own server (FTP, SFTP, WebDAV)', kind: 'usernamePassword', group: 'remoteServer', whereUrl: '' },
  { id: 'capsolver', label: 'CapSolver', kind: 'apiKey', group: 'captchaSolver', whereUrl: 'https://dashboard.capsolver.com/' },
];

/** The catalogue answers with `catalogue`, everything else as empty. */
function stubCatalogue(catalogue: unknown[]) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string) => {
      if (url === '/api/accounts/catalogue') return reply(catalogue);
      if (url === '/api/hosterauth/hosts') return reply([{ id: 'rapidgator.net', label: 'rapidgator.net' }]);
      if (url === '/api/settings') return reply({ premiumOnly: false });
      return reply([]);
    }),
  );
}

const windowTitle = () => document.querySelector('[role="dialog"] h2')?.textContent;

it('adds every kind of account from one floating action, listed in the order of the cards', async () => {
  stubCatalogue(services);
  await act(async () => root.render(page));

  const adders = [...document.querySelectorAll('button')].filter((b) => /^Add /.test(b.textContent ?? ''));
  expect(adders.map((b) => b.textContent)).toEqual(['Add an account']);
  expect(adders[0].closest('.glim-card')).toBeNull();

  await act(async () => adders[0].click());
  const kinds = [...document.querySelectorAll('[role="menu"] [role="menuitem"]')].map((i) => i.textContent);
  expect(kinds).toEqual(['Debrid account', 'Usenet server', 'Hoster account', 'Own server', 'Captcha account']);
});

it('offers no own server where the instance has no such service', async () => {
  stubCatalogue(services.filter((s) => s.group !== 'remoteServer'));
  await act(async () => root.render(page));
  await act(async () => button('Add an account').click());
  const kinds = [...document.querySelectorAll('[role="menu"] [role="menuitem"]')].map((i) => i.textContent);
  expect(kinds).toEqual(['Debrid account', 'Usenet server', 'Hoster account', 'Captcha account']);
});

it('opens the list of services for the picked kind, and a new account leads back to it', async () => {
  stubCatalogue(services);
  await act(async () => root.render(page));

  await addKind('Debrid account');
  expect(windowTitle()).toContain('Choose a debrid account');
  const service = [...document.querySelectorAll<HTMLButtonElement>('[role="dialog"] button')].find((b) =>
    b.textContent?.endsWith('TorBox'),
  )!;
  await act(async () => service.click());
  expect(windowTitle()).toContain('Add your TorBox account');

  // The form's row is Back and Save, as in the window it was picked in.
  const dialog = document.querySelector('[role="dialog"]')!;
  const row = [...dialog.querySelectorAll('button')].map((b) => b.textContent).filter(Boolean);
  expect(row.slice(-2)).toEqual(['Back', 'Save']);
  await act(async () => button('Back').click());
  expect(windowTitle()).toContain('Choose a debrid account');

  await act(async () => button('Cancel').click());
  await addKind('Captcha account');
  expect(windowTitle()).toContain('Choose a captcha account');
});

it('opens the window of the cards that keep their own, each time it is asked for', async () => {
  stubCatalogue(services);
  await act(async () => root.render(page));

  await addKind('Usenet server');
  expect(windowTitle()).toContain('New Usenet server');
  await act(async () => button('Cancel').click());
  expect(document.querySelector('[role="dialog"]')).toBeNull();
  await addKind('Usenet server');
  expect(windowTitle()).toContain('New Usenet server');
  await act(async () => button('Cancel').click());

  await addKind('Hoster account');
  expect(windowTitle()).toContain('Choose a hoster account');
  await act(async () => button('Cancel').click());
  expect(document.querySelector('[role="dialog"]')).toBeNull();
  await addKind('Hoster account');
  expect(windowTitle()).toContain('Choose a hoster account');
});

it('sends the reader of an empty card to the floating action by its name', async () => {
  stubCatalogue(services);
  await act(async () => root.render(page));
  const pointer = 'To add one, use “Add an account” at the bottom of the page.';
  const empty = [...host.querySelectorAll('.glim-well')].filter((w) => w.textContent?.includes('yet'));
  expect(empty).toHaveLength(5);
  for (const card of empty) expect(card.textContent).toContain(pointer);
});
