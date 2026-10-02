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

beforeEach(() => {
  patched = [];
  vi.stubGlobal('WebSocket', QuietSocket);
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
