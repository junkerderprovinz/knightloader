import { describe, expect, it } from 'vitest';
import { en } from './locales/en';
import {
  CHANGES,
  RELEASES,
  bundledNotes,
  changesOf,
  isAt,
  newer,
  placed,
  recordFor,
  releaseNotesPage,
  releaseOf,
  releasesShown,
  routeOf,
  trail,
  type NewsRecord,
  type PlacedChange,
} from './whatsNew';

const sources = import.meta.glob(['../components/**/*.tsx', '../pages/**/*.tsx', '!../**/*.test.tsx'], {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>;

const marked = new Set(
  Object.values(sources).flatMap((text) => [...text.matchAll(/data-new="([^"]+)"/g)].map((m) => m[1])),
);

const ids = (version: string) => CHANGES.filter((c) => c.since === version).map((c) => c.id);
const upTo = (version: string) => CHANGES.filter((c) => !newer(c.since, version)).map((c) => c.id);

describe('the list of changes', () => {
  it('names only changes a component is marked with', () => {
    const lost = CHANGES.filter(placed)
      .filter((c) => !marked.has(c.id))
      .map((c) => c.id);
    expect(lost).toEqual([]);
  });

  it('knows every marker a component carries', () => {
    const known = new Set(CHANGES.filter(placed).map((c) => c.id));
    expect([...marked].filter((id) => !known.has(id))).toEqual([]);
  });

  it('gives every change an id of its own', () => {
    const all = CHANGES.map((c) => c.id);
    expect(new Set(all).size).toBe(all.length);
  });

  it('fills every label a sentence quotes', () => {
    for (const change of CHANGES) {
      const asked = [...en[change.text].matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort();
      expect(asked, change.id).toEqual(Object.keys(change.names ?? {}).sort());
    }
  });

  it('says each change in a sentence', () => {
    for (const change of CHANGES) expect(en[change.text], change.id).toMatch(/\.$/);
  });

  it('names each release once, newest first', () => {
    const versions = RELEASES.map((r) => r.version);
    for (const version of versions) expect(version).toMatch(/^\d+\.\d+\.\d+$/);
    expect(versions.every((v, i) => i === 0 || newer(versions[i - 1], v))).toBe(true);
  });

  // From 2.1.0 on, each feature a release adds under Added gets a dot. A patch
  // release without an entry of its own is folded into the next release that
  // has one, and its features count there.
  it('gives every feature a release adds a change of its own', () => {
    const added = new Map<string, number>();
    for (const [version, text] of Object.entries(__RELEASE_NOTES__)) {
      if (newer('2.1.0', version)) continue;
      const home = [...RELEASES].reverse().find((r) => !newer(version, r.version))?.version ?? version;
      const list = /^## .*Added\s*\n([\s\S]*?)(?=^## |(?![\s\S]))/m.exec(text)?.[1] ?? '';
      const features = list.split('\n').filter((l) => l.startsWith('- ')).length;
      added.set(home, (added.get(home) ?? 0) + features);
    }
    const short = [...added]
      .filter(([version, features]) => ids(version).length < features)
      .map(([version, features]) => `${version}: ${features} added, ${ids(version).length} listed`);
    expect(Object.keys(__RELEASE_NOTES__)).toContain('2.1.0');
    expect(short).toEqual([]);
  });
});

describe('the record', () => {
  it('marks nothing on a first install and leaves the window closed', () => {
    const record = recordFor(undefined, 'v2.2.0', false);
    expect(record).toMatchObject({ version: 'v2.2.0', from: '', notes: true });
    expect(record.seen).toEqual(CHANGES.map((c) => c.id));
  });

  it('marks every change up to the running release on an instance that was in use before it kept a record', () => {
    const record = recordFor(undefined, 'v2.1.0', true);
    expect(record).toMatchObject({ from: '', seen: [], notes: false });
    expect(changesOf(record).map((c) => c.id)).toEqual(upTo('2.1.0'));
  });

  it('keeps itself while the version stays', () => {
    const stored: NewsRecord = { version: 'v2.1.0', from: '2.0.0', seen: ['vpn'], notes: true };
    expect(recordFor(stored, 'v2.1.0', true)).toBe(stored);
  });

  it('marks every change after the release left, and keeps what was seen before it', () => {
    const stored: NewsRecord = { version: 'v2.1.0', from: '2.0.0', seen: ['vpn', 'overview-head'], notes: true };
    expect(recordFor(stored, 'v2.2.0', true)).toEqual({ version: 'v2.2.0', from: '2.1.0', seen: ['vpn', 'overview-head'], notes: false });
  });

  it('keeps a change seen on a patch release that a later release took in', () => {
    const stored: NewsRecord = { version: 'v2.1.6', from: '2.1.5', seen: ['folder-back', 'vpn'], notes: true };
    const record = recordFor(stored, 'v2.2.0', true);
    expect(record.seen).toEqual(['folder-back', 'vpn']);
    const unseen = changesOf(record).filter((c) => !record.seen.includes(c.id)).map((c) => c.id);
    expect(unseen).not.toContain('folder-back');
  });

  it('keeps what was seen on a build without a release number', () => {
    const stored: NewsRecord = { version: 'preview-59b73a6', from: '', seen: ['quiet-waive'], notes: true };
    expect(recordFor(stored, 'v2.2.0', true)).toMatchObject({ from: '', seen: ['quiet-waive'] });
  });

  it('takes over the record of one release that older versions kept', () => {
    const old = { version: 'v2.1.0', after: '2.0.0', seen: ['vpn-routes'], notes: true };
    const record = recordFor(old, 'v2.2.0', true);
    expect(record.from).toBe('2.1.0');
    const unseen = changesOf(record).filter((c) => !record.seen.includes(c.id)).map((c) => c.id);
    expect(unseen).toEqual([...ids('2.2.0'), ...ids('2.1.0').filter((id) => id !== 'vpn-routes')]);
  });

  it('leaves out what came after the running release', () => {
    const record: NewsRecord = { version: 'v2.1.0', from: '2.0.0', seen: [], notes: true };
    const shown = changesOf(record).map((c) => c.id);
    expect(shown).toEqual(upTo('2.1.0'));
    expect(shown).not.toContain('folder-back');
  });
});

describe('the releases the window shows', () => {
  const notes = { '2.1.4': '', '2.1.3': '', '2.1.2': '', '2.1.1': '', '2.1.0': '', '2.0.0': '' };
  const record = (version: string, from: string): NewsRecord => ({ version, from, seen: [], notes: true });

  it('are every release since the one left, up to the running one', () => {
    expect(releasesShown(record('v2.1.3', '2.1.0'), [], notes)).toEqual(['2.1.3', '2.1.2', '2.1.1']);
  });

  it('include a release with a change still unseen', () => {
    const vpn = CHANGES.find((c) => c.id === 'vpn')!;
    expect(releasesShown(record('v2.1.3', '2.1.2'), [vpn], notes)).toEqual(['2.1.3', '2.1.0']);
  });

  it('are the running release alone when no earlier one is known', () => {
    expect(releasesShown(record('v2.1.3', ''), [], notes)).toEqual(['2.1.3']);
  });

  it('take the catalog for a release this build has no notes for', () => {
    expect(releasesShown(record('v2.2.0', '2.0.0'), [], {})).toEqual(['2.2.0', '2.1.0']);
  });
});

describe('the way to a change', () => {
  const at = (to: string[]): PlacedChange => ({ id: 'x', to, text: 'whatsnew.title', since: '2.1.0' });

  it('leads over the section and the page', () => {
    expect(trail('/settings/pairing', '/downloads')).toEqual(['/settings', '/settings/pairing']);
  });

  it('leaves out the section the reader is already in', () => {
    expect(trail('/settings/pairing', '/settings/look')).toEqual(['/settings/pairing']);
  });

  it('is empty on the page itself', () => {
    expect(trail('/downloads', '/downloads')).toEqual([]);
    expect(trail('/', '/')).toEqual([]);
  });

  it('leads to the overview from any other page', () => {
    expect(trail('/', '/collector')).toEqual(['/']);
  });

  it('goes to the page a folded settings page is drawn on', () => {
    expect(routeOf('/settings/schedule')).toBe('/settings/automation');
    expect(trail('/settings/reconnect', '/')).toEqual(['/settings', '/settings/network']);
    expect(isAt(at(['/settings/schedule']), '/settings/automation')).toBe(true);
  });

  it('takes a section for every page in it', () => {
    const search = at(['/settings']);
    expect(isAt(search, '/settings/look')).toBe(true);
    expect(isAt(search, '/downloads')).toBe(false);
    expect(trail('/settings', '/downloads')).toEqual(['/settings']);
    expect(trail('/settings', '/settings/look')).toEqual([]);
  });

  it('does not take the overview for a section', () => {
    expect(isAt(at(['/']), '/downloads')).toBe(false);
  });
});

describe('the release notes', () => {
  const bundle = { '2.1.0': '\n## Added\n\n- A thing.\n', '2.0.0': 'Older.' };

  it('are the ones the build carries for a release', () => {
    expect(bundledNotes('v2.1.0', bundle)).toBe('## Added\n\n- A thing.');
    expect(bundledNotes('v2.1.0+main.abc', bundle)).toBe('## Added\n\n- A thing.');
    expect(bundledNotes('2.0.0', bundle)).toBe('Older.');
  });

  it('are missing for a release the build does not know and for a build without a release number', () => {
    expect(bundledNotes('v2.1.1', bundle)).toBe('');
    expect(bundledNotes('dev', bundle)).toBe('');
  });

  it('are linked by tag, or to every release for a build without one', () => {
    expect(releaseNotesPage('v1.9.0')).toBe('https://github.com/junkerderprovinz/knightloader/releases/tag/v1.9.0');
    expect(releaseNotesPage('dev')).toBe('https://github.com/junkerderprovinz/knightloader/releases');
    expect(releaseOf('1.9.0')).toBe('1.9.0');
  });
});
