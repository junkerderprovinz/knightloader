// What a link's or a package's More sheet offers, and what each entry sends.
// The entries follow the web interface's right-click menu, state for state,
// and every call goes to a route the instance lets a paired phone reach.
//
// src/api/taskActions.ts imports nothing at run time, so node strips its types
// and these checks call the real functions. The state sets are read off the
// web's ListToolbar.tsx, the routes off internal/api/group_routes.go and the
// labels off the English catalogue.
//
// Run by hand and by CI, from mobile/: `node check-task-actions.mjs`
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const actions = await import(pathToFileURL(join(here, 'src', 'api', 'taskActions.ts')).href);
const { offeredActions, actionCall, retryFailedCall, undoCall, sharedPriority, ACTION_LABELS, PRIORITY_KEYS } = actions;

const problems = [];
const expect = (what, got, want) => {
  const g = JSON.stringify(got);
  const w = JSON.stringify(want);
  if (g !== w) problems.push(`${what}: got ${g}, want ${w}`);
};

const now = Date.parse('2026-06-01T12:00:00Z');
let n = 0;
const task = (status, more = {}) => ({ id: `t${++n}`, status, priority: 0, enabled: true, ...more });
const offered = (tasks, opts = {}) =>
  offeredActions(tasks, { now, priorities: true, ...opts }).map((g) => g.map((o) => o.id));

expect('a running link', offered([task('running')]), [['pause'], ['moveTop', 'moveBottom', 'priority'], ['remove']]);
expect('a waiting link', offered([task('queued')]), [['force', 'pause'], ['moveTop', 'moveBottom', 'priority'], ['remove']]);
expect('a paused link', offered([task('paused')]), [['force', 'resume'], ['moveTop', 'moveBottom', 'priority'], ['remove']]);
expect(
  'a collected link',
  offered([task('collected')]),
  [['force', 'recheck'], ['moveTop', 'moveBottom', 'priority'], ['remove']],
);
expect('a finished link', offered([task('done')]), [['restart'], ['priority'], ['remove']]);
expect('a failed link with no retry ahead', offered([task('error')]), [['restart'], ['priority'], ['remove']]);
expect(
  'a failed link waiting on its next try',
  offered([task('error', { nextTry: '2026-06-01T12:05:00Z' })]),
  [['retryNow', 'restart'], ['priority'], ['remove']],
);
expect(
  'a failed link whose try is past',
  offered([task('error', { nextTry: '2026-06-01T11:55:00Z' })]),
  [['restart'], ['priority'], ['remove']],
);
expect(
  'the zero time Go sends for no retry',
  offered([task('error', { nextTry: '0001-01-01T00:00:00Z' })]),
  [['restart'], ['priority'], ['remove']],
);
expect(
  'a link started ahead of the queue',
  offered([task('queued', { forced: true })]),
  [['force', 'unforce', 'pause'], ['moveTop', 'moveBottom', 'priority'], ['remove']],
);
expect('an unpacking link', offered([task('extracting')]), [['moveTop', 'moveBottom', 'priority'], ['remove']]);
expect(
  'no priority without the list of them',
  offered([task('queued')], { priorities: false }),
  [['force', 'pause'], ['moveTop', 'moveBottom'], ['remove']],
);
expect('a finished package in its own part', offered([task('done'), task('done')], { settled: true }), [['restart'], ['remove']]);
expect(
  'a seeding torrent in its own part',
  offered([task('done', { seeding: true })], { settled: true }),
  [['restart'], ['remove']],
);

// A package acts on all of its links, and each verb on the ones it applies to.
const running = task('running');
const paused = task('paused');
const failed = task('error');
const pkg = [running, paused, failed];
const byId = Object.fromEntries(offeredActions(pkg, { now, priorities: true }).flat().map((o) => [o.id, o.ids]));
expect('a package pauses its running links', byId.pause, [running.id]);
expect('and resumes its paused ones', byId.resume, [paused.id]);
expect('and resets all of them, which the instance narrows', byId.restart, pkg.map((x) => x.id));
expect('and removes all of them', byId.remove, pkg.map((x) => x.id));
expect('and moves all of them', byId.moveTop, pkg.map((x) => x.id));
const collected = task('collected');
const staged = offeredActions([collected, task('queued')], { now, priorities: true }).flat();
expect('a recheck asks about the collected links only', staged.find((o) => o.id === 'recheck').ids, [collected.id]);
const waiting = task('error', { nextTry: '2026-06-01T12:05:00Z' });
const mixed = offeredActions([waiting, task('error')], { now, priorities: true }).flat();
expect('retry now spends the waiting links only', mixed.find((o) => o.id === 'retryNow').ids, [waiting.id]);

