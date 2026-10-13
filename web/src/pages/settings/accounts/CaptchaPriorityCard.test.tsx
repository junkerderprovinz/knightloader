// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { type Account, ApiError, type CatalogueService, type Settings } from '../../../lib/api';
import { I18nProvider } from '../../../lib/i18n';
import { ToastProvider } from '../../../lib/toast';
import { SettingsProvider, type SettingsDraft } from '../context';
import type { Feature } from '../features';
import { CaptchaPriorityCard, connectedSolvers, solverAccount, testRefusal } from './CaptchaPriorityCard';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

beforeEach(() => {
  // The picture stands still, as it does for somebody who asked for less motion.
  vi.stubGlobal('matchMedia', () => ({ matches: true, addEventListener() {}, removeEventListener() {} }));
  vi.stubGlobal(
    'IntersectionObserver',
    class {
      observe() {}
      disconnect() {}
    },
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

function account(service: string, configured: boolean, enabled: boolean): Account {
  return { service, account: '', configured, enabled } as Account;
}

const solver = (id: string, label: string) =>
  ({ id, label, kind: 'apiKey', group: 'captchaSolver', whereUrl: '' }) as CatalogueService;
const CATALOGUE = [solver('2captcha', '2Captcha'), solver('9kw', '9kw.eu'), solver('capsolver', 'CapSolver')];
const CAPTCHA: Feature = { id: 'captcha', verdict: 'shipped', page: 'accounts', enabled: true, switch: 'setting', parked: false };
const BOTH = [account('capsolver', true, true), account('2captcha', true, true)];

async function drawCard(order: string[], accounts: Account[] = BOTH) {
  const patch = vi.fn();
  const cfg = { captchaSolverOrder: order, captchaSolverOnlyUnwatched: false, captchaSolverWait: 60 } as Settings;
  const draft = { cfg, patch } as unknown as SettingsDraft;
  await act(async () =>
    root.render(
      <I18nProvider>
        <ToastProvider>
          <MemoryRouter>
            <SettingsProvider draft={draft} features={{ features: { modules: [CAPTCHA], pages: [] }, toggle: async () => {} }}>
              <CaptchaPriorityCard hue={0} accounts={accounts} catalogue={CATALOGUE} />
            </SettingsProvider>
          </MemoryRouter>
        </ToastProvider>
      </I18nProvider>,
    ),
  );
  return patch;
}

const grip = (name: string) => host.querySelector<HTMLButtonElement>(`button[aria-label="Move ${name}"]`);
const rows = () => [...host.querySelectorAll('[data-ladder-id]')].map((r) => r.getAttribute('data-ladder-id'));

describe('the captcha priority card', () => {
  it('lists only the connected captcha accounts, in the stored order', async () => {
    await drawCard(['capsolver', '2captcha'], [...BOTH, account('9kw', true, false)]);
    expect(host.textContent).toContain('Captcha priority order');
    expect(rows()).toEqual(['capsolver', '2captcha']);
    expect(grip('9kw.eu')).toBeNull();
  });

  it('names each station right beside it, ahead of the grip', async () => {
    await drawCard(['capsolver', '2captcha']);
    const first = host.querySelector('[data-ladder-id="capsolver"]')!;
    expect(first.firstElementChild!.textContent).toBe('CapSolver');
    expect(first.lastElementChild).toBe(grip('CapSolver'));
  });

  it('names the captcha glyph above the first station in the type of the rows', async () => {
    await drawCard(['capsolver', '2captcha']);
    const entry = host.querySelector('[data-new="priority-start-names"]')!;
    const row = host.querySelector('[data-ladder-id="capsolver"]')!;
    expect(entry.textContent).toBe('New captcha');
    const type = (el: Element) => [...el.classList].filter((c) => c.startsWith('text-') || c.startsWith('leading-'));
    expect(type(entry.firstElementChild!)).toEqual(type(row.firstElementChild!.firstElementChild!));
  });

  it('puts a connected account the order leaves out after the ones it names', async () => {
    await drawCard(['capsolver']);
    expect(rows()).toEqual(['capsolver', '2captcha']);
  });

  it('leads to the Captcha module switch and carries when the accounts start and the test', async () => {
    await drawCard([]);
    expect(host.querySelector('[role="switch"][aria-label="Captcha"]')).toBeNull();
    // The way to the switch ends the row of test buttons, a button like them.
    const row = [...host.querySelectorAll('button')].filter((b) => b.closest('.flex-wrap'));
    expect(row.map((b) => b.textContent)).toEqual([
      'Send a test captcha',
      'Send one to the captcha accounts too',
      'On and off under Modules',
    ]);
    expect(host.textContent).toContain('When captcha accounts start');
    expect(host.textContent).toContain('Send one to the captcha accounts too');
  });

  it('has nothing to order and nothing to start while no account is connected', async () => {
    await drawCard(['capsolver'], [account('capsolver', true, false)]);
    expect(rows()).toEqual([]);
    expect(host.textContent).not.toContain('When captcha accounts start');
    expect(host.textContent).not.toContain('Send one to the captcha accounts too');
  });

  it('moves an account with the arrow keys on its grip and keeps the place of one whose key is gone', async () => {
    const patch = await drawCard(['capsolver', '9kw', '2captcha']);
    await act(async () => {
      grip('2Captcha')!.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, key: 'ArrowUp' }));
    });
    expect(patch).toHaveBeenCalledWith({ captchaSolverOrder: ['2captcha', 'capsolver', '9kw'] });
  });
});

describe('the connected captcha accounts', () => {
  it('leave out an account without a key and one that is switched off', () => {
    const accounts = [account('2captcha', true, false), account('9kw', false, true), account('capsolver', true, true)];
    expect(connectedSolvers([], accounts, CATALOGUE).map((s) => s.id)).toEqual(['capsolver']);
  });

  it('count a key stored under a name, as the instance tries it', () => {
    const named = { ...account('2captcha', true, true), account: 'Testkonto' };
    expect(connectedSolvers([], [named], CATALOGUE).map((s) => s.id)).toEqual(['2captcha']);
  });

  it('take the unnamed account over a named one, and a named one only while it is switched on', () => {
    const unnamed = account('capsolver', true, true);
    const named = { ...account('capsolver', true, true), account: 'Testkonto' };
    expect(solverAccount('capsolver', [named, unnamed])).toBe(unnamed);
    expect(solverAccount('capsolver', [{ ...named, enabled: false }])).toBeUndefined();
  });
});

describe('a refused test captcha', () => {
  it('names what stands in the way', () => {
    expect(testRefusal(new ApiError('', 'captchaOff'))).toBe('settings.captcha.testOff');
    expect(testRefusal(new ApiError('', 'captchaJDOff'))).toBe('settings.captcha.testJDOff');
    expect(testRefusal(new ApiError('', 'noCaptchaAccount'))).toBe('settings.captcha.testNoAccount');
    expect(testRefusal(new TypeError('Failed to fetch'))).toBe('captcha.networkError');
  });
});
