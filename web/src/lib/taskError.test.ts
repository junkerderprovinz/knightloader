import { describe, expect, it } from 'vitest';

import serverCodes from '../../../internal/core/errorcode.go?raw';
import { en } from './locales/en';
import { explainFailure } from './taskError';
import type { TranslationKey } from './i18n';

// interpolate's own isolate marks need a page to read the direction from.
const t = (key: TranslationKey, vars: Record<string, string | number> = {}): string =>
  Object.entries(vars).reduce<string>((s, [k, v]) => s.replaceAll(`{${k}}`, String(v)), en[key]);

describe('explainFailure', () => {
  it('words a damaged part with its name and the way out, and keeps the tool\'s own words for support', () => {
    const got = explainFailure(t, {
      error: 'extract: Vier.minus.drei.part1.rar: rardecode: bad block header',
      errorCode: 'archiveDamaged',
      errorParams: { part: 'Vier.minus.drei.part1.rar' },
    });
    expect(got?.line).toBe('Vier.minus.drei.part1.rar is damaged.');
    expect(got?.next).toContain('another mirror');
    expect(got?.raw).toBe('extract: Vier.minus.drei.part1.rar: rardecode: bad block header');
  });

  it('fills a value the server left out from the row', () => {
    const got = explainFailure(t, { error: 'x', errorCode: 'archiveDamaged' }, { part: 'film.part1.rar' });
    expect(got?.line).toBe('film.part1.rar is damaged.');
  });

  it('gives a failure nothing recognised the general sentence beside its own words', () => {
    const got = explainFailure(t, { error: 'rapidgator: error code 7731' });
    expect(got?.line).toBe(en['failure.unknown.line']);
    expect(got?.raw).toBe('rapidgator: error code 7731');
  });

  it('reads a row stored before codes existed by its reason', () => {
    const got = explainFailure(t, { error: 'http request fail, code:404', reason: 'gone' });
    expect(got?.line).toBe(en['failure.gone.line']);
  });

  it('lets a code outrank the reason it belongs to', () => {
    const got = explainFailure(t, { error: 'i/o timeout', errorCode: 'timeout', reason: 'network' });
    expect(got?.line).toBe(en['failure.timeout.line']);
  });

  it('gives a code from a newer server the general sentence', () => {
    const got = explainFailure(t, { error: 'x', errorCode: 'somethingNew', reason: 'gone' });
    expect(got?.line).toBe(en['failure.unknown.line']);
  });

  it('says nothing about a row that has not failed', () => {
    expect(explainFailure(t, {})).toBeNull();
  });
});

// Every code the server sends has words here, so none of them falls back to
// the general sentence. The codes are read from the server's own list.
it('has words for every code the server sends', () => {
  const codes = [...serverCodes.matchAll(/ErrorCode = "(\w+)"/g)].map((m) => m[1]);
  expect(codes.length).toBeGreaterThan(20);
  for (const code of codes) {
    expect(explainFailure(t, { error: 'x', errorCode: code })?.line, code).not.toBe(en['failure.unknown.line']);
  }
});
