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
    saved: { trustedProxies: [] },
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

/** The field of the row captioned `caption`, or undefined while the row is not there. */
const field = (caption: string) =>
  [...host.querySelectorAll('[data-glim-label]')]
    .find((c) => c.getAttribute('data-glim-label') === caption)
    ?.closest('label')
    ?.querySelector('input') ?? undefined;

function type(input: HTMLInputElement, text: string) {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, text);
  act(() => input.dispatchEvent(new Event('input', { bubbles: true })));
}

const apply = () => [...host.querySelectorAll('button')].find((b) => b.textContent === en['settings.setPassword'])!;

async function setPassword(password: string) {
  type(field(en['settings.passwordNew'])!, password);
  type(field(en['auth.reset.repeat'])!, password);
  await act(async () => apply().click());
}

describe('the password card', () => {
  it('says whether a password is set just before the button, both at the end of the row', async () => {
    await open();
    const apply = [...host.querySelectorAll('button')].find((b) => b.textContent === en['settings.setPassword'])!;
    const row = apply.parentElement!;
    const state = apply.previousElementSibling!;
    expect(row.className).toContain('justify-end');
    expect(state.textContent).toBe(en['settings.lockOff']);
    // A state that grew would push itself to the start of the row, away from the button.
    expect(state.className).not.toMatch(/(^|\s)(flex-1|grow|flex-\[1)/);
    expect(state.className).toContain('text-end');
    expect(row.lastElementChild).toBe(apply);
  });

  it('sets a short password and only says beforehand that it is easy to guess', async () => {
    await open();
    expect(host.textContent).not.toContain(en['auth.passwordShortHint']);

    type(field(en['settings.passwordNew'])!, 'äöüßäöü');
    expect(host.textContent).toContain(en['auth.passwordShortHint']);
    await setPassword('äöüßäöü');

    expect(toast).not.toHaveBeenCalledWith(expect.anything(), 'fail');
    expect(puts).toEqual([{ current: '', new: 'äöüßäöü' }]);
  });

  it('drops the hint once the password has twelve characters', async () => {
    await open();

    type(field(en['settings.passwordNew'])!, 'äöüßäöüéäöüß');

    expect(host.textContent).not.toContain(en['auth.passwordShortHint']);
  });

  it('refuses a password longer than bcrypt takes in its own words', async () => {
    await open();

    await setPassword('ä'.repeat(37));

    expect(toast).toHaveBeenCalledWith(en['auth.passwordLong'], 'fail');
    expect(puts).toHaveLength(0);
  });

  it('asks for a new password a second time once one is typed, and sets it only when both match', async () => {
    await open();
    expect(field(en['auth.reset.repeat'])).toBeUndefined();

    type(field(en['settings.passwordNew'])!, 'a-good-password');
    expect(field(en['auth.reset.repeat'])).toBeDefined();
    expect(apply().disabled).toBe(true);

    type(field(en['auth.reset.repeat'])!, 'a-good-passwort');
    expect(host.textContent).toContain(en['auth.reset.mismatch']);
    expect(field(en['auth.reset.repeat'])!.getAttribute('aria-invalid')).toBe('true');
    expect(apply().disabled).toBe(true);

    type(field(en['auth.reset.repeat'])!, 'a-good-password');
    expect(host.textContent).not.toContain(en['auth.reset.mismatch']);
    await act(async () => apply().click());
    expect(puts).toEqual([expect.objectContaining({ new: 'a-good-password' })]);
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
