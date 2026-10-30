// @vitest-environment jsdom
import { afterEach, describe, expect, it } from 'vitest';

import { de } from './locales/de';
import { en } from './locales/en';
import { explainFailure } from './taskError';
import type { TranslationKey } from './i18n';

const words =
  (dict: Record<string, string>) =>
  (key: TranslationKey, vars: Record<string, string | number> = {}): string =>
    Object.entries(vars).reduce<string>((s, [k, v]) => s.replaceAll(`{${k}}`, String(v)), dict[key]);

// The row JDownloader gave up on: its captcha sentence stays the evidence.
const spent = {
  error: 'jd: Skipped - Captcha is required',
  errorCode: 'quotaUsed',
  reason: 'limit',
};
// What the row passes for a JDownloader task, which must not become the service.
const fallback = { service: 'JDownloader', part: 'x.rar', file: 'x.rar', path: '/downloads' };

afterEach(() => {
  document.documentElement.lang = '';
});

describe('a spent download volume', () => {
  it('names the debrid service, the hoster and when the volume comes back', () => {
    document.documentElement.lang = 'en-GB';
    const got = explainFailure(
      words(en),
      { ...spent, errorParams: { host: 'rapidgator.net', service: 'Debrid-Link', resetsAt: '2026-10-30T12:00:00Z' } },
      fallback,
    );
    expect(got?.line).toBe('The Debrid-Link download volume for rapidgator.net is used up.');
    expect(got?.next).toMatch(/^The volume resets at .*30\/10\/2026.*\. Wait until then, or top up the account's traffic\.$/);
    expect(got?.raw).toBe('jd: Skipped - Captcha is required');
  });

  it('says it in German', () => {
    document.documentElement.lang = 'de';
    const got = explainFailure(
      words(de),
      { ...spent, errorParams: { host: 'rapidgator.net', service: 'Debrid-Link', resetsAt: '2026-10-30T12:00:00Z' } },
      fallback,
    );
    expect(got?.line).toBe('Das Download-Volumen bei Debrid-Link für rapidgator.net ist aufgebraucht.');
    expect(got?.next).toMatch(/^Ab .*30\.10\.26.* ist wieder Volumen da\. Warte bis dahin oder buch Traffic für das Konto nach\.$/);
  });

  it('blames the hoster login, not the backend, when no service is named', () => {
    const got = explainFailure(words(en), { ...spent, errorParams: { host: 'rapidgator.net' } }, fallback);
    expect(got?.line).toBe('The download volume of your rapidgator.net account is used up.');
    expect(got?.next).toBe(en['failure.quotaUsed.next']);
  });

  it('never reads as a captcha', () => {
    const got = explainFailure(words(en), { ...spent, errorParams: { host: 'rapidgator.net' } }, fallback);
    expect(got?.line).not.toBe(en['failure.captcha.line']);
    expect(got?.next.toLowerCase()).not.toContain('captcha');
  });
});
