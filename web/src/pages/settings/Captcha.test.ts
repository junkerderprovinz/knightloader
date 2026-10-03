import { describe, expect, it } from 'vitest';

import { type Account, ApiError } from '../../lib/api';
import { canTakeATest, keyState, testRefusal } from './Captcha';

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
});

describe('a refused test captcha', () => {
  it('names what stands in the way', () => {
    expect(testRefusal(new ApiError('', 'captchaOff'))).toBe('settings.captcha.testOff');
    expect(testRefusal(new ApiError('', 'captchaJDOff'))).toBe('settings.captcha.testJDOff');
    expect(testRefusal(new ApiError('', 'noCaptchaAccount'))).toBe('settings.captcha.testNoAccount');
    expect(testRefusal(new TypeError('Failed to fetch'))).toBe('captcha.networkError');
  });
});
