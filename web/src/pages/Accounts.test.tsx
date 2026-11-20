// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

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

/** The card headed `title`. */
const card = (title: string) =>
  [...host.querySelectorAll('.glim-card')].find((c) => c.querySelector('h2')?.textContent?.startsWith(title))!;

/** The Add button at the end of a card's heading. */
const adder = (title: string) =>
  [...card(title).querySelectorAll<HTMLButtonElement>('button')].find((b) => /^Add /.test(b.textContent ?? ''));

/** The gear at the end of a card's heading, beside its Add button. */
const gear = (title: string) => card(title).querySelector<HTMLButtonElement>('button[aria-label="Card actions"]');

/** The entry of the open menu reading `text`. */
const entry = (text: string) =>
  [...document.querySelectorAll<HTMLButtonElement>('[role="menu"] button')].find((b) => b.textContent === text)!;

/** openSwitches opens the list of a card's accounts from the gear on its heading. */
async function openSwitches(title: string) {
  await act(async () => gear(title)!.click());
  await act(async () => entry('Enable or disable accounts').click());
}

/** The switch of one account in the open list. */
const accountSwitch = (name: string) =>
  document.querySelector<HTMLButtonElement>(`[role="dialog"] [role="switch"][aria-label="Enable ${name}"]`)!;

/** addIn presses the Add button of the card headed `title`. */
async function addIn(title: string) {
  await act(async () => adder(title)!.click());
}

// jsdom has no SMIL clock for the priority pictures to hold.
Object.assign(SVGSVGElement.prototype, { pauseAnimations() {}, unpauseAnimations() {}, setCurrentTime() {} });

