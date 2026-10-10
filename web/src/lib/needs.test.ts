import { describe, expect, it } from 'vitest';

import type { Account, CaptchaChallenge, DiskVolume, HealthReport, HosterLogin, Task, VolumeUsage } from './api';
import { headState, kindSegments, needsOf, queueFigures, type NeedSources } from './needs';

const NOW = Date.parse('2026-10-10T12:00:00Z');
const DAY = 24 * 60 * 60 * 1000;
const GO_ZERO = '0001-01-01T00:00:00Z';

const task = (over: Partial<Task>): Task =>
  ({
    id: 'a',
    url: 'https://files.example/a.zip',
    name: 'a.zip',
    package: '',
    resolver: 'http',
    status: 'queued',
    size: 100,
    loaded: 0,
    speed: 0,
    enabled: true,
    nextTry: GO_ZERO,
    createdAt: '2026-10-10T10:00:00Z',
    ...over,
  }) as Task;

const volume = (over: Partial<DiskVolume>): DiskVolume => ({
  dir: '/downloads',
  measured: '/downloads',
  exists: true,
  known: true,
  free: 50,
  used: 50,
  total: 100,
  queued: 0,
  tasks: 0,
  role: 'downloads',
  ...over,
});

const health = (over: Partial<HealthReport>): HealthReport =>
  ({ status: 'ok', subsystems: [], volumes: [], halted: false, quiet: false, ...over }) as HealthReport;

const sources = (over: Partial<NeedSources>): NeedSources => ({
  tasks: [],
  captchas: [],
  accounts: [],
  services: [],
  logins: [],
  health: null,
  volume: null,
  diskLow: 0,
  diskStop: 0,
  done: new Set(),
  now: NOW,
  ...over,
});

const kinds = (src: Partial<NeedSources>) => needsOf(sources(src)).map((n) => n.kind);

