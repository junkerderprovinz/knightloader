// @vitest-environment jsdom
import { act, useState } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { HosterLogin } from '../lib/api';
import { I18nProvider } from '../lib/i18n';
import { en } from '../lib/locales/en';
import { ToastProvider } from '../lib/toast';
import { HosterLoginSection, useHosterLogins } from './HosterLoginSection';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;
/** The bodies of every save, in order. */
let saves: { host: string; username: string; password: string; code?: string }[];
/** What the next saves answer with, one row each. */
let answers: Partial<HosterLogin>[];
/** The rows the login list answers with. */
let listed: HosterLogin[];
/** A refusal the next save gets instead of a row. */
let refuse: string | undefined;

const row = (r: Partial<HosterLogin>): HosterLogin => ({
  host: 'rapidgator.net',
  username: 'john@example.com',
  status: 'active',
  enabled: true,
  ...r,
});

const reply = (body: unknown) =>
  new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } });

class OffScreen {
  observe() {}
  disconnect() {}
}

const button = (text: string) =>
  [...document.querySelectorAll<HTMLButtonElement>('button')]
    .reverse()
    .find((b) => b.textContent === text || b.getAttribute('aria-label') === text);

const dialog = () => document.querySelector<HTMLElement>('[role="dialog"]');
const codeField = () => document.querySelector<HTMLInputElement>('input[autocomplete="one-time-code"]');
const passwordField = () => document.querySelector<HTMLInputElement>('input[type="password"]');

function type(input: HTMLInputElement, text: string) {
  const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!;
  act(() => {
    set.call(input, text);
    input.dispatchEvent(new Event('input', { bubbles: true }));
  });
}