beforeEach(() => {
  patched = [];
  // A desktop width, with a pointer.
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
      if (url === '/api/features') return reply({ modules: [], pages: [] });
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
      if (url === '/api/features') return reply({ modules: [], pages: [] });
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

  // It stands above both priority cards.
  const titled = (text: string) => [...host.querySelectorAll('h2, h3')].find((h) => h.textContent?.trim() === text);
  for (const order of ['Download priority order', 'Captcha priority order']) {
    const heading = titled(order);
    expect(heading, order).toBeDefined();
    expect(toggle!.compareDocumentPosition(heading!) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  }

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
      if (url === '/api/features') return reply({ modules: [], pages: [] });
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

it('adds a seedbox login under its hostname, cut from a pasted link', async () => {
  const posted: unknown[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url === '/api/accounts/catalogue') {
        return reply([
          {
            id: 'remotefs',
            label: 'Seedbox (FTP, SFTP, WebDAV)',
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
      if (url === '/api/features') return reply({ modules: [], pages: [] });
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
  expect(host.querySelector('[aria-label="Seedboxes"]')?.textContent).toContain('nas.lan');

  const field = (caption: string) =>
    [...document.querySelectorAll('label')].find((l) => l.textContent?.startsWith(caption))!.querySelector('input')!;
  const type = (input: HTMLInputElement, text: string) => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, text);
    act(() => input.dispatchEvent(new Event('input', { bubbles: true })));
  };

  await addIn('Seedboxes');
  type(field('Hostname'), 'ftp://Seedbox.Example.net:2121/files/');
  type(field('Username'), 'knight');
  type(field('Password'), 'hunter2');
  await act(async () => button('Save').click());

  expect(posted).toEqual([{ service: 'remotefs', account: 'seedbox.example.net', username: 'knight', password: 'hunter2' }]);
});

it('saves a seedbox only under a name a host can have', async () => {
  const posted: unknown[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url === '/api/accounts/catalogue') {
        return reply([
          {
            id: 'remotefs',
            label: 'Seedbox (FTP, SFTP, WebDAV)',
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
      if (url === '/api/features') return reply({ modules: [], pages: [] });
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

  await addIn('Seedboxes');
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
      if (url === '/api/features') return reply({ modules: [], pages: [] });
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
  await openSwitches('Usenet servers');
  await act(async () => accountSwitch('news.example.com · Level 1').click());
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
  { id: 'remotefs', label: 'Seedbox (FTP, SFTP, WebDAV)', kind: 'usernamePassword', group: 'remoteServer', whereUrl: '' },
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
      if (url === '/api/features') return reply({ modules: [], pages: [] });
      return reply([]);
    }),
  );
}

const windowTitle = () => document.querySelector('[role="dialog"] h2')?.textContent;

it('gives every card its own Add button at the end of its heading', async () => {
  stubCatalogue(services);
  await act(async () => root.render(page));

  const titles = ['Debrid accounts', 'Hoster accounts', 'Usenet servers', 'Seedboxes', 'Captcha accounts'];
  expect(titles.map((t) => adder(t)?.textContent)).toEqual([
    'Add an account',
    'Add an account',
    'Add a server',
    'Add a seedbox',
    'Add an account',
  ]);
  const loose = [...document.querySelectorAll('button')].filter(
    (b) => /^Add /.test(b.textContent ?? '') && !b.closest('.glim-card'),
  );
  expect(loose).toEqual([]);
});

it('puts the hoster accounts right under the debrid ones, each card at the next palette place', async () => {
  stubCatalogue(services);
  await act(async () => root.render(page));

  const cards = [...host.querySelectorAll<HTMLElement>('.glim-card')].slice(0, 5);
  expect(cards.map((c) => c.querySelector('h2')?.textContent)).toEqual([
    expect.stringMatching(/^Debrid accounts/),
    expect.stringMatching(/^Hoster accounts/),
    expect.stringMatching(/^Usenet servers/),
    expect.stringMatching(/^Seedboxes/),
    expect.stringMatching(/^Captcha accounts/),
  ]);
  expect(cards.map((c) => c.style.getPropertyValue('--item-hue'))).toEqual([0, 1, 2, 3, 4].map((n) => `var(--rb-${n})`));
});

it('offers no seedbox card where the instance has no such service', async () => {
  stubCatalogue(services.filter((s) => s.group !== 'remoteServer'));
  await act(async () => root.render(page));
  expect(card('Seedboxes')).toBeUndefined();
});

it('opens the picker of its own kind from a card, in one window', async () => {
  stubCatalogue(services);
  await act(async () => root.render(page));
  await addIn('Captcha accounts');
  expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(1);
  expect(document.querySelector('[data-new="account-picker"]')?.textContent).toContain('CapSolver');
  expect(document.querySelector('[data-new="account-picker"]')?.textContent).not.toContain('TorBox');
});

it('opens the list of services from the debrid card, and a new account leads back to it', async () => {
  stubCatalogue(services);
  await act(async () => root.render(page));

  await addIn('Debrid accounts');
  expect(windowTitle()).toContain('Choose a debrid account');
  // A tile is the brand's name over the site it lives at.
  const service = [...document.querySelectorAll<HTMLButtonElement>('[role="dialog"] button')].find(
    (b) => b.textContent === 'TorBoxtorbox.app',
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
  await addIn('Captcha accounts');
  expect(windowTitle()).toContain('Choose a captcha account');
});

it('opens the window of the cards that keep their own, each time it is asked for', async () => {
  stubCatalogue(services);
  await act(async () => root.render(page));

  await addIn('Usenet servers');
  expect(windowTitle()).toContain('Choose a Usenet provider');
  await act(async () => button('Cancel').click());
  expect(document.querySelector('[role="dialog"]')).toBeNull();
  await addIn('Usenet servers');
  expect(windowTitle()).toContain('Choose a Usenet provider');
  await act(async () => button('Cancel').click());

  await addIn('Hoster accounts');
  expect(windowTitle()).toContain('Choose a hoster account');
  await act(async () => button('Cancel').click());
  expect(document.querySelector('[role="dialog"]')).toBeNull();
  await addIn('Hoster accounts');
  expect(windowTitle()).toContain('Choose a hoster account');
});

describe('a new Usenet server', () => {
  const field = (caption: string) =>
    [...document.querySelectorAll('[role="dialog"] label')].find((l) => l.textContent?.startsWith(caption))!.querySelector('input')!;
  const type = (input: HTMLInputElement, text: string) => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, text);
    act(() => input.dispatchEvent(new Event('input', { bubbles: true })));
  };
  const tile = (name: string) =>
    [...document.querySelectorAll<HTMLButtonElement>('[data-new="account-picker"] button')].find((b) =>
      b.textContent?.startsWith(name),
    )!;
  const tls = () => document.querySelector('[role="dialog"] [role="switch"][aria-label="Encrypted connection (TLS)"]')!;

  it('starts from the tiles of well-known providers and fills in the server of the one picked', async () => {
    const posted: Record<string, unknown>[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string, init?: RequestInit) => {
        if (url === '/api/usenet/servers' && init?.method === 'POST') {
          posted.push(JSON.parse(String(init.body)));
          return reply([]);
        }
        if (url === '/api/settings') return reply({ premiumOnly: false });
        if (url === '/api/features') return reply({ modules: [], pages: [] });
        return reply([]);
      }),
    );
    await act(async () => root.render(page));
    await addIn('Usenet servers');
    expect(tile('Eweka').textContent).toBe('Ewekaeweka.nl');

    await act(async () => tile('Eweka').click());
    expect(windowTitle()).toContain('Add your Eweka account');
    expect(field('Server').value).toBe('news.eweka.nl');
    expect(field('Port').value).toBe('563');
    expect(field('Connections').value).toBe('50');
    expect(tls().getAttribute('aria-checked')).toBe('true');

    type(field('Username'), 'reader');
    type(field('Password'), 'hunter2');
    await act(async () => button('Save').click());
    expect(posted).toEqual([
      expect.objectContaining({ id: '', host: 'news.eweka.nl', port: 563, tls: true, connections: 50, username: 'reader', password: 'hunter2' }),
    ]);
  });

  it('keeps an empty form for any other server', async () => {
    stubCatalogue(services);
    await act(async () => root.render(page));
    await addIn('Usenet servers');
    await act(async () => tile('Other server').click());
    expect(windowTitle()).toContain('New Usenet server');
    expect(field('Server').value).toBe('');
    expect(field('Port').value).toBe('563');
  });

  it('leads back from a provider\'s form to the tiles', async () => {
    stubCatalogue(services);
    await act(async () => root.render(page));
    await addIn('Usenet servers');
    await act(async () => tile('Newshosting').click());
    await act(async () => button('Back').click());
    expect(windowTitle()).toContain('Choose a Usenet provider');
    expect(tile('Newshosting')).toBeDefined();
  });
});

it('offers an account’s options as buttons on its row, Remove last', async () => {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string) => {
      if (url === '/api/accounts/catalogue') return reply(services);
      if (url === '/api/accounts') {
        return reply([
          { id: 'torbox', service: 'torbox', account: '', label: '', enabled: true, configured: true, fromEnv: false, ok: true, detail: 'ok', hosts: 0, tier: 'premium' },
        ]);
      }
      if (url === '/api/settings') return reply({ premiumOnly: false });
      if (url === '/api/features') return reply({ modules: [], pages: [] });
      return reply([]);
    }),
  );
  await act(async () => root.render(page));
  const row = host.querySelector('[aria-label="Debrid accounts"] [data-glim="ConnectionRow"][aria-label="TorBox"]')!;
  const words = [...row.querySelectorAll('.glim-connection-actions button')].map((b) => b.textContent);
  expect(words).toEqual(['Disable', 'Refresh', 'Edit', 'Remove']);

  await act(async () => row.querySelector<HTMLButtonElement>('[data-connection="edit"]')!.click());
  expect(windowTitle()).toContain('TorBox');
});

describe('every connected service as a connection row', () => {
  const later = '2027-06-08T12:00:00Z';
  const account = (extra: Record<string, unknown>) => ({
    id: 'realdebrid',
    service: 'realdebrid',
    account: '',
    label: '',
    enabled: true,
    configured: true,
    fromEnv: false,
    ok: true,
    detail: 'ok',
    hosts: 0,
    tier: 'premium',
    traffic: { used: 30e9, limit: 100e9, unlimited: false },
    ...extra,
  });
  const catalogue = [
    { id: 'realdebrid', label: 'Real-Debrid', kind: 'apiKey', group: 'debrid', whereUrl: 'https://real-debrid.com/apitoken' },
    { id: 'premiumize', label: 'Premiumize.me', kind: 'apiKey', group: 'debrid', whereUrl: '' },
    { id: 'remotefs', label: 'Seedbox (FTP, SFTP, WebDAV)', kind: 'usernamePassword', group: 'remoteServer', whereUrl: '' },
  ];
  let answers: Record<string, unknown>;

  beforeEach(() => {
    answers = { '/api/accounts/catalogue': catalogue };
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string) => {
        if (url in answers) return reply(answers[url]);
        if (url === '/api/settings') return reply({ premiumOnly: false });
        if (url === '/api/features') return reply({ modules: [], pages: [] });
        return reply([]);
      }),
    );
  });

  const row = (name: string) => host.querySelector<HTMLElement>(`[data-glim="ConnectionRow"][aria-label="${name}"]`)!;
  const status = (name: string) => row(name).querySelector('.glim-connection-status')?.textContent;
  const buttons = (name: string) => [...row(name).querySelectorAll('.glim-connection-actions button')].map((b) => b.textContent);

  it('says a debrid account’s plan, how long it runs and what is left of its traffic, and offers to renew it', async () => {
    answers['/api/accounts'] = [account({ expiry: later })];
    await act(async () => root.render(page));
    expect(status('Real-Debrid')).toMatch(/^Premium · Runs until .*2027/);
    expect(row('Real-Debrid').textContent).toContain('70 GB of 100 GB left');
    expect(row('Real-Debrid').querySelector('.glim-connection-badges')).toBeNull();
    expect(buttons('Real-Debrid')).toEqual(['Renew', 'Disable', 'Refresh', 'Edit', 'Remove']);
  });

  it('puts a failed check in a badge after the name and its reason in the status line', async () => {
    answers['/api/accounts'] = [account({ ok: false, detail: 'The key was refused (401)' })];
    await act(async () => root.render(page));
    expect(row('Real-Debrid').querySelector('.glim-connection-badges')?.textContent).toBe('Not working');
    expect(status('Real-Debrid')).toBe('The key was refused (401)');
  });

  it('turns the Refresh button red with the reason when the account still fails', async () => {
    answers['/api/accounts'] = [account({})];
    answers['/api/accounts/realdebrid/test'] = account({ ok: false, detail: 'The key was refused (401)' });
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string) => {
        if (url.startsWith('/api/accounts/test')) return reply(answers['/api/accounts/realdebrid/test']);
        if (url in answers) return reply(answers[url]);
        if (url === '/api/settings') return reply({ premiumOnly: false });
        if (url === '/api/features') return reply({ modules: [], pages: [] });
        return reply([]);
      }),
    );
    await act(async () => root.render(page));
    await act(async () => row('Real-Debrid').querySelector<HTMLButtonElement>('[data-connection="test"]')!.click());
    const test = row('Real-Debrid').querySelector<HTMLButtonElement>('[data-connection="test"]')!;
    expect(test.className).toContain('bg-statusFailSolid');
    expect(row('Real-Debrid').querySelector('[role="status"]')?.textContent).toBe('The key was refused (401)');
  });

  it('keeps an account from the environment and says where it is set', async () => {
    answers['/api/accounts'] = [account({ id: 'premiumize', service: 'premiumize', fromEnv: true, envVar: 'KL_PREMIUMIZE' })];
    await act(async () => root.render(page));
    expect(buttons('Premiumize.me')).toEqual(['Disable', 'Refresh', 'Edit']);
    expect(row('Premiumize.me').textContent).toContain('KL_PREMIUMIZE');
  });

  it('gives a seedbox no test, since only a download tries its login', async () => {
    answers['/api/accounts'] = [account({ id: 'remotefs:nas.lan', service: 'remotefs', account: 'nas.lan', detail: '', tier: 'unknown' })];
    await act(async () => root.render(page));
    expect(buttons('nas.lan')).toEqual(['Disable', 'Edit', 'Remove']);
    expect(status('nas.lan')).toContain('only when it downloads from it');
  });

  it('says a hoster login’s plan and traffic and offers no test, which JD has none of', async () => {
    answers['/api/hosterauth/logins'] = [
      { host: 'rapidgator.net', username: 'maria', status: 'active', enabled: true, tier: 'free', expiry: later, trafficLeft: 3e9, trafficMax: 10e9 },
    ];
    await act(async () => root.render(page));
    expect(status('rapidgator.net')).toMatch(/^Free · Runs until /);
    expect(row('rapidgator.net').textContent).toContain('3.0 GB of 10 GB left');
    expect(buttons('rapidgator.net')).toEqual(['Disable', 'Edit', 'Remove']);
  });

  it('marks a disabled hoster login and offers to enable it again', async () => {
    const posted: unknown[] = [];
    answers['/api/hosterauth/logins'] = [{ host: 'nitroflare.com', username: 'maria', status: 'off', enabled: false }];
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string, init?: RequestInit) => {
        if (url === '/api/hosterauth/logins/enabled') posted.push(JSON.parse(String(init?.body)));
        if (url in answers) return reply(answers[url]);
        if (url === '/api/settings') return reply({ premiumOnly: false });
        if (url === '/api/features') return reply({ modules: [], pages: [] });
        return reply([]);
      }),
    );
    await act(async () => root.render(page));
    expect(row('nitroflare.com').querySelector('.glim-connection-badges')?.textContent).toBe('Disabled');
    await act(async () => row('nitroflare.com').querySelector<HTMLButtonElement>('[data-connection="switch"]')!.click());
    expect(posted).toEqual([{ host: 'nitroflare.com', enabled: true }]);
  });

  it('tests a Usenet server from its row and says what the server answered', async () => {
    const server = { id: 'news.example.com', host: 'news.example.com', port: 563, tls: true, connections: 8, level: 0, retentionDays: 0, optional: false, enabled: true, username: 'reader', hasPassword: true };
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string) => {
        if (url === '/api/usenet/servers/test') return reply({ ok: false, detail: '481 authentication failed' });
        if (url === '/api/usenet/servers') return reply([server]);
        if (url === '/api/settings') return reply({ premiumOnly: false });
        if (url === '/api/features') return reply({ modules: [], pages: [] });
        return reply([]);
      }),
    );
    await act(async () => root.render(page));
    expect(status('news.example.com')).toBe('Not checked');
    await act(async () => row('news.example.com').querySelector<HTMLButtonElement>('[data-connection="test"]')!.click());
    expect(status('news.example.com')).toBe('481 authentication failed');
    expect(row('news.example.com').querySelector('.glim-connection-badges')?.textContent).toBe('Not working');
  });
});

