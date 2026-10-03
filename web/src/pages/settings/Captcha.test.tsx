// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { describe, expect, it } from 'vitest';

import { type Account, ApiError } from '../../lib/api';
import { I18nProvider } from '../../lib/i18n';
import { canTakeATest, keyState, SolverRow, testRefusal } from './Captcha';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

function account(service: string, configured: boolean, enabled: boolean): Account {
  return { service, account: '', configured, enabled } as Account;
}

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

  it('moves the key status and the actions under a name and link too wide to share a line with them', async () => {
    const host = document.createElement('div');
    document.body.append(host);
    const root = createRoot(host);
    await act(async () =>
      root.render(
        <I18nProvider>
          <ul>
            <SolverRow
              svc={{ id: '2captcha', label: '2Captcha', kind: 'apiKey', group: 'captchaSolver', whereUrl: 'https://2captcha.com/' }}
              hue={0}
              enabled
              position={0}
              count={2}
              last
              state="off"
              onToggle={() => {}}
              onMove={() => {}}
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
    expect(status.parentElement!.className).toMatch(/\bflex-wrap\b/);
    act(() => root.unmount());
    host.remove();
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
