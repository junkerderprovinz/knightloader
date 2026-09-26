import { describe, expect, it } from 'vitest';

import serverCodes from '../../../internal/core/errorcode.go?raw';
import serverTask from '../../../internal/core/task.go?raw';
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

  it('words a link the link filter rejected by its rule, and says how to let it through', () => {
    const got = explainFailure(t, {
      error: 'rejected by link filter rule "no samples"',
      rejectCode: 'filterRule',
      rejectParams: { rule: 'no samples' },
    });
    expect(got?.line).toBe('rejected by link filter rule "no samples"');
    expect(got?.next).toBe(en['failure.filterRule.next']);
  });

  it('words a torrent the tracker ban rejected', () => {
    const got = explainFailure(t, {
      error: 'x',
      rejectCode: 'bannedTracker',
      rejectParams: { host: 'tracker.example' },
    });
    expect(got?.line).toBe('announces tracker.example, which is on the banned trackers list');
    expect(got?.next).toBe(en['failure.bannedTracker.next']);
  });

  it('names the file the system refused, or the row folder where the server did not', () => {
    expect(explainFailure(t, { error: 'x', errorCode: 'noPermission', errorParams: { path: '/config/cookies.txt' } })?.line).toBe(
      'KnightLoader is not allowed to access /config/cookies.txt.',
    );
    expect(explainFailure(t, { error: 'x', errorCode: 'noPermission' }, { path: '/downloads' })?.line).toBe(
      'KnightLoader is not allowed to access /downloads.',
    );
  });
});

// A row stored before codes existed reads by its reason, as the code the
// server gives that reason (core.Reason.Code), so the row's badge and its line
// agree. Both lists are read from the server's own source.
it('reads every reason as the code the server gives it', () => {
  const reasons = new Map([...serverTask.matchAll(/(Reason\w+) +Reason = "(\w+)"/g)].map((m) => [m[1], m[2]]));
  const codes = new Map([...serverCodes.matchAll(/(Code\w+) +ErrorCode = "(\w+)"/g)].map((m) => [m[1], m[2]]));
  const pairs = [...serverCodes.matchAll(/case (Reason\w+):\s+return (Code\w+)/g)];
  expect(pairs.length).toBeGreaterThan(10);
  for (const [, reason, code] of pairs) {
    const byReason = explainFailure(t, { error: 'x', reason: reasons.get(reason) })?.line;
    const byCode = explainFailure(t, { error: 'x', errorCode: codes.get(code) })?.line;
    expect(byReason, reason).toBe(byCode);
  }
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
