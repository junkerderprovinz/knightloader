// The background watch announces what it should, once, and stops when it
// should.
//
// src/watch/rules.ts imports nothing at run time, so node strips its types and
// these checks call the real functions: what counts as a captcha that arrived,
// a finished package and a failed download, what keeps the service running,
// and how the pace slows while nothing changes.
//
// Run by hand and by CI, from mobile/: `node check-watch.mjs`
import { registerHooks } from 'node:module';
import { dirname, join } from 'node:path';
import { mock } from 'node:test';
import { fileURLToPath, pathToFileURL } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const w = await import(pathToFileURL(join(here, 'src', 'watch', 'rules.ts')).href);

const problems = [];
const expect = (what, got, want) => {
  const g = JSON.stringify(got);
  const x = JSON.stringify(want);
  if (g !== x) problems.push(`${what}: got ${g}, want ${x}`);
};

const task = (id, status, extra = {}) => ({
  id,
  url: `https://example.net/${id}`,
  name: `${id}.bin`,
  package: '',
  resolver: 'http',
  size: 1,
  loaded: 0,
  speed: 0,
  status,
  createdAt: '2026-01-01T00:00:00Z',
  priority: 0,
  position: 0,
  enabled: true,
  ...extra,
});
const captcha = (id) => ({ id, source: 'jd', host: 'example.net', kind: 'image', expiresAt: '' });
const names = (list) => list.map((p) => p.name);

// The first look is the baseline.
{
  const news = w.compare(null, [task('a', 'done')], [captcha('c1')]);
  expect('the first look announces nothing', [news.arrived.length, news.finished.length, news.failed.length], [0, 0, 0]);
}

// Captchas.
{
  const before = w.lookOf([], [captcha('c1')]);
  const news = w.compare(before, [], [captcha('c1'), captcha('c2')]);
  expect('a new captcha arrives', news.arrived.map((c) => c.id), ['c2']);
  expect('a captcha already waiting does not arrive again', w.compare(before, [], [captcha('c1')]).arrived, []);
  expect('a captcha that left is a change', w.compare(before, [], []).changed, true);
}

// A lone download.
{
  const before = w.lookOf([task('a', 'running')], []);
  const done = w.compare(before, [task('a', 'done')], []);
  expect('a running download that finishes is news', names(done.finished), ['a.bin']);
  expect('a finished download changed', done.changed, true);
  const again = w.compare(w.lookOf([task('a', 'done')], []), [task('a', 'done')], []);
  expect('a finished download is announced once', again.finished, []);
  expect('nothing changed', again.changed, false);
  const failed = w.compare(before, [task('a', 'error')], []);
  expect('a download that fails is news', names(failed.failed), ['a.bin']);
  expect('a failure alone is no finished package', failed.finished, []);
  const seeding = w.compare(before, [task('a', 'done', { seeding: true })], []);
  expect('a torrent that starts seeding has finished', names(seeding.finished), ['a.bin']);
}

// A download added and finished between two looks.
{
  const before = w.lookOf([task('a', 'running')], []);
  const news = w.compare(before, [task('a', 'running'), task('b', 'done')], []);
  expect('a quick download is still announced', names(news.finished), ['b.bin']);
}

// Packages.
{
  const p = (id, status, extra = {}) => task(id, status, { package: 'Holiday', ...extra });
  const before = w.lookOf([p('a', 'done'), p('b', 'running'), p('c', 'queued')], []);
  const halfway = w.compare(before, [p('a', 'done'), p('b', 'done'), p('c', 'running')], []);
  expect('a package with a file still to come has not finished', halfway.finished, []);

  const last = w.compare(w.lookOf([p('a', 'done'), p('b', 'done'), p('c', 'running')], []), [p('a', 'done'), p('b', 'done'), p('c', 'done')], []);
  expect('the last file finishes the package', names(last.finished), ['Holiday']);
  expect('a finished package counts its files', [last.finished[0].total, last.finished[0].done, last.finished[0].failed], [3, 3, 0]);

  const mixed = w.compare(w.lookOf([p('a', 'done'), p('b', 'running')], []), [p('a', 'done'), p('b', 'error')], []);
  expect('a package whose last file fails still finished', [mixed.finished[0].done, mixed.finished[0].failed], [1, 1]);
  expect('and the failure is news of its own', names(mixed.failed), ['Holiday']);

  const disabled = w.compare(
    w.lookOf([p('a', 'running'), p('b', 'queued', { enabled: false }), p('c', 'collected')], []),
    [p('a', 'done'), p('b', 'queued', { enabled: false }), p('c', 'collected')],
    [],
  );
  expect('a disabled or collected link does not hold a package back', names(disabled.finished), ['Holiday']);

  const failures = w.compare(
    w.lookOf([p('a', 'running'), p('b', 'running'), p('c', 'queued')], []),
    [p('a', 'error'), p('b', 'error'), p('c', 'running')],
    [],
  );
  expect('failures in one package make one piece of news', failures.failed.map((n) => n.tasks.length), [2]);
}

