// @vitest-environment jsdom
// The gate asks the kit whether the instance wants a sign-in and shows the app
// or GlimStone's sign-in card.
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { refusal } from '../lib/api';
import { I18nProvider } from '../lib/i18n';
import { AuthGate, gateOpens } from './AuthGate';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const offer = (open: boolean, signedIn: boolean) => ({ open, owner: true, minPassword: 12, signedIn, passkeys: false, services: [] });

describe('gateOpens', () => {
  it('opens for everyone while nobody can sign in', () => {
    expect(gateOpens(offer(true, false))).toBe(true);
  });
  it('opens for a browser that signed in', () => {
    expect(gateOpens(offer(false, true))).toBe(true);
  });
  it('asks a browser that has not signed in', () => {
    expect(gateOpens(offer(false, false))).toBe(false);
  });
  it('opens when the server could not be asked', () => {
    expect(gateOpens(null)).toBe(true);
  });
});

let root: Root;
let host: HTMLDivElement;
let answer: () => Response;

beforeEach(() => {
  vi.stubGlobal(
    'fetch',
    vi.fn(() => Promise.resolve(answer())),
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

const json = (body: unknown, status = 200) => () =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

async function render() {
  await act(async () =>
    root.render(
      <I18nProvider>
        <AuthGate>
          <p data-testid="app">the app</p>
        </AuthGate>
      </I18nProvider>,
    ),
  );
}

describe('AuthGate', () => {
  it('shows the app on an open instance', async () => {
    answer = json(offer(true, false));
    await render();
    expect(host.querySelector('[data-testid="app"]')).not.toBeNull();
  });

  it('shows the sign-in card with the forgotten password as a link', async () => {
    answer = json(offer(false, false));
    await render();
    expect(host.querySelector('[data-testid="app"]')).toBeNull();
    expect(host.querySelector('.glim-signin-card')).not.toBeNull();
    expect(host.querySelector('.glim-signin-forgot')?.textContent).toBe('Forgot password?');
  });

  it('brings the card back once a session ends under an open page', async () => {
    answer = json(offer(false, true));
    await render();
    expect(host.querySelector('[data-testid="app"]')).not.toBeNull();
    answer = json(offer(false, false));
    await act(async () => {
      await refusal(new Response('unauthorized', { status: 401 }));
    });
    await act(async () => {});
    expect(host.querySelector('.glim-signin-card')).not.toBeNull();
    expect(host.textContent).toContain('Your session ran out. Please sign in again.');
  });
});