describe('enabling and disabling accounts', () => {
  const debrid = (id: string, label: string) => ({ id, label, kind: 'apiKey', group: 'debrid', whereUrl: '' });
  const key = (service: string, enabled: boolean) => ({
    id: service,
    service,
    account: '',
    label: '',
    enabled,
    configured: true,
    fromEnv: false,
    ok: true,
    detail: 'ok',
    hosts: 0,
    tier: 'premium',
  });
  let switched: unknown[];

  beforeEach(() => {
    switched = [];
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string, init?: RequestInit) => {
        if (url === '/api/accounts/enabled') {
          switched.push(JSON.parse(String(init?.body)));
          return reply({});
        }
        if (url === '/api/accounts/catalogue') return reply([debrid('torbox', 'TorBox'), debrid('realdebrid', 'Real-Debrid')]);
        if (url === '/api/accounts') return reply([key('torbox', true), key('realdebrid', false)]);
        if (url === '/api/settings') return reply({ premiumOnly: false });
        if (url === '/api/features') return reply({ modules: [], pages: [] });
        return reply([]);
      }),
    );
  });

  const row = (name: string) => host.querySelector(`[aria-label="Debrid accounts"] [data-glim="ConnectionRow"][aria-label="${name}"]`)!;

  it('leaves the switch out of the rows and says in its row that an account is disabled', async () => {
    await act(async () => root.render(page));
    expect(card('Debrid accounts').querySelector('[role="switch"]')).toBeNull();
    expect(row('Real-Debrid').textContent).toContain('Disabled');
    expect(row('TorBox').textContent).not.toContain('Disabled');
  });

  it('switches one account at a time in the list behind the gear on its card', async () => {
    await act(async () => root.render(page));
    await openSwitches('Debrid accounts');
    expect(accountSwitch('TorBox').getAttribute('aria-checked')).toBe('true');
    expect(accountSwitch('Real-Debrid').getAttribute('aria-checked')).toBe('false');
    await act(async () => accountSwitch('Real-Debrid').click());
    expect(switched).toEqual([{ service: 'realdebrid', account: '', enabled: true }]);
    expect(accountSwitch('Real-Debrid').getAttribute('aria-checked')).toBe('true');
  });

  it('enables every account of a card at once from its gear, and leaves the ones already on alone', async () => {
    await act(async () => root.render(page));
    await act(async () => gear('Debrid accounts')!.click());
    expect(entry('Disable all').disabled).toBe(false);
    await act(async () => entry('Enable all').click());
    expect(switched).toEqual([{ service: 'realdebrid', account: '', enabled: true }]);
    expect(row('Real-Debrid').textContent).not.toContain('Disabled');
  });

  it('offers to enable all only while one is disabled', async () => {
    await act(async () => root.render(page));
    await act(async () => gear('Debrid accounts')!.click());
    await act(async () => entry('Disable all').click());
    await act(async () => gear('Debrid accounts')!.click());
    expect(entry('Disable all').disabled).toBe(true);
    expect(entry('Enable all').disabled).toBe(false);
  });

  it('disables one account from its own row', async () => {
    await act(async () => root.render(page));
    await act(async () => row('TorBox').querySelector<HTMLButtonElement>('[data-connection="switch"]')!.click());
    expect(switched).toEqual([{ service: 'torbox', account: '', enabled: false }]);
    expect(row('TorBox').textContent).toContain('Disabled');
  });
});

