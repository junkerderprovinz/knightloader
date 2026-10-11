// @vitest-environment jsdom
// The sign-in screen's way back in for somebody who lost the password: it has
// the server write a code where only a person with access to the machine can
// read it, then takes that code and a new password.
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { refusal } from '../lib/api';
import { I18nProvider } from '../lib/i18n';
import { en } from '../lib/locales/en';
import { AuthGate } from './AuthGate';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const LOCKED = { enabled: true, authenticated: false };
const SIGNED_IN = { enabled: true, authenticated: true };
const NEW_PASSWORD = 'another-good-password';

interface Call {
  path: string;
  body: unknown;
}

let root: Root;
let host: HTMLDivElement;
let calls: Call[];
/** What the server answers, by path. */
let answers: Record<string, () => Response>;

function answer(body: unknown, status = 200): () => Response {
  return () =>
    new Response(typeof body === 'string' ? body : JSON.stringify(body), {
      status,
      headers: { 'Content-Type': 'application/json' },
    });
}

beforeEach(() => {
  calls = [];
  answers = {
    '/api/auth': answer(LOCKED),
    '/api/auth/reset': answer({ place: 'container' }),
  };
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input);
      calls.push({ path, body: typeof init?.body === 'string' ? JSON.parse(init.body) : undefined });
      const a = answers[path];
      if (!a) return Promise.reject(new Error(`no answer for ${path}`));
      return Promise.resolve(a());
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
  await act(async () =>
    root.render(
      <I18nProvider>
        <AuthGate>
          <p>the app</p>
        </AuthGate>
      </I18nProvider>,
    ),
  );
}

function button(label: string): HTMLButtonElement | undefined {
  return [...host.querySelectorAll('button')].find((b) => b.textContent === label);
}

async function press(label: string) {
  await act(async () => button(label)!.click());
}

function field(label: string): HTMLInputElement | null {
  const caption = [...host.querySelectorAll('[data-glim-label]')].find(
    (c) => c.getAttribute('data-glim-label') === label,
  );
  return caption?.closest('label')?.querySelector('input') ?? null;
}

function type(label: string, text: string) {
  const input = field(label)!;
  // React tracks the value it last rendered, so the native setter is what
  // makes the input event count as a change.
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, text);
  act(() => input.dispatchEvent(new Event('input', { bubbles: true })));
}

async function forgot() {
  await open();
  await press(en['auth.forgot']);
}

async function fillAndSubmit(code: string, password: string, again = password) {
  type(en['auth.reset.code.container'], code);
  type(en['settings.passwordNew'], password);
  type(en['auth.reset.repeat'], again);
  await press(en['settings.setPassword']);
}

function alert(): string | null {
  return host.querySelector('[role="alert"]')?.textContent ?? null;
}

function posts(path: string): Call[] {
  return calls.filter((c) => c.path === path);
}

