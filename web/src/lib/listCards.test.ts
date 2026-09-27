import { describe, expect, it } from 'vitest';

import type { Task } from './api';
import { packageCard, seedingOff, seedingOn, splitByCard } from './listCards';

const link = (id: string, over: Partial<Task> = {}): Task => ({
  id,
  url: `https://files.example/${id}`,
  name: `${id}.mkv`,
  package: 'Sintel',
  resolver: 'direct',
  size: 1000,
  loaded: 1000,
  speed: 0,
  status: 'done',
  createdAt: '2026-09-24T12:00:00Z',
  priority: 0,
  position: 0,
  enabled: true,
  ...over,
});

const ON = { finished: true, torrents: true };

const torrent = (id: string, over: Partial<Task> = {}): Task => link(id, { resolver: 'torrent', ...over });

describe('packageCard', () => {
  it('puts a package whose links have all downloaded under Finished', () => {
    expect(packageCard([link('a'), link('b', { unpack: 'done' })])).toBe('finished');
  });

  it('keeps a package with a link still to download in the download list', () => {
    for (const status of ['queued', 'running', 'paused'] as const) {
      expect(packageCard([link('a'), link('b', { status })])).toBe('downloads');
    }
  });

  it('keeps a package in the download list while an archive in it waits or unpacks', () => {
    expect(packageCard([link('a'), link('b', { status: 'extracting' })])).toBe('downloads');
  });

  it('keeps a package whose archive did not unpack in the download list', () => {
    expect(packageCard([link('a', { unpack: 'error' }), link('b')])).toBe('downloads');
    expect(packageCard([link('a', { unpack: 'password' })])).toBe('downloads');
  });

  it('keeps a package with a failed link in the download list', () => {
    expect(packageCard([link('a'), link('b', { status: 'error' })])).toBe('downloads');
  });

  it('puts a finished package holding a torrent under Torrents', () => {
    expect(packageCard([link('a'), torrent('b', { seeding: true })])).toBe('torrents');
  });

  it('keeps a torrent under Torrents once it stopped seeding', () => {
    expect(packageCard([torrent('a', { seeding: false })])).toBe('torrents');
  });

  it('keeps a torrent beside a download still queued in the download list', () => {
    expect(packageCard([torrent('a', { seeding: true }), link('b', { status: 'queued' })])).toBe('downloads');
  });

  it('keeps a torrent still downloading in the download list', () => {
    expect(packageCard([torrent('a', { status: 'running', loaded: 500 })])).toBe('downloads');
  });

  it('treats a torrent a debrid service fetched as an ordinary link', () => {
    expect(packageCard([link('a', { resolver: 'realdebrid' })])).toBe('finished');
  });

  it('does not let a switched-off link that never downloaded hold a package back', () => {
    const parked = link('b', { enabled: false, status: 'queued', loaded: 0 });
    expect(packageCard([link('a'), parked])).toBe('finished');
    expect(packageCard([link('a'), { ...parked, status: 'error' }])).toBe('finished');
  });

  it('lets a switched-off link that is still downloading hold a package back', () => {
    expect(packageCard([link('a'), link('b', { enabled: false, status: 'running' })])).toBe('downloads');
  });

  it('keeps a package whose links are all switched off in the download list', () => {
    expect(packageCard([link('a', { enabled: false, status: 'queued' })])).toBe('downloads');
    expect(packageCard([link('a', { enabled: false }), link('b', { enabled: false, status: 'queued' })])).toBe('downloads');
  });

  it('moves a finished package back to the download list when a link is downloaded again', () => {
    const finished = [link('a'), link('b')];
    expect(packageCard(finished)).toBe('finished');
    expect(packageCard([link('a'), link('b', { status: 'queued', loaded: 0 })])).toBe('downloads');
    expect(packageCard([...finished, link('c', { status: 'queued', loaded: 0 })])).toBe('downloads');
  });

  it('moves a torrent back to the download list when it is downloaded again', () => {
    expect(packageCard([torrent('a')])).toBe('torrents');
    expect(packageCard([torrent('a', { status: 'queued', loaded: 0 })])).toBe('downloads');
  });
});

describe('splitByCard', () => {
  const groups = (list: Task[]): [string, Task[]][] => {
    const m = new Map<string, Task[]>();
    for (const x of list) m.set(x.package, [...(m.get(x.package) ?? []), x]);
    return [...m.entries()];
  };

  it('sorts packages into the three cards, in list order', () => {
    const split = splitByCard(
      groups([
        link('a', { package: 'Done' }),
        link('b', { package: 'Busy', status: 'running' }),
        torrent('c', { package: 'Upload', seeding: true }),
        link('d', { package: 'Done too' }),
        torrent('e', { package: 'Seeded' }),
      ]),
      ON,
    );
    expect(split.downloads.map(([n]) => n)).toEqual(['Busy']);
    expect(split.finished.map(([n]) => n)).toEqual(['Done', 'Done too']);
    expect(split.torrents.map(([n]) => n)).toEqual(['Upload', 'Seeded']);
  });

  it('places the loose links one by one', () => {
    const split = splitByCard(
      groups([
        link('a', { package: '' }),
        link('b', { package: '', status: 'running' }),
        torrent('c', { package: '' }),
      ]),
      ON,
    );
    expect(split.downloads).toEqual([['', [expect.objectContaining({ id: 'b' })]]]);
    expect(split.finished).toEqual([['', [expect.objectContaining({ id: 'a' })]]]);
    expect(split.torrents).toEqual([['', [expect.objectContaining({ id: 'c' })]]]);
  });

  it('leaves the packages of a switched-off Finished card in the download list', () => {
    const list = groups([link('a', { package: 'Done' }), torrent('b', { package: 'Upload', seeding: true })]);
    const noFinished = splitByCard(list, { finished: false, torrents: true });
    expect(noFinished.downloads.map(([n]) => n)).toEqual(['Done']);
    expect(noFinished.finished).toEqual([]);
    expect(noFinished.torrents.map(([n]) => n)).toEqual(['Upload']);
  });

  it('files torrents under Finished while the Torrents card is off', () => {
    const list = groups([link('a', { package: 'Done' }), torrent('b', { package: 'Upload', seeding: true })]);
    const noTorrents = splitByCard(list, { finished: true, torrents: false });
    expect(noTorrents.finished.map(([n]) => n)).toEqual(['Done', 'Upload']);
    expect(noTorrents.torrents).toEqual([]);
    const neither = splitByCard(list, { finished: false, torrents: false });
    expect(neither.downloads.map(([n]) => n)).toEqual(['Done', 'Upload']);
  });
});

describe('seeding verbs', () => {
  it('offers the stop for a finished torrent that seeds or is about to, and the start once its seeding is over', () => {
    expect([seedingOn, seedingOff].map((f) => f(torrent('a', { seeding: true })))).toEqual([true, false]);
    expect([seedingOn, seedingOff].map((f) => f(torrent('b')))).toEqual([true, false]);
    expect([seedingOn, seedingOff].map((f) => f(torrent('c', { seedingOver: true })))).toEqual([false, true]);
  });

  it('offers neither for a torrent still downloading or a download that is not a torrent', () => {
    for (const x of [torrent('a', { status: 'running' }), link('b'), link('c', { resolver: 'realdebrid' })]) {
      expect(seedingOn(x) || seedingOff(x)).toBe(false);
    }
  });
});