// A failure the instance retries by itself.
{
  const p = (id, status, extra = {}) => task(id, status, { package: 'Holiday', ...extra });
  const retry = (id) => p(id, 'error', { retrying: true });
  const before = w.lookOf([p('a', 'done'), p('b', 'running')], []);
  const waiting = w.compare(before, [p('a', 'done'), retry('b')], []);
  expect('a failure with a retry to come is no failed download', waiting.failed, []);
  expect('nor does it finish its package', waiting.finished, []);
  const tries = [p('b', 'queued'), p('b', 'running'), retry('b')];
  let look = w.lookOf([p('a', 'done'), retry('b')], []);
  const told = [];
  for (const b of tries) {
    const news = w.compare(look, [p('a', 'done'), b], []);
    told.push(news.failed.length + news.finished.length);
    look = w.lookOf([p('a', 'done'), b], []);
  }
  expect('a retry that fails again is still no news', told, [0, 0, 0]);
  const over = w.compare(look, [p('a', 'done'), p('b', 'error')], []);
  expect('the last failure is news once the retries are spent', names(over.failed), ['Holiday']);
  expect('and it finishes the package', [over.finished.length, over.finished[0]?.failed], [1, 1]);
  const after = w.compare(look, [p('a', 'done'), p('b', 'done')], []);
  expect('a retry that succeeds finishes the package', [after.finished[0]?.done, after.failed.length], [2, 0]);
}

// A mirror that takes over a failed download.
{
  const p = (id, status, extra = {}) => task(id, status, { package: 'Film', ...extra });
  const before = w.lookOf([p('a', 'done'), p('d', 'running'), p('m', 'collected', { enabled: false })], []);
  const over = w.compare(before, [p('a', 'done'), p('d', 'error', { handedOver: true }), p('m', 'queued')], []);
  expect('a failure a mirror took on is no failed download', over.failed, []);
  expect('nor does it finish its package', over.finished, []);
  const look = w.lookOf([p('a', 'done'), p('d', 'error', { handedOver: true }), p('m', 'running')], []);
  const done = w.compare(look, [p('a', 'done'), p('d', 'error', { handedOver: true }), p('m', 'done')], []);
  const f = done.finished[0];
  expect('the mirror finishing finishes the package without a failure', [f?.total, f?.done, f?.failed], [2, 2, 0]);
  const lost = w.compare(look, [p('a', 'done'), p('d', 'error'), p('m', 'error')], []);
  expect('when the mirror fails too, its failure is the news', lost.failed.map((n) => n.tasks.map((t) => t.id)), [['m']]);
}

// The full list of an instance from before GET /api/tasks/watch.
{
  const zero = '0001-01-01T00:00:00Z';
  const later = '2099-01-01T00:00:00Z';
  const read = (t) => {
    const r = w.watchTaskOf(t);
    return [r.retrying, r.stalled, r.remote];
  };
  expect('a failure with a retry due is retrying', read(task('a', 'error', { nextTry: later })), [true, false, false]);
  expect('Go\'s zero time is no retry', read(task('a', 'error', { nextTry: zero })), [false, false, false]);
  expect('a stalled download is stalled', read(task('a', 'running', { stalledSince: later })), [false, true, false]);
  expect('Go\'s zero time is no stall', read(task('a', 'running', { stalledSince: zero })), [false, false, false]);
  expect('a debrid fetch is remote', read(task('a', 'running', { remote: { progress: 0.5 } })), [false, false, true]);
  expect('a task with no name goes by its link', w.watchTaskOf(task('a', 'done', { name: '' })).name, 'https://example.net/a');
}