it('keeps every connected debrid account in the download order, and says why one does not work', async () => {
  const debrid = (id: string, label: string) => ({ id, label, kind: 'apiKey', group: 'debrid', whereUrl: '' });
  const key = (service: string, ok: boolean, detail: string) => ({
    id: service,
    service,
    account: '',
    label: '',
    enabled: true,
    configured: true,
    fromEnv: false,
    ok,
    detail,
  });
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string) => {
      if (url === '/api/accounts/catalogue') return reply([debrid('torbox', 'TorBox'), debrid('realdebrid', 'Real-Debrid')]);
      if (url === '/api/accounts') return reply([key('torbox', true, 'credential accepted'), key('realdebrid', false, 'bad token')]);
      // The instance has wired TorBox, not yet Real-Debrid.
      if (url === '/api/resolvers/priority') return reply([{ id: 'torbox', prio: 50 }, { id: 'direct', prio: 40 }]);
      if (url === '/api/settings') return reply({ premiumOnly: false });
      if (url === '/api/features') return reply({ modules: [], pages: [] });
      return reply([]);
    }),
  );
  await act(async () => root.render(page));
  const list = host.querySelector('ol[aria-label="Download priority order"]')!;
  const rows = [...list.querySelectorAll('li')];
  expect(rows.map((r) => r.getAttribute('data-ladder-id'))).toEqual(['torbox', 'direct', 'realdebrid']);
  expect(rows.map((r) => r.textContent)).toEqual(['TorBox', 'Direct download', 'Real-Debrid']);
  expect(rows[2].querySelector('[aria-label="The last check of this account failed: bad token"]')).not.toBeNull();
  expect(rows[0].querySelector('[aria-label^="The last check"]')).toBeNull();
});

