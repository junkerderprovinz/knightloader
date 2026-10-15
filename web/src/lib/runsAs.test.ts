// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';

import { idleProblemText } from '../components/IdleActionBanner';
import type { FileOwnerIdentity } from './api';
import type { TranslationKey } from './i18n';
import { interpolate } from './interpolate';
import { en } from './locales/en';
import { accountOf, withRunsAs } from './runsAs';
import { adviceFor, adviceNamesAccount } from './startupAdvice';

const english = (key: TranslationKey, vars?: Record<string, string | number>) => interpolate(en[key], vars);

const readout = (over: Partial<FileOwnerIdentity>): FileOwnerIdentity => ({
  known: true,
  deployment: 'container',
  uid: 99,
  gid: 100,
  user: '',
  group: 'users',
  umask: '0000',
  umaskKnown: true,
  umaskApplied: true,
  env: { puid: '', pgid: '', umask: '000' },
  idsRead: false,
  ...over,
});

describe('the account a hint names', () => {
  it('is the uid and gid the readout reports', () => {
    expect(accountOf(readout({}))).toEqual({ uid: 99, gid: 100 });
  });

  it('is absent where files have no owners, and for root', () => {
    expect(accountOf(readout({ known: false, uid: 0, gid: 0 }))).toBeUndefined();
    expect(accountOf(readout({ uid: 0, gid: 0 }))).toBeUndefined();
  });

  it('is added to a hint, and a hint without one stays as it is', () => {
    expect(withRunsAs(english, 'Hint.', { uid: 99, gid: 100 })).toBe(
      'Hint. KnightLoader runs as uid 99 and gid 100, not as root.',
    );
    expect(withRunsAs(english, 'Hint.', undefined)).toBe('Hint.');
  });
});

describe('the hints that depend on who the process is', () => {
  const runsAs = { uid: 500, gid: 501 };

  it('name the account for a program that cannot be run or is refused', () => {
    for (const problem of ['notExecutable', 'permission']) {
      const text = idleProblemText(english, problem, { program: '/config/sleep.sh', runsAs });
      expect(text, problem).toContain('uid 500 and gid 501');
    }
  });

  it('leave the account out where it decides nothing', () => {
    expect(idleProblemText(english, 'timeout', { seconds: 5, runsAs })).not.toContain('uid');
    expect(idleProblemText(english, 'permission', { action: 'suspend', runsAs })).not.toContain('uid');
  });

  it('name the account for a folder the instance may not write', () => {
    const key = adviceFor('folder/downloads', 'denied');
    expect(key).toBe('settings.diagnostics.fix.denied');
    expect(adviceNamesAccount(key!)).toBe(true);
    expect(adviceNamesAccount('settings.diagnostics.fix.full')).toBe(false);
  });
});

// Every language, since a translation can keep a number the English has lost.
const locales = import.meta.glob('./locales/*.ts', { import: 'default', eager: true, query: '?raw' }) as Record<
  string,
  string
>;

describe('no hint guesses the account', () => {
  it('carries no fixed uid in any language', () => {
    const files = Object.entries(locales).filter(([path]) => !path.endsWith('/index.ts'));
    expect(files.length).toBeGreaterThan(40);
    for (const [path, src] of files) {
      expect(src, path).not.toMatch(/uid\s*1000/);
    }
  });
});
