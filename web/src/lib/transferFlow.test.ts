import { describe, expect, it } from 'vitest';

import type { CatalogueService, DiskReport, DiskVolume, Task } from './api';
import type { Connection } from './connections';
import type { TranslationKey } from './i18n';
import { interpolate } from './interpolate';
import { en } from './locales/en';
import { FLOW_ROWS, density, flowOf, type FlowContext } from './transferFlow';

const MB = 1_000_000;

const t = ((key: TranslationKey, vars?: Record<string, string | number>) => interpolate(en[key], vars)) as Parameters<
  typeof flowOf
>[2];

const task = (over: Partial<Task>): Task =>
  ({
    id: 'a',
    url: 'https://files.example/a.zip',
    name: 'a.zip',
    package: '',
    resolver: 'direct',
    status: 'running',
    size: 100,
    loaded: 0,
    speed: MB,
    enabled: true,
    createdAt: '2026-10-10T10:00:00Z',
    ...over,
  }) as Task;

const volume = (dir: string, role = 'downloads'): DiskVolume => ({
  dir,
  measured: dir,
  exists: true,
  known: true,
  free: 40,
  used: 60,
  total: 100,
  queued: 0,
  tasks: 0,
  role,
});

const disk: DiskReport = { volumes: [volume('/downloads'), volume('/media', 'category')], truncated: false, sampledAt: '' };

const ctx: FlowContext = {
  connections: new Map<string, Connection>([
    ['direct', { id: 'direct', type: 'direct', enabled: true, order: 0 }],
    ['p1', { id: 'p1', type: 'socks5', host: '10.0.0.2', port: 1080, enabled: true, order: 1 }],
  ]),
  services: [{ id: 'realdebrid', label: 'Real-Debrid', kind: 'apiKey', group: 'debrid', whereUrl: '' }] as CatalogueService[],
  disk,
};

const flow = (...tasks: Task[]) => flowOf(tasks, ctx, t);
const names = (nodes: { name: string }[]) => nodes.map((n) => n.name);

describe('the way of a download', () => {
  it('runs from its host over its connection into its folder', () => {
    const f = flow(task({ connection: 'direct' }));
    expect(names(f.sources)).toEqual(['files.example']);
    expect(f.sources[0].host).toBe('files.example');
    expect(f.routes.map((r) => [r.kind, r.name])).toEqual([['direct', 'Direct']]);
    expect(f.folders.map((d) => [d.name, d.volume?.dir])).toEqual([['downloads', '/downloads']]);
    expect(f.links.map((l) => [l.from, l.to, l.speed])).toEqual([
      ['s:files.example', 'r:direct', MB],
      ['r:direct', 'f:/downloads', MB],
    ]);
  });

  it('names a proxy by its address', () => {
    const f = flow(task({ connection: 'p1' }));
    expect(f.routes.map((r) => [r.kind, r.name])).toEqual([['proxy', 'socks5://10.0.0.2:1080']]);
  });

  it('goes through the debrid account that fetches the link, and says which proxy carries that', () => {
    const f = flow(task({ resolver: 'realdebrid#work', host: 'rapidgator.net', connection: 'p1' }));
    expect(names(f.sources)).toEqual(['rapidgator.net']);
    expect(f.routes.map((r) => [r.kind, r.name, r.via])).toEqual([
      ['account', 'Real-Debrid (work)', 'socks5://10.0.0.2:1080'],
    ]);
  });

  it('starts at the account itself when the link has no host of its own', () => {
    const f = flow(task({ resolver: 'realdebrid', url: 'magnet:?xt=urn:btih:abc', host: 'torrent-magnet', connection: 'direct' }));
    expect(f.sources.map((s) => [s.name, s.host])).toEqual([['Real-Debrid', undefined]]);
    expect(f.routes.map((r) => r.kind)).toEqual(['direct']);
  });

  it('draws no way where nothing says what carries the bytes', () => {
    const f = flow(task({ resolver: 'torrent', url: 'magnet:?xt=urn:btih:abc', host: 'torrent-magnet' }));
    expect(names(f.sources)).toEqual(['Peers']);
    expect(f.routes).toEqual([]);
    expect(f.links.map((l) => [l.from, l.to])).toEqual([['s:peers', 'f:/downloads']]);
    expect(f.paths[0].route).toBeUndefined();
  });

  it('lands in the folder the download names, measured by the report row above it', () => {
    const f = flow(task({ dir: '/media/films/2026', connection: 'direct' }));
    expect(f.folders.map((d) => [d.name, d.volume?.dir])).toEqual([['2026', '/media']]);
  });

  it('stands a folder unmeasured where the report has no row for it', () => {
    const f = flow(task({ dir: '/elsewhere', connection: 'direct' }));
    expect(f.folders[0].volume).toBeNull();
  });
});

describe('the lines of the picture', () => {
  it('add up the downloads that share them', () => {
    const f = flow(
      task({ id: 'a', connection: 'direct', speed: MB }),
      task({ id: 'b', connection: 'direct', speed: 2 * MB }),
      task({ id: 'c', url: 'https://other.example/c.zip', connection: 'direct', speed: 4 * MB }),
    );
    expect(f.sources.map((s) => [s.name, s.speed])).toEqual([
      ['other.example', 4 * MB],
      ['files.example', 3 * MB],
    ]);
    expect(f.links.find((l) => l.from === 'r:direct')!.speed).toBe(7 * MB);
    expect(f.speed).toBe(7 * MB);
  });

  it('keep a download that waits or is stalled, at no speed', () => {
    const f = flow(task({ connection: 'p1', speed: 0, stalledSince: '2026-10-10T10:00:00Z' }));
    expect(f.links.map((l) => l.speed)).toEqual([0, 0]);
    expect(f.paths[0].speed).toBe(0);
  });

  it('leave out everything that is not loading', () => {
    const f = flow(task({ status: 'queued' }), task({ status: 'paused' }), task({ status: 'done' }), task({ status: 'extracting' }));
    expect(f.sources).toEqual([]);
    expect(f.links).toEqual([]);
  });

  it('draw the slowest sources as one once a column is full', () => {
    const hosts = ['a.example', 'b.example', 'c.example', 'd.example', 'e.example', 'f.example'];
    const f = flow(...hosts.map((h, i) => task({ id: h, url: `https://${h}/x`, connection: 'direct', speed: (hosts.length - i) * MB })));
    expect(f.sources).toHaveLength(FLOW_ROWS);
    expect(names(f.sources)).toEqual(['a.example', 'b.example', 'c.example', '3 more']);
    expect(f.sources[3].speed).toBe(6 * MB);
    // The three share one line into the connection.
    expect(f.links.filter((l) => l.from === 's:+').map((l) => l.speed)).toEqual([6 * MB]);
    expect(f.paths).toHaveLength(hosts.length);
  });
});

describe('the picture at rest', () => {
  it('keeps the download folder and nothing else', () => {
    const f = flow();
    expect(f.sources).toEqual([]);
    expect(f.links).toEqual([]);
    expect(f.folders.map((d) => d.dir)).toEqual(['/downloads']);
  });

  it('has no folder either until the disk report has arrived', () => {
    expect(flowOf([], { ...ctx, disk: null }, t).folders).toEqual([]);
  });
});

describe('the density of packets', () => {
  it('is none on a line nothing moves on', () => {
    expect(density(0)).toBe(0);
  });

  it('rises with the speed in fixed steps', () => {
    const steps = [64_000, 500_000, 4 * MB, 16 * MB, 64 * MB].map(density);
    expect(steps).toEqual([1, 2, 3, 4, 5]);
  });
});