it.each([
  [
    'ownActivate',
    'The hoster says the email address of this account is not activated yet. Activate it with the link in the hoster’s activation mail (check the spam folder too), then sign in again.',
  ],
  [
    'ownNewAddress',
    'The hoster saw a sign-in from a new IP address and wants it confirmed through the link it just emailed. Confirm it, then switch this login off and on again.',
  ],
])('says what a hoster login refused with %s waits for, not to check the password', async (code, words) => {
  const refused = { host: 'rapidgator.net', username: 'john', status: 'rejected', code, enabled: true };
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string) => {
      if (url === '/api/hosterauth/logins') return reply([refused]);
      if (url === '/api/settings') return reply({ premiumOnly: false });
      if (url === '/api/features') return reply({ modules: [], pages: [] });
      return reply([]);
    }),
  );
  await act(async () => root.render(page));
  const row = card('Hoster accounts').querySelector('[data-glim="ConnectionRow"][aria-label="rapidgator.net"]')!;
  expect(row.querySelector('.glim-connection-status')?.textContent).toBe(words);
});

/** stubPriority answers the download order with `rows` and records what the card saves. */
function stubPriority(rows: unknown[], saved: unknown[]) {
  const debrid = { id: 'torbox', label: 'TorBox', kind: 'apiKey', group: 'debrid', whereUrl: '' };
  const torbox = { id: 'torbox', service: 'torbox', account: '', label: '', enabled: true, configured: true, fromEnv: false, ok: true, detail: '' };
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url === '/api/accounts/catalogue') return reply([debrid]);
      if (url === '/api/accounts') return reply([torbox]);
      if (url === '/api/resolvers/priority' && init?.method === 'POST') {
        saved.push(JSON.parse(String(init.body)));
        return reply(rows);
      }
      if (url === '/api/resolvers/priority') return reply(rows);
      if (url === '/api/settings') return reply({ premiumOnly: false });
      if (url === '/api/features') return reply({ modules: [], pages: [] });
      return reply([]);
    }),
  );
}

