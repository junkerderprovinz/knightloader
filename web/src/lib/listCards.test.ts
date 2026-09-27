import { describe, expect, it } from 'vitest';

import type { Task } from './api';
import { packageCard, splitByCard } from './listCards';

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

const ON = { seeding: true, finished: true };

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

  it('puts a package with a torrent still uploading under Seeding', () => {
    expect(packageCard([link('a'), link('b', { seeding: true })])).toBe('seeding');
  });

  it('keeps a seeding torrent beside a download still queued in the download list', () => {
    expect(packageCard([link('a', { seeding: true }), link('b', { status: 'queued' })])).toBe('downloads');
  });

  it('moves a torrent that stopped seeding on to Finished', () => {
    expect(packageCard([link('a', { seeding: false })])).toBe('finished');
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
  });

  it('moves a finished package back to the download list when a link is downloaded again', () => {
    const finished = [link('a'), link('b')];
    expect(packageCard(finished)).toBe('finished');
    expect(packageCard([link('a'), link('b', { status: 'queued', loaded: 0 })])).toBe('downloads');
    expect(packageCard([...finished, link('c', { status: 'queued', loaded: 0 })])).toBe('downloads');
  });

  it('moves a seeding package back to the download list when a link is retried', () => {
    expect(packageCard([link('a', { seeding: true }), link('b', { status: 'running' })])).toBe('downloads');
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
        link('c', { package: 'Upload', seeding: true }),
        link('d', { package: 'Done too' }),
      ]),
      ON,
    );
    expect(split.downloads.map(([n]) => n)).toEqual(['Busy']);
    expect(split.seeding.map(([n]) => n)).toEqual(['Upload']);
    expect(split.finished.map(([n]) => n)).toEqual(['Done', 'Done too']);
  });

  it('places the loose links one by one', () => {
    const split = splitByCard(
      groups([
        link('a', { package: '' }),
        link('b', { package: '', status: 'running' }),
        link('c', { package: '', seeding: true }),
      ]),
      ON,
    );
    expect(split.downloads).toEqual([['', [expect.objectContaining({ id: 'b' })]]]);
    expect(split.seeding).toEqual([['', [expect.objectContaining({ id: 'c' })]]]);
    expect(split.finished).toEqual([['', [expect.objectContaining({ id: 'a' })]]]);
  });

  it('leaves the packages of a switched-off card in the download list', () => {
    const list = groups([link('a', { package: 'Done' }), link('b', { package: 'Upload', seeding: true })]);
    const noFinished = splitByCard(list, { seeding: true, finished: false });
    expect(noFinished.downloads.map(([n]) => n)).toEqual(['Done']);
    expect(noFinished.finished).toEqual([]);
    const noSeeding = splitByCard(list, { seeding: false, finished: true });
    expect(noSeeding.downloads.map(([n]) => n)).toEqual(['Upload']);
    expect(noSeeding.seeding).toEqual([]);
  });
});