describe('a forgotten password on the sign-in screen', () => {
  it('has the code written before it asks for one', async () => {
    await open();
    expect(field(en['auth.reset.code.container'])).toBeNull();

    await press(en['auth.forgot']);

    expect(posts('/api/auth/reset')).toHaveLength(1);
    expect(field(en['auth.reset.code.container'])).not.toBeNull();
    expect(field(en['auth.password'])).toBeNull();
  });

  it('names the place the server wrote to, and the folder where the desktop app gave one', async () => {
    answers['/api/auth/reset'] = answer({
      place: 'file',
      file: 'password-reset-code.txt',
      dir: '/home/somebody/.config/KnightLoader',
    });
    await forgot();

    const caption = [...host.querySelectorAll('[data-glim-label]')].find(
      (c) => c.getAttribute('data-glim-label') === en['auth.reset.code.file'],
    )!;
    const explained = caption.querySelector('[aria-label]')!.getAttribute('aria-label')!;
    expect(explained).toContain('password-reset-code.txt');
    expect(explained).toContain('/home/somebody/.config/KnightLoader');
    expect(explained).toContain(en['auth.reset.effect']);
  });

  it('says so in its own words when the server could write no code, and stays on the sign-in form', async () => {
    answers['/api/auth/reset'] = answer({ code: 'resetUnavailable', error: 'xx-not-a-sentence-xx' }, 503);

    await forgot();

    expect(alert()).toBe(en['auth.reset.unavailable']);
    expect(host.textContent).not.toContain('xx-not-a-sentence-xx');
    expect(field(en['auth.password'])).not.toBeNull();
  });

  it('sets the password with the code and opens the app', async () => {
    answers['/api/auth/reset/finish'] = answer(SIGNED_IN);
    await forgot();

    await fillAndSubmit('abcd-efgh', NEW_PASSWORD);

    expect(posts('/api/auth/reset/finish').map((c) => c.body)).toEqual([{ code: 'abcd-efgh', password: NEW_PASSWORD }]);
    expect(host.textContent).toBe('the app');
  });

  it('sends nothing while the two passwords differ', async () => {
    await forgot();

    await fillAndSubmit('abcd-efgh', NEW_PASSWORD, 'another-good-passwort');

    expect(alert()).toBe(en['auth.reset.mismatch']);
    expect(posts('/api/auth/reset/finish')).toHaveLength(0);
  });

  it.each([
    ['shorter than the server takes', 'short'],
    ['longer than the server takes', 'a'.repeat(73)],
  ])('sends nothing for a password %s', async (_, password) => {
    await forgot();

    await fillAndSubmit('abcd-efgh', password);

    expect(alert()).toBe(en['auth.reset.length']);
    expect(posts('/api/auth/reset/finish')).toHaveLength(0);
  });

  it('asks for a code again after a refused one, empties the field and keeps the passwords', async () => {
    answers['/api/auth/reset/finish'] = answer({ code: 'codeRejected', error: 'that code was not accepted' }, 401);
    await forgot();

    await fillAndSubmit('abcd-efgh', NEW_PASSWORD);

    expect(alert()).toBe(en['auth.reset.rejected']);
    expect(posts('/api/auth/reset')).toHaveLength(2);
    expect(field(en['auth.reset.code.container'])!.value).toBe('');
    expect(field(en['settings.passwordNew'])!.value).toBe(NEW_PASSWORD);
  });

  it('says to wait once the attempts have run out, and keeps the code', async () => {
    answers['/api/auth/reset/finish'] = answer('too many attempts, wait a moment', 429);
    await forgot();

    await fillAndSubmit('abcd-efgh', NEW_PASSWORD);

    expect(alert()).toBe(en['auth.reset.wait']);
    expect(posts('/api/auth/reset')).toHaveLength(1);
    expect(field(en['auth.reset.code.container'])!.value).toBe('abcd-efgh');
  });

  it('goes back to the password', async () => {
    await forgot();

    await press(en['onboarding.back']);

    expect(field(en['auth.password'])).not.toBeNull();
    expect(field(en['auth.reset.code.container'])).toBeNull();
  });

  it('is not offered beside the second factor’s code, where the password was right', async () => {
    answers['/api/auth/login'] = answer({ ...LOCKED, twoFactorRequired: true });
    await open();
    type(en['auth.password'], 'a-good-password');

    await press(en['auth.signIn']);

    expect(field(en['auth.twoFactor.confirmLabel'])).not.toBeNull();
    expect(button(en['auth.forgot'])).toBeUndefined();
  });
});

describe('a session that ends under an open page', () => {
  it('brings the sign-in screen back at the next refused call', async () => {
    answers['/api/auth'] = answer(SIGNED_IN);
    await open();
    expect(host.textContent).toBe('the app');

    answers['/api/auth'] = answer(LOCKED);
    await act(async () => {
      await refusal(new Response('unauthorized', { status: 401 }));
    });

    expect(field(en['auth.password'])).not.toBeNull();
    expect(host.textContent).not.toContain('the app');
  });

  it('stays in the app when the refusal was a peer’s and this session still holds', async () => {
    answers['/api/auth'] = answer(SIGNED_IN);
    await open();

    await act(async () => {
      await refusal(new Response('unauthorized', { status: 401 }));
    });

    expect(host.textContent).toBe('the app');
  });
});