it('shows a hoster in the download order only once a login for it is connected', async () => {
  stubPriority(
    [
      { id: 'mega', prio: 55 },
      { id: 'torbox', prio: 50 },
      { id: 'ddownload', prio: 42, ownHoster: true },
      { id: 'login:ddownload.com', prio: 60, ownLogin: true },
      { id: 'pixeldrain', prio: 42, ownHoster: true },
      { id: 'direct', prio: 40 },
      { id: 'jd', prio: 10 },
    ],
    [],
  );
  await act(async () => root.render(page));
  const rows = [...host.querySelectorAll('ol[aria-label="Download priority order"] li')];
  expect(rows.map((r) => r.getAttribute('data-ladder-id'))).toEqual(['mega', 'torbox', 'login:ddownload.com', 'direct', 'jd']);
});

it('has no order to set while only the built-in backends and the free hoster clients are there', async () => {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string) => {
      if (url === '/api/resolvers/priority') {
        return reply([
          { id: 'mega', prio: 55 },
          { id: 'torrent', prio: 50 },
          { id: 'filespace', prio: 42, ownHoster: true },
          { id: 'direct', prio: 40 },
          { id: 'ytdlp', prio: 30 },
          { id: 'jd', prio: 10 },
        ]);
      }
      if (url === '/api/settings') return reply({ premiumOnly: false });
      if (url === '/api/features') return reply({ modules: [], pages: [] });
      return reply([]);
    }),
  );
  await act(async () => root.render(page));
  expect(host.querySelector('ol[aria-label="Download priority order"]')).toBeNull();
  expect(host.textContent).toContain('No account is connected, so there is no order to set yet.');
});