expect('one priority all links share', sharedPriority([task('queued', { priority: 2 }), task('queued', { priority: 2 })]), 2);
expect('none when they differ', sharedPriority([task('queued', { priority: 2 }), task('queued', { priority: -1 })]), undefined);

// What each entry sends.
const ids = ['a', 'b'];
const call = (id, extra) => actionCall({ id, ids }, extra);
expect('pause', call('pause'), { path: '/tasks/pause', body: { ids } });
expect('resume', call('resume'), { path: '/tasks/resume', body: { ids } });
expect('reset', call('restart'), { path: '/tasks/restart', body: { ids } });
expect('retry now is a reset of the waiting links', call('retryNow'), { path: '/tasks/restart', body: { ids } });
expect('recheck', call('recheck'), { path: '/tasks/recheck', body: { ids } });
expect('start now', call('force'), { path: '/queue/force', body: { ids } });
expect('cancel start now', call('unforce'), { path: '/tasks/force', body: { ids, forced: false } });
expect('to the top', call('moveTop'), { path: '/queue/move', body: { ids, where: 'top' } });
expect('to the bottom', call('moveBottom'), { path: '/queue/move', body: { ids, where: 'bottom' } });
expect('a priority', call('priority', 3), { path: '/queue/priority', body: { ids, priority: 3 } });
expect('remove keeps the files', call('remove'), { path: '/tasks/delete', body: { ids, files: false } });
expect('reset failed names no link, which means all failed', retryFailedCall, { path: '/tasks/restart', body: { ids: [] } });
expect('undo', undoCall('tok'), { path: '/tasks/undo-delete', body: { token: 'tok' } });

// Every call reaches a route a paired phone may call.
const routes = readFileSync(join(here, '..', 'internal', 'api', 'group_routes.go'), 'utf8');
const allowed = new Set([...routes.matchAll(/^\s*"([A-Z]+ \/api\/[^"]+)":\s*true,/gm)].map((m) => m[1]));
const calls = [
  ...Object.keys(ACTION_LABELS).map((id) => actionCall({ id, ids }, 0)),
  retryFailedCall,
  undoCall('tok'),
];
for (const c of calls) {
  if (!allowed.has(`POST /api${c.path}`)) problems.push(`POST /api${c.path} is not on the relay's allow-list`);
}
if (!/"POST \/api\/tasks\/delete": func[\s\S]*?return !b\.Files/.test(routes)) {
  problems.push('group_routes.go lets a member erase files with a removal, which the app relies on the relay refusing');
}

// The state sets are the web's, which check-queue-reach.mjs holds to the server.
const toolbar = readFileSync(join(here, '..', 'web', 'src', 'components', 'ListToolbar.tsx'), 'utf8');
for (const name of ['FORCE_STATES', 'MOVE_STATES', 'PRIORITY_STATES']) {
  const m = toolbar.match(new RegExp(`export const ${name}: readonly TaskStatus\\[\\] = \\[([^\\]]*)\\]`));
  if (!m) {
    problems.push(`ListToolbar.tsx has no ${name} to compare with`);
    continue;
  }
  const web = [...m[1].matchAll(/'([a-z]+)'/g)].map((x) => x[1]);
  expect(`${name} as the web has it`, [...actions[name]], web);
}

// Every label is a key the English catalogue has.
const en = readFileSync(join(here, 'src', 'i18n', 'en.ts'), 'utf8');
for (const key of [...Object.values(ACTION_LABELS), ...Object.values(PRIORITY_KEYS)]) {
  if (!en.includes(`'${key}':`)) problems.push(`${key} is not in en.ts`);
}

if (problems.length) {
  console.error(problems.join('\n'));
  process.exit(1);
}
console.log('Task actions check out.');