describe('what needs the person', () => {
  it('lists a download that failed for good and leaves one with a retry coming alone', () => {
    const tasks = [
      task({ id: 'dead', status: 'error', gaveUp: true }),
      task({ id: 'spent', status: 'error', retries: 3 }),
      task({ id: 'soon', status: 'error', retries: 1, nextTry: new Date(NOW + 60_000).toISOString() }),
      task({ id: 'off', status: 'error', enabled: false }),
    ];
    const needs = needsOf(sources({ tasks }));
    expect(needs.map((n) => n.key)).toEqual(['failed:dead', 'failed:spent']);
  });

  it('puts the failed links of one package on one row', () => {
    const tasks = [
      task({ id: '1', package: 'Season 1', status: 'error' }),
      task({ id: '2', package: 'Season 1', status: 'error' }),
      task({ id: '3', package: 'Season 2', status: 'error' }),
      task({ id: '4', status: 'error' }),
      task({ id: '5', status: 'error' }),
    ];
    const needs = needsOf(sources({ tasks }));
    expect(needs.map((n) => (n.kind === 'failed' ? n.tasks.map((t) => t.id) : []))).toEqual([['1', '2'], ['3'], ['4'], ['5']]);
  });

  it('lists an archive that did not unpack, and says when it wants a password', () => {
    const needs = needsOf(sources({ tasks: [task({ status: 'done', unpack: 'password' }), task({ id: 'b', status: 'done' })] }));
    expect(needs).toHaveLength(1);
    expect(needs[0]).toMatchObject({ kind: 'unpack', password: true, tone: 'fail' });
  });

  it('drops what was marked as done, link by link', () => {
    const tasks = [task({ id: '1', package: 'P', status: 'error' }), task({ id: '2', package: 'P', status: 'error' })];
    const one = needsOf(sources({ tasks, done: new Set(['task:1']) }));
    expect(one.map((n) => (n.kind === 'failed' ? n.tasks.map((t) => t.id) : []))).toEqual([['2']]);
    expect(needsOf(sources({ tasks, done: new Set(['task:1', 'task:2']) }))).toEqual([]);
  });

  it('puts a waiting captcha first and leaves a test captcha out', () => {
    const captcha = (over: Partial<CaptchaChallenge>): CaptchaChallenge =>
      ({ id: 'c', source: 'jd', host: 'files.example', kind: 'image', expiresAt: GO_ZERO, ...over }) as CaptchaChallenge;
    const src = {
      tasks: [task({ status: 'error' })],
      captchas: [captcha({}), captcha({ id: 't', test: true })],
    };
    expect(needsOf(sources(src)).map((n) => n.key)).toEqual(['captcha:c', 'failed:a']);
    expect(needsOf(sources(src))[0].covers).toEqual([]);
  });

  it('warns below the start floor and fails below the stop floor', () => {
    const report = health({
      volumes: [
        volume({ dir: '/low', free: 8 }),
        volume({ dir: '/full', free: 2 }),
        volume({ dir: '/full', free: 2, role: 'category' }),
        volume({ dir: '/fine', free: 50 }),
        volume({ dir: '/unknown', free: 0, known: false }),
      ],
    });
    const needs = needsOf(sources({ health: report, diskLow: 10, diskStop: 5 }));
    expect(needs.map((n) => [n.key, n.tone])).toEqual([
      ['disk:stop:/full', 'fail'],
      ['disk:low:/low', 'warn'],
    ]);
  });

  it('measures no folder against a floor that is not set', () => {
    expect(needsOf(sources({ health: health({ volumes: [volume({ free: 0 })] }) }))).toEqual([]);
  });

  it('lists an account that stopped working, and one that is about to end', () => {
    const account = (over: Partial<Account>): Account =>
      ({ id: 'x', service: 'realdebrid', account: '', label: '', enabled: true, configured: true, ok: true, detail: 'ok', ...over }) as Account;
    const accounts = [
      account({ id: 'bad', ok: false, detail: 'bad token' }),
      account({ id: 'new', ok: false, detail: '' }),
      account({ id: 'off', ok: false, detail: 'bad token', enabled: false }),
      account({ id: 'ends', expiry: new Date(NOW + 3 * DAY).toISOString() }),
      account({ id: 'later', expiry: new Date(NOW + 30 * DAY).toISOString() }),
    ];
    const services = [{ id: 'realdebrid', label: 'Real-Debrid', kind: 'apiKey' as const, group: 'debrid' as const, whereUrl: 'https://real-debrid.example' }];
    const needs = needsOf(sources({ accounts, services }));
    expect(needs.map((n) => n.kind)).toEqual(['account', 'expiry']);
    expect(needs[0]).toMatchObject({ name: 'Real-Debrid', detail: 'bad token', tone: 'fail' });
    expect(needs[1]).toMatchObject({ name: 'Real-Debrid', renewUrl: 'https://real-debrid.example', tone: 'warn' });
  });

  it('lists a login the hoster rejected, but not one still being checked', () => {
    const login = (over: Partial<HosterLogin>): HosterLogin => ({ host: 'h.example', username: 'u', status: 'active', enabled: true, ...over });
    const logins = [login({ host: 'no.example', status: 'rejected' }), login({ host: 'wait.example', status: 'queued' })];
    expect(needsOf(sources({ logins })).map((n) => n.key)).toEqual(['login:no.example']);
  });

  it('lists a reached volume cap until the counter starts over', () => {
    const usage: VolumeUsage = { used: 10, cap: 10, action: 'pause', throttle: 0, periodStart: '2026-10-01', periodEnd: '2026-11-01', reached: true };
    expect(kinds({ volume: usage })).toEqual(['volumeCap']);
    expect(kinds({ volume: { ...usage, reached: false } })).toEqual([]);
    expect(kinds({ volume: usage, done: new Set(['volumeCap:2026-10-01']) })).toEqual([]);
    expect(kinds({ volume: { ...usage, periodStart: '2026-11-01' }, done: new Set(['volumeCap:2026-10-01']) })).toEqual(['volumeCap']);
  });

  it('lists a part of the instance that is down with a remedy, once', () => {
    const report = health({
      subsystems: [
        { id: 'store', state: 'ok' },
        { id: 'queue', state: 'degraded' },
        { id: 'disk', state: 'degraded', remedy: 'disk.low' },
        { id: 'jd', state: 'failed', remedy: 'jd.unreachable' },
        { id: 'ytdlp', state: 'unused', remedy: 'ytdlp.missing' },
        { id: 'relay', state: 'degraded', remedy: 'relay.down' },
        { id: 'captcha', state: 'degraded', remedy: 'captcha.waiting' },
      ],
    });
    expect(needsOf(sources({ health: report })).map((n) => [n.key, n.tone])).toEqual([
      ['health:jd:jd.unreachable', 'fail'],
      ['health:relay:relay.down', 'warn'],
    ]);
  });
});

