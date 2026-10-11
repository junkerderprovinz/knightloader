// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { en } from '../../lib/locales/en';
import { PasswordCard } from './Access';
import { SettingsProvider, type SettingsDraft } from './context';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const { toast } = vi.hoisted(() => ({ toast: vi.fn() }));
vi.mock('../../lib/toast', () => ({ useToast: () => ({ toast }) }));

let root: Root;
let host: HTMLDivElement;
let puts: unknown[];
let answerPut: () => Response;

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

beforeEach(() => {
  toast.mockReset();
  puts = [];
  answerPut = () => json({ enabled: true, authenticated: true });
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input);
      if (path === '/api/auth/password') {
        puts.push(JSON.parse(String(init?.body)));
        return Promise.resolve(answerPut());
      }
      if (path === '/api/auth') return Promise.resolve(json({ enabled: false, authenticated: true }));
      return Promise.resolve(json({ exposed: false }));
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

async function open() {
  const draft = {
    cfg: { trustedProxies: [] },
    patch: () => {},
    fieldError: () => undefined,
    showRefusalsAt: () => () => {},
  } as unknown as SettingsDraft;
  const features = { features: { modules: [], pages: [] }, toggle: async () => {} };
  await act(async () =>
    root.render(
      <SettingsProvider draft={draft} features={features}>
        <PasswordCard onAuthChanged={() => {}} />
      </SettingsProvider>,
    ),
  );
}

async function setPassword(password: string) {
  const caption = [...host.querySelectorAll('[data-glim-label]')].find(
    (c) => c.getAttribute('data-glim-label') === en['settings.passwordNew'],
  )!;
  const input = caption.closest('label')!.querySelector('input')!;
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, password);
  act(() => input.dispatchEvent(new Event('input', { bubbles: true })));
  const apply = [...host.querySelectorAll('button')].find((b) => b.textContent === en['settings.setPassword'])!;
  await act(async () => apply.click());
}

describe('the password card', () => {
  it('counts characters, not bytes, and says so in its own words', async () => {
    await open();

    await setPassword('äöüßäöü');

    expect(toast).toHaveBeenCalledWith(en['auth.passwordShort'], 'fail');
    expect(puts).toHaveLength(0);
  });

  it('refuses a password longer than bcrypt takes before sending it', async () => {
    await open();

    await setPassword('ä'.repeat(37));

    expect(toast).toHaveBeenCalledWith(en['auth.passwordLong'], 'fail');
    expect(puts).toHaveLength(0);
  });

  it('translates the server’s refusal of the length instead of showing its English', async () => {
    answerPut = () => json({ code: 'passwordTooLong', error: 'the password must be at most 72 bytes' }, 400);
    await open();

    await setPassword('a-good-password');

    expect(puts).toHaveLength(1);
    expect(toast).toHaveBeenCalledWith(en['auth.passwordLong'], 'fail');
  });
});