it('keeps the place of the hoster clients it leaves out when a row is moved', async () => {
  const saved: unknown[] = [];
  stubPriority(
    [
      { id: 'torbox', prio: 1000 },
      { id: 'ddownload', prio: 999, ownHoster: true },
      { id: 'jd', prio: 998 },
      { id: 'katfile', prio: 42, ownHoster: true },
      { id: 'direct', prio: 40 },
    ],
    saved,
  );
  await act(async () => root.render(page));
  const grip = host.querySelector<HTMLButtonElement>('[aria-label="Download priority order"] button[aria-label="Move TorBox"]')!;
  await act(async () => grip.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, key: 'ArrowDown' })));
  expect(saved).toEqual([{ order: ['jd', 'ddownload', 'torbox', 'katfile', 'direct'] }]);
});

it('orders the connected captcha accounts beside the download backends and saves without a settings draft', async () => {
  const solver = (id: string, label: string) => ({ id, label, kind: 'apiKey', group: 'captchaSolver', whereUrl: '' });
  const key = (service: string) => ({ id: service, service, account: '', label: '', enabled: true, configured: true, fromEnv: false });
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url === '/api/accounts/catalogue') return reply([...services, solver('2captcha', '2Captcha')]);
      if (url === '/api/accounts') return reply([key('capsolver'), key('2captcha')]);
      if (url === '/api/settings' && init?.method === 'PATCH') {
        const body = JSON.parse(String(init.body));
        patched.push(body);
        return reply({ premiumOnly: false, captchaSolverOrder: null, ...body });
      }
      if (url === '/api/settings') return reply({ premiumOnly: false, captchaSolverOrder: null });
      if (url === '/api/features') return reply({ modules: [], pages: [] });
      return reply([]);
    }),
  );
  await act(async () => root.render(page));
  expect(host.textContent).toContain('Download priority order');
  expect(host.textContent).toContain('Captcha priority order');

  const grip = host.querySelector<HTMLButtonElement>('[aria-label="Captcha priority order"] button[aria-label="Move CapSolver"]')!;
  await act(async () => grip.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, key: 'ArrowDown' })));
  // Edits are gathered for a moment before they go out.
  await act(async () => void (await new Promise((done) => setTimeout(done, 700))));
  expect(patched).toEqual([{ captchaSolverOrder: ['2captcha', 'capsolver'] }]);
});

