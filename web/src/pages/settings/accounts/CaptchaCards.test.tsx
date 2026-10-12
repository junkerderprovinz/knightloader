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
import { CaptchaCards, canTakeATest, keyState, SolverRow, testRefusal } from './CaptchaCards';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let root: Root;
let host: HTMLDivElement;

beforeEach(() => {
  // The chain stands still, as it does for somebody who asked for less motion.
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

async function drawCards(order: string[]) {
  const patch = vi.fn();
  const cfg = { captchaSolverOrder: order, captchaSolverOnlyUnwatched: false, captchaSolverWait: 60 } as Settings;
  const draft = { cfg, patch } as unknown as SettingsDraft;
  await act(async () =>
    root.render(
      <I18nProvider>
        <ToastProvider>
          <MemoryRouter>
            <SettingsProvider draft={draft} features={{ features: { modules: [CAPTCHA], pages: [] }, toggle: async () => {} }}>
              <CaptchaCards hue={0} accounts={[account('capsolver', true, true)]} catalogue={CATALOGUE} />
            </SettingsProvider>
          </MemoryRouter>
        </ToastProvider>
      </I18nProvider>,
    ),
  );
  return patch;
}

const grip = (name: string) => host.querySelector<HTMLButtonElement>(`button[aria-label="Move ${name}"]`)!;
const pointer = (type: string, target: EventTarget, y: number) =>
  target.dispatchEvent(
    new PointerEvent(type, { bubbles: true, button: 0, isPrimary: true, pointerId: 1, pointerType: 'mouse', clientY: y }),
  );

describe('the captcha account order on the Accounts page', () => {
  it('carries the Captcha module switch and a grip on every account in the order', async () => {
    await drawCards(['capsolver', '2captcha']);
    expect(host.querySelector('[role="switch"][aria-label="Captcha"]')).not.toBeNull();
    expect(grip('CapSolver')).not.toBeNull();
    expect(grip('2Captcha')).not.toBeNull();
    // An account that is not in the order has nothing to move.
    expect(grip('9kw.eu')).toBeNull();
  });

  it('moves an account with the arrow keys on its grip', async () => {
    const patch = await drawCards(['capsolver', '2captcha']);
    await act(async () => {
      grip('2Captcha').dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, key: 'ArrowUp' }));
    });
    expect(patch).toHaveBeenCalledWith({ captchaSolverOrder: ['2captcha', 'capsolver'] });
  });

  it('saves the order an account is dragged into by its grip', async () => {
    const patch = await drawCards(['capsolver', '2captcha']);
    // The first move lifts the row, the next carries it below the other.
    await act(async () => pointer('pointerdown', grip('CapSolver'), 0));
    await act(async () => pointer('pointermove', document, 40));
    await act(async () => pointer('pointermove', document, 120));
    await act(async () => pointer('pointerup', document, 120));
    expect(patch).toHaveBeenCalledWith({ captchaSolverOrder: ['2captcha', 'capsolver'] });
  });
});

describe('the test captcha for the captcha accounts', () => {
  it('is offered only while an account in the order has a key and is switched on', () => {
    expect(canTakeATest(['2captcha'], [])).toBe(false);
    expect(canTakeATest(['2captcha'], [account('2captcha', false, true)])).toBe(false);
    expect(canTakeATest(['2captcha'], [account('2captcha', true, false)])).toBe(false);
    expect(canTakeATest([], [account('2captcha', true, true)])).toBe(false);
    expect(canTakeATest(['anticaptcha', '2captcha'], [account('2captcha', true, true)])).toBe(true);
  });
});

describe('a captcha account row', () => {
  it('says the account is switched off rather than that its key is set', () => {
    expect(keyState('2captcha', [account('2captcha', true, false)])).toBe('off');
    expect(keyState('2captcha', [account('2captcha', true, true)])).toBe('set');
    expect(keyState('2captcha', [account('2captcha', false, false)])).toBe('notSet');
    expect(keyState('2captcha', [])).toBe('notSet');
  });

  it('moves the key status under a name and link too wide to share a line with it', async () => {
    await act(async () =>
      root.render(
        <I18nProvider>
          <ul>
            <SolverRow
              svc={{ id: '2captcha', label: '2Captcha', kind: 'apiKey', group: 'captchaSolver', whereUrl: 'https://2captcha.com/' }}
              hue={0}
              enabled
              last
              state="off"
              onToggle={() => {}}
            />
          </ul>
        </I18nProvider>,
      ),
    );
    const name = host.querySelector('a')!.parentElement!;
    const status = [...host.querySelectorAll('span')].find((s) => s.textContent === 'Switched off')!;
    // A name cell allowed below its content's width lets the link run over
    // whatever sits beside it.
    expect(name.className).not.toMatch(/\bmin-w-0\b/);
    expect(name.parentElement!.className).toMatch(/\bflex-wrap\b/);
    expect(name.contains(status)).toBe(false);
    expect(status.parentElement!.parentElement!.className).toMatch(/\bflex-wrap\b/);
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