describe('the ring', () => {
  it('has one segment per backend, coloured by the worst of its downloads', () => {
    const tasks = [
      task({ id: '1', resolver: 'jd', status: 'done' }),
      task({ id: '2', resolver: 'jd', status: 'running' }),
      task({ id: '3', resolver: 'torrent', status: 'done' }),
      task({ id: '4', resolver: 'ytdlp', status: 'error', gaveUp: true }),
      task({ id: '5', resolver: 'ytdlp', status: 'running' }),
      task({ id: '6', resolver: 'realdebrid#main', status: 'queued', waiting: 'captcha' }),
      task({ id: '7', resolver: 'http', status: 'queued', waiting: 'slot' }),
      task({ id: '8', resolver: 'nntp', status: 'collected' }),
    ];
    expect(kindSegments(tasks, new Set(), NOW)).toEqual([
      { id: 'http', tone: 'neutral', held: undefined },
      { id: 'jd', tone: 'run', held: undefined },
      { id: 'realdebrid', tone: 'warn', held: 'captcha' },
      { id: 'torrent', tone: 'ok', held: undefined },
      { id: 'ytdlp', tone: 'fail', held: undefined },
    ]);
  });

  it('is green for a kind only once everything of it is loaded', () => {
    const tasks = [task({ id: '1', status: 'done' }), task({ id: '2', status: 'queued' })];
    expect(kindSegments(tasks, new Set(), NOW)[0].tone).toBe('neutral');
  });

  it('counts a failure marked as done as settled', () => {
    const tasks = [task({ id: '1', status: 'error' }), task({ id: '2', status: 'done' })];
    expect(kindSegments(tasks, new Set(), NOW)[0].tone).toBe('fail');
    expect(kindSegments(tasks, new Set(['task:1']), NOW)[0].tone).toBe('ok');
  });
});

describe('the queue in figures', () => {
  it('counts what runs and what waits, with the time the open queue takes', () => {
    const tasks = [
      task({ id: '1', status: 'running', size: 1000, loaded: 400, speed: 100 }),
      task({ id: '2', status: 'queued', size: 400 }),
      task({ id: '3', status: 'paused', size: 400 }),
      task({ id: '4', status: 'queued', enabled: false }),
      task({ id: '5', status: 'collected' }),
      task({ id: '6', status: 'error', nextTry: new Date(NOW + 60_000).toISOString() }),
    ];
    const f = queueFigures(tasks, NOW);
    expect(f).toMatchObject({ listed: 5, running: 1, waiting: 3, speed: 100, eta: 10, halted: false });
  });

  it('measures progress over the packages still being loaded', () => {
    const tasks = [
      task({ id: '1', package: 'P', status: 'done', size: 100, loaded: 100 }),
      task({ id: '2', package: 'P', status: 'running', size: 100, loaded: 50, speed: 1 }),
      task({ id: '3', package: 'Old', status: 'done', size: 1000, loaded: 1000 }),
    ];
    expect(queueFigures(tasks, NOW).percent).toBe(75);
    expect(queueFigures([task({ status: 'running', size: 0 })], NOW).percent).toBeNull();
  });

  it('sees the master switch holding a download back', () => {
    expect(queueFigures([task({ waiting: 'halted' })], NOW).halted).toBe(true);
  });
});

describe('the sentence of the head', () => {
  const figures = (tasks: Task[]) => queueFigures(tasks, NOW);

  it('never rounds a wait up to everything being loaded', () => {
    expect(headState(2, figures([task({ status: 'running' })]))).toBe('needs');
    expect(headState(0, figures([task({ status: 'running' })]))).toBe('running');
    expect(headState(0, figures([task({ status: 'done' }), task({ id: 'b' })]))).toBe('waiting');
    expect(headState(0, figures([task({ status: 'done' })]))).toBe('done');
    expect(headState(0, figures([task({ status: 'collected' })]))).toBe('empty');
  });
});