beforeEach(() => {
  saves = [];
  answers = [];
  listed = [];
  refuse = undefined;
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  vi.stubGlobal('IntersectionObserver', OffScreen);
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url === '/api/hosterauth/logins' && init?.method === 'POST') {
        const body = JSON.parse(String(init.body));
        saves.push(body);
        if (refuse) return new Response(JSON.stringify({ error: refuse }), { status: 502 });
        const saved = row(answers.shift() ?? {});
        listed = [saved];
        return reply(saved);
      }
      if (url === '/api/hosterauth/logins') return reply(listed);
      if (url === '/api/hosterauth/hosts') return reply([{ id: 'rapidgator.net', label: 'rapidgator.net' }]);
      if (url === '/api/connect') return reply({ active: true, apps: [] });
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

/** Opens the window on its picker of hosters. */
async function openPicker() {
  await act(async () =>
    root.render(
      <I18nProvider>
        <ToastProvider>
          <HosterCard />
        </ToastProvider>
      </I18nProvider>,
    ),
  );
  await act(async () => button('Add')!.click());
}

const rapidgatorTile = () =>
  [...document.querySelectorAll<HTMLElement>('.glim-readme-btn-unit')].find(
    (li) => li.querySelector('.glim-readme-btn-name')?.textContent === 'rapidgator.net',
  )!;

/** Opens the window, picks Rapidgator and fills in the login. */
async function fillIn() {
  await openPicker();
  const tile = rapidgatorTile();
  await act(async () => (tile.querySelector('button') ?? tile).click());
  type(document.querySelector<HTMLInputElement>('input[autocomplete="off"]')!, 'john@example.com');
  type(passwordField()!, 'hunter2');
}

const save = () => act(async () => button(en['accounts.save'])!.click());

describe('the hoster login window', () => {
  // The debrid and Usenet pickers show a brand's site under its name while the
  // pointer is on the tile, and the hoster picker reads the same.
  it('shows each hoster’s site under its name on the tile', async () => {
    await openPicker();
    expect(rapidgatorTile().querySelector('.glim-readme-btn-sub')?.textContent).toBe('rapidgator.net');
  });

  it('stays open after Save and says Connected once the hoster takes the login', async () => {
    await fillIn();
    await save();
    expect(dialog()).not.toBeNull();
    expect(dialog()!.textContent).toContain(en['accounts.hoster.connected']);
    const connected = button(en['accounts.hoster.connected'])!;
    expect(connected.disabled).toBe(true);
    await act(async () => button(en['common.close'])!.click());
    expect(dialog()).toBeNull();
  });

  it('says why a wrong password was turned down and offers no code field', async () => {
    answers = [{ status: 'rejected', code: 'ownInvalid', detail: 'rapidgator: check the password' }];
    await fillIn();
    await save();
    expect(dialog()!.textContent).toContain(en['accounts.hoster.detail.ownInvalid']);
    expect(codeField()).toBeNull();
    expect(button(en['accounts.save'])!.disabled).toBe(false);
  });

  it.each([
    [{ code: 'ownCodeNeeded', askCode: true }, 'accounts.hoster.codeNeeded'],
    [{ code: 'ownCodeWrong', askCode: true }, 'accounts.hoster.codeWrong'],
    [{ code: 'ownCodeEnded', askCode: true }, 'accounts.hoster.codeEnded'],
    [{ code: 'ownActivate', codeMayHelp: true }, 'accounts.hoster.codeActivate'],
    [{ code: 'ownNewAddress', codeMayHelp: true }, 'accounts.hoster.codeNewAddress'],
  ] as const)('shows the code field with its reason for %o', async (answer, note) => {
    answers = [{ status: 'rejected', ...answer }];
    await fillIn();
    await save();
    expect(codeField()).not.toBeNull();
    expect(document.getElementById('hoster-code-note')?.textContent).toBe(en[note]);
  });

  it('sends the code once and tests the login again in the same window', async () => {
    answers = [{ status: 'rejected', code: 'ownActivate', codeMayHelp: true }, { status: 'active' }];
    await fillIn();
    await save();
    type(codeField()!, '482913');
    await save();
    expect(saves.map((s) => s.code)).toEqual([undefined, '482913']);
    expect(codeField()).toBeNull();
    expect(button(en['accounts.hoster.connected'])!.disabled).toBe(true);
  });

  it.each([
    ['ownActivate', 'accounts.hoster.codeDidNotHelp'],
    ['ownNewAddress', 'accounts.hoster.codeDidNotHelpAddress'],
  ] as const)('says so when the hoster answers %s with the code as well', async (refusal, note) => {
    answers = [
      { status: 'rejected', code: refusal, codeMayHelp: true },
      { status: 'rejected', code: refusal, codeMayHelp: true },
    ];
    await fillIn();
    await save();
    type(codeField()!, '482913');
    await save();
    expect(saves.map((s) => s.code)).toEqual([undefined, '482913']);
    const shown = document.getElementById('hoster-code-note')!;
    expect(shown.textContent).toBe(en[note]);
    expect(shown.getAttribute('role')).toBe('alert');
    expect(codeField()!.value).toBe('');
  });

  it('says in the window why a save with a code did not go through, and keeps the code', async () => {
    answers = [{ status: 'rejected', code: 'ownActivate', codeMayHelp: true }];
    await fillIn();
    await save();
    type(codeField()!, '482913');
    refuse = 'rapidgator: Rapidgator could not be reached';
    await save();
    expect(dialog()!.querySelector('[role="status"]')?.textContent).toBe('rapidgator: Rapidgator could not be reached');
    expect(codeField()!.value).toBe('482913');
  });

  it('waits for a login another backend checks and says Connected once it is', async () => {
    answers = [{ status: 'queued', code: 'adding' }];
    await fillIn();
    await save();
    expect(dialog()!.textContent).toContain(en['accounts.hoster.testing']);
    listed = [row({ status: 'active' })];
    await act(() => new Promise((resolve) => setTimeout(resolve, 2200)));
    expect(dialog()!.textContent).toContain(en['accounts.hoster.connected']);
  });

  it('offers Save again once a field changes after the login connected', async () => {
    await fillIn();
    await save();
    type(passwordField()!, 'hunter3');
    expect(button(en['accounts.save'])!.disabled).toBe(false);
  });
});
