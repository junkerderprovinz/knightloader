import { describe, expect, it } from 'vitest';
import {
  checkButton,
  componentRows,
  numberLink,
  timeAgo,
  updateAction,
  updateLine,
  whatsNewLabel,
  type VersionCardText,
} from './versionCards';

const T = {
  whatsNew: "What's new",
  count: (n: number) => String(n),
  current: 'Up to date',
  checked: () => 'Last checked now',
  checking: 'Checking …',
  available: (v: string) => `Update to ${v} available`,
  failed: 'Check failed',
  downloading: (p: number) => `Downloading … ${p}%`,
  ready: (v: string) => `${v} is ready`,
  update: 'Update now',
  restart: (v: string) => `Restart for ${v}`,
  guide: 'How to update',
  store: (s: string) => `Updates come through ${s}.`,
} as unknown as VersionCardText;

describe('the Version card logic', () => {
  it('offers the one action only once a new version is there, following the route', () => {
    expect(updateAction('install', { kind: 'current' }, T)).toBeNull();
    expect(updateAction('install', { kind: 'available', version: '2.0.0' }, T)?.label).toBe('Update now');
    expect(updateAction('guide', { kind: 'available', version: '2.0.0' }, T)?.label).toBe('How to update');
    expect(updateAction('store', { kind: 'available', version: '2.0.0' }, T)).toBeNull();
    expect(updateAction('install', { kind: 'ready', version: '2.0.0' }, T)?.label).toBe('Restart for 2.0.0');
    expect(updateAction('guide', { kind: 'ready', version: '2.0.0' }, T)).toBeNull();
  });

  it('drops the check once there is nothing left to look for', () => {
    expect(checkButton('install', { kind: 'unchecked' })).toEqual({ busy: false });
    expect(checkButton('guide', { kind: 'checking' })).toEqual({ busy: true });
    expect(checkButton('install', { kind: 'available', version: '2.0.0' })).toBeNull();
    expect(checkButton('store', { kind: 'current' })).toBeNull();
  });

  it('says nothing before the first check', () => {
    expect(updateLine({ kind: 'unchecked' }, T).line).toBe('');
    expect(updateLine({ kind: 'failed', reason: 'offline' }, T)).toEqual({ line: 'Check failed', tone: 'fail', note: 'offline' });
  });

  it('counts the unseen changes after the word', () => {
    expect(whatsNewLabel(T, 0)).toBe("What's new");
    expect(whatsNewLabel(T, 3)).toBe("What's new · 3");
  });

  it('says a time before now in the language', () => {
    const now = new Date('2026-11-21T12:00:00Z');
    expect(timeAgo(new Date('2026-11-21T11:57:00Z'), 'en', now)).toBe('3 minutes ago');
    expect(timeAgo(new Date('2026-11-21T11:59:50Z'), 'de', now)).toBe('jetzt');
  });
});

describe('the Components card logic', () => {
  it('sorts parts first, then the programs by name, the design language and the installer', () => {
    const rows = componentRows([
      { name: 'PommelPorter', version: '1.2.5', kind: 'installer' },
      { name: 'yt-dlp', version: '1' },
      { name: 'GlimStone', version: '4.8.2', kind: 'design' },
      { name: 'Deno', version: '2' },
    ]);
    expect(rows.map((r) => r.name)).toEqual(['Deno', 'yt-dlp', 'GlimStone', 'PommelPorter']);
  });

  it('leads a release to the newest one and a stamp nowhere', () => {
    expect(numberLink({ version: 'v2.9.4', repoUrl: 'https://x/y' })).toEqual({ shown: '2.9.4', released: true, href: 'https://x/y/releases/latest' });
    expect(numberLink({ version: 'dev', repoUrl: 'https://x/y' })).toEqual({ shown: 'dev', released: false });
    expect(numberLink({ version: '50123', releasePage: 'https://jdownloader.org/' }).href).toBe('https://jdownloader.org/');
  });
});