// Busy.
expect('a running download is busy', w.isBusy([task('a', 'running')], []), true);
expect('an unpacking is busy', w.isBusy([task('a', 'extracting')], []), true);
expect('a waiting captcha is busy', w.isBusy([], [captcha('c1')]), true);
expect('a queue that waits is not busy', w.isBusy([task('a', 'queued')], []), false);
expect('seeding is not busy', w.isBusy([task('a', 'done', { seeding: true })], []), false);
expect('a stalled download is still busy', w.isBusy([task('a', 'running', { stalled: true })], []), true);
expect('a running download moves', w.isMoving([task('a', 'running')], []), true);
expect('a waiting captcha moves', w.isMoving([], [captcha('c1')]), true);
expect('a stalled download does not move', w.isMoving([task('a', 'running', { stalled: true })], []), false);
expect('a torrent a debrid service fetches does not move here', w.isMoving([task('a', 'running', { remote: true })], []), false);

// Keeping the service.
const now = 10 * 60 * 60_000;
expect('a fresh start keeps running', w.keepRunning([], now - 1000, now, false), true);
expect('nothing busy stops it', w.keepRunning([{ lastBusy: 0, ok: true }], 0, now, false), false);
expect('busy a moment ago keeps it', w.keepRunning([{ lastBusy: now - 30_000, ok: true }], 0, now, false), true);
expect('idle past the grace stops it', w.keepRunning([{ lastBusy: now - w.GRACE_MS - 1, ok: true }], 0, now, false), false);
expect(
  'an instance out of reach since it was busy keeps it longer',
  w.keepRunning([{ lastBusy: now - w.GRACE_MS - 1, ok: false }], 0, now, false),
  true,
);
expect(
  'but not for ever',
  w.keepRunning([{ lastBusy: now - w.OFFLINE_GRACE_MS - 1, ok: false }], 0, now, false),
  false,
);

// Staying connected.
expect('staying connected keeps it with nothing busy', w.keepRunning([{ lastBusy: 0, ok: true }], 0, now, true), true);
expect('staying connected keeps it with an instance out of reach', w.keepRunning([{ lastBusy: 0, ok: false }], 0, now, true), true);
expect('staying connected with no instance saved stops it', w.keepRunning([], 0, now, true), false);

// Pace.
expect('a change brings the pace back up', w.nextDelay(w.SLOW_MS, true, false, true), w.FAST_MS);
expect('a waiting captcha keeps the pace up', w.nextDelay(w.IDLE_MS, false, true, false), w.FAST_MS);
let d = w.FAST_MS;
for (let i = 0; i < 10; i++) d = w.nextDelay(d, false, false, true);
expect('a download without news slows to the slowest busy pace', d, w.SLOW_MS);
expect('nothing running waits at the idle pace', w.nextDelay(w.FAST_MS, false, false, false), w.IDLE_MS);
expect('a download starting after idling looks again soon', w.nextDelay(w.IDLE_MS, false, false, true), w.SLOW_MS);
expect('the busy paces keep the phone awake', [w.FAST_MS, w.SLOW_MS].map(w.stayAwake), [true, true]);
expect('the idle pace leaves the wait to an alarm', w.stayAwake(w.IDLE_MS), false);

// Notification ids.
expect('the same parts give the same id', w.noticeId('captcha', 'x'), w.noticeId('captcha', 'x'));
const ids = ['', 'a', 'b', 'captcha', 'finished'].map((s) => w.noticeId(s));
expect('ids stay clear of the service notification', ids.every((id) => id >= 2 && id <= 0x7fffffff), true);
expect('different parts give different ids', w.noticeId('finished', 'x', 'p') !== w.noticeId('failed', 'x', 'p'), true);

