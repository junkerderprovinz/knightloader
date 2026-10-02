// The background watch announces what it should, once, and stops when it
// should.
//
// src/watch/rules.ts imports nothing at run time, so node strips its types and
// these checks call the real functions: what counts as a captcha that arrived,
// a finished package and a failed download, what keeps the service running,
// and how the pace slows while nothing changes.
//
// Run by hand and by CI, from mobile/: `node check-watch.mjs`
import { dirname, join } from 'node:path';
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

// Busy.
expect('a running download is busy', w.isBusy([task('a', 'running')], []), true);
expect('an unpacking is busy', w.isBusy([task('a', 'extracting')], []), true);
expect('a waiting captcha is busy', w.isBusy([], [captcha('c1')]), true);
expect('a queue that waits is not busy', w.isBusy([task('a', 'queued')], []), false);
expect('seeding is not busy', w.isBusy([task('a', 'done', { seeding: true })], []), false);

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

if (problems.length) {
  console.error(problems.join('\n'));
  process.exit(1);
}
console.log('ok: the watch announces and stops as it should');