describe('a hoster login with two-factor sign-in', () => {
  const field = (caption: string) =>
    [...document.querySelectorAll('[role="dialog"] label')].find((l) => l.textContent?.startsWith(caption))?.querySelector('input');
  const type = (input: HTMLInputElement, text: string) => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, text);
    act(() => input.dispatchEvent(new Event('input', { bubbles: true })));
  };

  /**
   * The server answers each save with the next of `rows` and lists `stored`
   * as the logins there are.
   */
  function stubSaves(rows: unknown[], { stored = [] as unknown[] } = {}) {
    const posted: Record<string, unknown>[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string, init?: RequestInit) => {
        if (url === '/api/hosterauth/logins' && init?.method === 'POST') {
          posted.push(JSON.parse(String(init.body)));
          return reply(rows[posted.length - 1]);
        }
        if (url === '/api/hosterauth/logins') return reply(stored);
        if (url === '/api/hosterauth/hosts') return reply([{ id: 'rapidgator.net', label: 'rapidgator.net' }]);
        if (url === '/api/settings') return reply({ premiumOnly: false });
        if (url === '/api/features') return reply({ modules: [], pages: [] });
        return reply([]);
      }),
    );
    return posted;
  }

  const asking = (code: string) => ({ host: 'rapidgator.net', username: 'john', status: 'rejected', code, enabled: true, askCode: true });

  async function openNewLogin() {
    await act(async () => root.render(page));
    await addIn('Hoster accounts');
    const tile = [...document.querySelectorAll<HTMLButtonElement>('[data-new="hoster-logos"] button')].find((b) =>
      b.textContent?.startsWith('Rapidgator'),
    )!;
    await act(async () => tile.click());
    type(field('Username')!, 'john');
    type(field('Password')!, 'hunter2');
  }

  async function openStored() {
    await act(async () => root.render(page));
    const row = card('Hoster accounts').querySelector('[data-glim="ConnectionRow"][aria-label="rapidgator.net"]')!;
    await act(async () => row.querySelector<HTMLButtonElement>('[data-connection="edit"]')!.click());
  }

  async function saveLogin() {
    await openNewLogin();
    expect(field('Sign-in code')).toBeUndefined();
    await act(async () => button('Save').click());
  }

  it('leaves the code field out of a new login until the hoster asks for one', async () => {
    const posted = stubSaves([{ host: 'rapidgator.net', username: 'john', status: 'active', enabled: true }]);
    await openNewLogin();
    expect(field('Sign-in code')).toBeUndefined();
    await act(async () => button('Save').click());

    expect(posted).toEqual([{ host: 'rapidgator.net', username: 'john', password: 'hunter2' }]);
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('Connected');
  });

  it.each(['ownInvalid', 'ownActivate', 'ownNewAddress'])('edits a login refused with %s without the code field', async (code) => {
    stubSaves([], { stored: [{ host: 'rapidgator.net', username: 'john', status: 'rejected', code, enabled: true }] });
    await openStored();
    expect(field('Password')).toBeDefined();
    expect(field('Sign-in code')).toBeUndefined();
  });

  it('keeps the code field on a login whose session ended, and signs in with the new code', async () => {
    const posted = stubSaves([{ host: 'rapidgator.net', username: 'john', status: 'active', enabled: true }], {
      stored: [asking('ownCodeEnded')],
    });
    await openStored();
    expect(document.querySelector('#hoster-code-note')?.textContent).toBe(
      'The session has ended. Enter a new code to sign in again.',
    );
    expect(button('Save').disabled).toBe(true);
    type(field('Sign-in code')!, '482913');
    await act(async () => button('Save').click());

    expect(posted).toEqual([{ host: 'rapidgator.net', username: 'john', password: '********', code: '482913' }]);
  });

  it('asks for the code the hoster wants and signs in with it, keeping the window open until then', async () => {
    const posted = stubSaves([asking('ownCodeNeeded'), { host: 'rapidgator.net', username: 'john', status: 'active', enabled: true }]);
    await saveLogin();

    const code = field('Sign-in code')!;
    expect(code.getAttribute('inputmode')).toBe('numeric');
    expect(code.getAttribute('autocomplete')).toBe('one-time-code');
    expect(button('Save').disabled).toBe(true);
    type(code, '48 29-13');
    expect(code.value).toBe('482913');
    await act(async () => button('Save').click());

    expect(posted).toEqual([
      { host: 'rapidgator.net', username: 'john', password: 'hunter2' },
      { host: 'rapidgator.net', username: 'john', password: 'hunter2', code: '482913' },
    ]);
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain('Connected');
  });

  it('says so when the hoster turned the code down, and clears it for the next', async () => {
    stubSaves([asking('ownCodeNeeded'), asking('ownCodeWrong')]);
    await saveLogin();
    type(field('Sign-in code')!, '000000');
    await act(async () => button('Save').click());

    expect(document.querySelector('[role="dialog"] [role="alert"]')?.textContent).toBe(
      'That code did not work. Enter the current one from the app or the newest mail.',
    );
    expect(field('Sign-in code')!.value).toBe('');
  });
});