// The pass in src/watch/watch.ts, on stand-ins for React, React Native, the
// native module and the instances: what it asks an instance for, what it posts
// and how it paces the service.
const s = {
  appState: 'background',
  running: false,
  listeners: [],
  cleanups: [],
  calls: [],
  storage: new Map(),
  conns: [{ id: 'one', name: 'One' }],
  captchas: [],
  fetchWatch: async () => ({ tag: '', tasks: [] }),
  fetchTasks: async () => [],
  ApiError: class extends Error {
    constructor(message, status) {
      super(message);
      this.status = status;
    }
  },
};
s.native = {
  channels() {},
  start: () => (s.running = true),
  stop() {
    s.running = false;
    s.calls.push('stop');
  },
  running: () => s.running,
  next: (ms, awake) => s.calls.push({ next: ms, awake }),
  autostart: (on) => s.calls.push({ autostart: on }),
  batteryExempt: () => true,
  notify: (n) => s.calls.push({ notify: n.channel, text: n.text }),
  cancel() {},
  enabled: () => true,
  takeOpen: () => null,
  addListener: () => ({ remove() {} }),
};
globalThis.watchStandIns = s;
const stubs = {
  react:
    'export const useEffect = (f) => { const undo = f(); if (undo) globalThis.watchStandIns.cleanups.push(undo); };\n' +
    'export const useRef = (current) => ({ current });',
  'react-native':
    'const s = globalThis.watchStandIns;\n' +
    'export const AppState = { get currentState() { return s.appState; }, addEventListener(_, f) { s.listeners.push(f); return { remove() {} }; } };\n' +
    "export const Platform = { OS: 'android', Version: 34 };\n" +
    "export const PermissionsAndroid = { PERMISSIONS: {}, RESULTS: { GRANTED: 'granted' }, check: async () => true, request: async () => 'granted' };",
  '../../modules/watch': 'export const KnightWatch = globalThis.watchStandIns.native;',
  '../api/client':
    'const s = globalThis.watchStandIns;\n' +
    'export const ApiError = s.ApiError;\n' +
    'export const fetchWatch = (...a) => s.fetchWatch(...a);\n' +
    'export const fetchTasks = (...a) => s.fetchTasks(...a);\n' +
    'export const fetchCaptchasUnwatched = async () => s.captchas;',
  '../i18n': "export const AVAILABLE = ['en'];\nexport const load = async () => ({});",
  '../i18n/I18nContext': "export const detectDeviceLanguage = () => 'en';\nexport const translate = (_, key) => key;",
  '../storage/connections': 'export const listConnections = async () => globalThis.watchStandIns.conns;',
  '../storage/languagePreference': 'export const getLanguageOverride = async () => null;',
  '@react-native-async-storage/async-storage':
    'const s = globalThis.watchStandIns;\n' +
    'export default { getItem: async (k) => s.storage.get(k) ?? null, setItem: async (k, v) => { s.storage.set(k, v); } };',
};
const src = pathToFileURL(join(here, 'src')).href + '/';
registerHooks({
  resolve(specifier, context, next) {
    if (!context.parentURL?.startsWith(src)) return next(specifier, context);
    if (Object.hasOwn(stubs, specifier)) return { url: `stub:${specifier}`, shortCircuit: true };
    if (/^\.\.?\//.test(specifier) && !/\.\w+$/.test(specifier)) return next(`${specifier}.ts`, context);
    return next(specifier, context);
  },
  load(url, context, next) {
    if (url.startsWith('stub:')) return { format: 'module', source: stubs[url.slice(5)], shortCircuit: true };
    return next(url, context);
  },
});
const watch = await import(pathToFileURL(join(here, 'src', 'watch', 'watch.ts')).href);

const settle = async () => {
  for (let i = 0; i < 20; i++) await new Promise((r) => setImmediate(r));
};
const prefs = (p) =>
  s.storage.set('knightloader-notifications', JSON.stringify({ captcha: true, finished: true, failed: true, stay: true, ...p }));
const answer = (tasks) => {
  s.fetchWatch = async () => ({ tag: JSON.stringify(tasks), tasks });
};
// Each part watches an instance of its own, so what one leaves behind is not
// what the next one compares with.
let instances = 0;
const fresh = () => {
  watch.stopWatch();
  s.conns = [{ id: `i${++instances}`, name: 'One' }];
  s.calls = [];
  s.captchas = [];
  s.running = false;
  s.appState = 'background';
};
const notices = () => s.calls.filter((c) => c.notify);
const lastNext = () => s.calls.filter((c) => c.next).at(-1);

// Asking only for what changed.
{
  fresh();
  prefs({});
  const tags = [];
  const lists = [
    { tag: 't1', tasks: [task('a', 'done')] },
    { tag: 't1', same: true },
    { tag: 't2', tasks: [task('a', 'done'), task('b', 'done')] },
  ];
  s.fetchWatch = async (_, tag) => {
    tags.push(tag);
    return lists.shift();
  };
  for (let i = 0; i < 3; i++) await watch.watchTask();
  expect('each look sends the tag of the list before it', tags, ['', 't1', 't1']);
  expect('an unchanged list keeps what the last one said', notices().map((c) => c.text), ['b.bin']);
}

// An instance from before the watch list.
{
  fresh();
  prefs({});
  let asked = 0;
  s.fetchWatch = async () => {
    asked++;
    throw new s.ApiError('no such endpoint', 404);
  };
  const lists = [
    [task('a', 'running')],
    [task('a', 'error', { nextTry: '2099-01-01T00:00:00Z' })],
    [task('a', 'error', { nextTry: '0001-01-01T00:00:00Z' })],
  ];
  s.fetchTasks = async () => lists.shift();
  await watch.watchTask();
  await watch.watchTask();
  expect('a retry read from the full list is no failure', notices(), []);
  await watch.watchTask();
  expect('the full list is read once the watch list is refused', asked, 1);
  expect('the last failure read from the full list is news', notices().map((c) => c.notify), ['failed']);
}

// The pace.
{
  fresh();
  prefs({ captcha: false });
  answer([task('a', 'queued')]);
  s.captchas = [captcha('c1')];
  await watch.watchTask();
  expect('a captcha nobody is told about keeps no quick pace', lastNext()?.next > w.FAST_MS, true);

  fresh();
  prefs({});
  answer([task('a', 'running', { stalled: true }), task('b', 'running', { remote: true })]);
  await watch.watchTask();
  expect('downloads that move nothing here leave the wait to an alarm', lastNext(), { next: w.IDLE_MS, awake: false });

  fresh();
  prefs({ stay: false });
  answer([task('b', 'running', { remote: true })]);
  await watch.watchTask();
  expect('a debrid fetch keeps the service with "Stay connected" off', s.calls.includes('stop'), false);
}

// The first look after a pause.
{
  fresh();
  prefs({ stay: false });
  answer([task('a', 'queued')]);
  await watch.watchTask();
  expect('nothing on the go stops the service', s.calls.includes('stop'), true);
  answer([task('a', 'done'), task('b', 'done'), task('c', 'error')]);
  await watch.watchPass();
  expect('what finished after the service stopped is not announced', notices(), []);

  fresh();
  prefs({ stay: false });
  answer([task('a', 'queued')]);
  s.appState = 'active';
  watch.useWatch(null);
  await settle();
  s.appState = 'background';
  answer([task('a', 'done'), task('b', 'done')]);
  s.appState = 'active';
  for (const f of s.listeners) f('active');
  await settle();
  for (const undo of s.cleanups) undo();
  expect('what finished while the app was away is not announced', notices(), []);
}

// Every kind switched off.
{
  fresh();
  s.running = true;
  watch.stopWatch();
  expect('stopping the watch keeps a restart from starting it', s.calls, [{ autostart: false }, 'stop']);
}

// Last, since a pass that never ends holds up every pass after it.
// A look that never answers.
{
  fresh();
  prefs({});
  s.fetchWatch = (_, __, signal) =>
    new Promise((_, reject) => signal?.addEventListener('abort', () => reject(new Error('aborted'))));
  mock.timers.enable({ apis: ['setTimeout'] });
  const pass = watch.watchPass();
  await settle();
  mock.timers.tick(60_000);
  const ended = await Promise.race([pass.then(() => true), settle().then(() => false)]);
  mock.timers.reset();
  expect('a look that never answers ends the pass', ended, true);
}

if (problems.length) {
  console.error(problems.join('\n'));
  process.exit(1);
}
console.log('ok: the watch announces and stops as it should');
