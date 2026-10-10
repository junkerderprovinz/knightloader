import { describe, expect, it } from 'vitest';
import { en } from './locales/en';
import {
  RELEASES,
  bundledNotes,
  isAt,
  recordFor,
  releaseNotesPage,
  releaseOf,
  releaseSince,
  routeOf,
  trail,
  type NewsRecord,
  type Release,
} from './whatsNew';

const sources = import.meta.glob(['../components/**/*.tsx', '../pages/**/*.tsx', '!../**/*.test.tsx'], {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>;

const marked = new Set(
  Object.values(sources).flatMap((text) => [...text.matchAll(/data-new="([^"]+)"/g)].map((m) => m[1])),
);

describe('the list of changes', () => {
  it('names only changes a component is marked with', () => {
    const lost = RELEASES[0].changes.filter((c) => !marked.has(c.id)).map((c) => c.id);
    expect(lost).toEqual([]);
  });

  it('knows every marker a component carries', () => {
    const known = new Set(RELEASES.flatMap((r) => r.changes.map((c) => c.id)));
    expect([...marked].filter((id) => !known.has(id))).toEqual([]);
  });

  it('gives every change an id of its own', () => {
    for (const release of RELEASES) {
      const ids = release.changes.map((c) => c.id);
      expect(new Set(ids).size).toBe(ids.length);
    }
  });

  it('fills every label a sentence quotes', () => {
    for (const change of RELEASES.flatMap((r) => r.changes)) {
      const asked = [...en[change.text].matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort();
      expect(asked, change.id).toEqual(Object.keys(change.names ?? {}).sort());
    }
  });

  it('stands newest first', () => {
    const order = RELEASES.map((r) => r.after);
    const sorted = [...order].sort((a, b) => b.localeCompare(a, undefined, { numeric: true }));
    expect(order).toEqual(sorted);
  });
});

describe('which release an update marks', () => {
  const releases: Release[] = [
    { after: '1.9.0', changes: [] },
    { after: '1.8.4', changes: [] },
  ];
  const marks = (from: string, running: string) => releaseSince(from, running, releases)?.after;

  it('marks what the release after the one left brought', () => {
    expect(marks('v1.8.4', 'v1.9.0')).toBe('1.8.4');
  });

  it('marks only the newest of several releases an update passes', () => {
    expect(marks('v1.8.4', 'v2.0.0')).toBe('1.9.0');
  });

  it('marks nothing on a release that brought no changes of its own', () => {
    expect(marks('v1.9.1', 'v1.9.2')).toBeUndefined();
  });

  it('marks nothing after going back to an older release', () => {
    expect(marks('v2.0.0', 'v1.9.0')).toBeUndefined();
  });

  it('takes a build without a release number for what comes next', () => {
    expect(marks('v1.8.4', 'dev')).toBe('1.9.0');
  });

  it('takes an unknown earlier version for older than every release', () => {
    expect(marks('', 'v1.9.0')).toBe('1.8.4');
  });
});

describe('the record of the running version', () => {
  it('marks nothing on a first install and leaves the release notes closed', () => {
    expect(recordFor(undefined, 'v1.9.0', false)).toEqual({
      version: 'v1.9.0',
      after: '',
      seen: [],
      notes: true,
      dots: true,
    });
  });

  it('marks the changes on an instance that was in use before it kept a record', () => {
    const record = recordFor(undefined, 'v1.9.0', true);
    expect(record.after).toBe('1.8.4');
    expect(record.notes).toBe(false);
  });

  it('keeps what was seen while the version stays', () => {
    const stored: NewsRecord = { version: 'v1.9.0', after: '1.8.4', seen: ['seed-ring'], notes: true, dots: false };
    expect(recordFor(stored, 'v1.9.0', true)).toBe(stored);
  });

  it('starts over with the next version', () => {
    const stored: NewsRecord = { version: 'v1.8.4', after: '', seen: ['x'], notes: true, dots: false };
    expect(recordFor(stored, 'v1.9.0', false)).toEqual({
      version: 'v1.9.0',
      after: '1.8.4',
      seen: [],
      notes: false,
      dots: true,
    });
  });
});

describe('the way to a change', () => {
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
    expect(isAt({ id: 'x', to: ['/settings/schedule'], text: 'whatsnew.title' }, '/settings/automation')).toBe(true);
  });
});

describe('the release notes', () => {
  const bundle = { version: '1.9.0', text: '\n## Added\n\n- A thing.\n' };

  it('are the ones the build carries for the running release', () => {
    expect(bundledNotes('v1.9.0', bundle)).toBe('## Added\n\n- A thing.');
    expect(bundledNotes('v1.9.0+main.abc', bundle)).toBe('## Added\n\n- A thing.');
  });

  it('are missing for another release and for a build without a release number', () => {
    expect(bundledNotes('v1.9.1', bundle)).toBe('');
    expect(bundledNotes('dev', bundle)).toBe('');
  });

  it('are linked by tag, or to every release for a build without one', () => {
    expect(releaseNotesPage('v1.9.0')).toBe('https://github.com/junkerderprovinz/knightloader/releases/tag/v1.9.0');
    expect(releaseNotesPage('dev')).toBe('https://github.com/junkerderprovinz/knightloader/releases');
    expect(releaseOf('1.9.0')).toBe('1.9.0');
  });
});
