// The speed graph on the Downloads screen draws by the web interface's rules:
// the same ceiling, the same seeding sum, the same window label. A limit is
// only a mark at the right edge, never a line across the plot: at its height
// inside the scale, or on the top edge with a chevron for one far above a slow
// transfer. Two figures never cover each other. The marks are muted and carry a
// limit sign, while the live rates above the plot wear their curves' colours.
// Readings taken while the server's record is on its way never reach the
// window, so the record and the live tail meet without a notch or a repeat.
//
// src/components/speedPlot.ts imports nothing at run time, so node strips its
// types and these checks call the real functions.
//
// Run by hand and by CI, from mobile/: `node check-speed-graph.mjs`
import { existsSync, readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const {
  busyOf,
  ceilingOf,
  curve,
  GRACE_TICKS,
  labelTops,
  limitMark,
  newFeed,
  reading,
  seedFeed,
  seedingDefault,
  spanLabel,
  tick,
  uploadOf,
  washOf,
} = await import(pathToFileURL(join(here, 'src', 'components', 'speedPlot.ts')).href);

const problems = [];
const expect = (what, got, want) => {
  const g = JSON.stringify(got);
  const w = JSON.stringify(want);
  if (g !== w) problems.push(`${what}: got ${g}, want ${w}`);
};
const check = (what, ok) => {
  if (!ok) problems.push(what);
};

const KB = 1000;
const MB = 1000 * KB;
const H = 56;
const PAD = 4;
const LABEL = 11;

expect('the ceiling is the peak without a limit', ceilingOf([MB, 3 * MB, 2 * MB]), 3 * MB);
expect('a trickle is drawn against the floor, not blown up to full height', ceilingOf([KB, 2 * KB]), 64 * KB);
expect('the seeding samples count towards the ceiling', ceilingOf([MB, 2 * MB, 5 * MB]), 5 * MB);
expect('a peak above the limit keeps the scale', ceilingOf([MB, 9 * MB], 2 * MB), 9 * MB);
check('a limit just above the peak is taken in with room over it', ceilingOf([4 * MB, 5 * MB], 5 * MB) > 5 * MB);
check('an idle box lets the limit set the scale', ceilingOf([0, 0, 0], 100 * MB) > 100 * MB);

{
  // The case seen on a phone: 1 MB/s under a 30 MB/s limit.
  const samples = new Array(31).fill(MB);
  const top = ceilingOf(samples, 30 * MB);
  expect('a limit far above a steady download stays out of the scale', top, MB);
  expect('and sits on the top edge', limitMark(30 * MB, top, H, PAD), { y: PAD, over: true });
}

{
  const top = ceilingOf([4 * MB, 5 * MB], 5 * MB);
  const m = limitMark(5 * MB, top, H, PAD);
  check('a limit inside the scale is marked at its height', m !== null && !m.over);
  if (m) check(`the mark sits inside the plot, got y ${m.y}`, m.y > PAD && m.y < H - PAD);
}

expect('a low limit stands a quarter of the way up', limitMark(MB, 4 * MB, H, PAD), { y: H - PAD - 12, over: false });

{
  const top = ceilingOf([0, 0, 0], 5 * MB);
  expect('an idle box draws its limit inside the plot', limitMark(5 * MB, top, H, PAD)?.over, false);
}

expect('no limit, no mark', limitMark(0, MB, H, PAD), null);

{
  // Read as text, since the view needs React Native to run: the limits' red
  // lines across the plot are gone, the marks at the right edge stay.
  const graph = readFileSync(join(here, 'src', 'components', 'SpeedGraph.tsx'), 'utf8');
  check('the graph draws no line across the plot for a limit', !graph.includes('statusFailSolid'));
  check('the graph still draws a mark for each limit', graph.includes('<LimitMarks marks={marks}'));

  const marks = graph.slice(graph.indexOf('function LimitMarks'), graph.indexOf('const notch'));
  check('the limit marks are drawn in the muted ink', marks.includes('const ink = c.textMuted'));
  check('the limit marks take no curve colour', !/downInk|upInk|accentInk/.test(marks));
  check('each limit figure carries the limit sign', marks.includes('<SpeedLimit color={ink}'));
  check('the limit sign is in the assets', existsSync(join(here, 'assets', 'speed-limit.png')));
  const badge = readFileSync(join(here, 'src', 'components', 'IconBadge.tsx'), 'utf8');
  check('the limit sign is a tinted bitmap', badge.includes("require('../../assets/speed-limit.png')"));

  const rates = graph.slice(graph.indexOf('<View style={styles.rates}>'), graph.indexOf('<View style={[styles.plot'));
  check('the live download rate is in the download colour', /color: downInk \}\]}[^>]*>\s*\{fmtSpeed\(speed\)\}/.test(rates));
  check('the live upload rate is in the seeding colour', /color: upInk \}\]}[^>]*>\s*\{fmtSpeed\(upload\)\}/.test(rates));
}

{
  // Two ticks before the record arrives, both zero since the task list has not
  // either, then the record, then the live readings.
  const f = newFeed(true);
  check('a tick while the record is on its way is not taken', !tick(f, 0, 0, 30));
  check('nor is a real reading', !tick(f, 900, 40, 30));
  expect('the window waits empty for the record', f.samples, { down: [], up: [] });
  seedFeed(f, { down: [500, 600, 700], up: [10, 20, 30] }, 30);
  tick(f, 0, 0, 30);
  tick(f, 800, 50, 30);
  expect('the record and the live tail meet without a zero or a repeat', f.samples, {
    down: [500, 600, 700, 700, 800],
    up: [10, 20, 30, 30, 50],
  });

  const long = Array.from({ length: 120 }, (_, i) => i + 1);
  const g = newFeed(true);
  seedFeed(g, { down: long, up: long }, 30);
  expect('a record longer than the window keeps its newest samples', g.samples.down, long.slice(-31));

  const h = newFeed(false);
  tick(h, 5, 1, 30);
  check('without a record to wait for, the first tick is taken', h.samples.down.length === 1);
  for (let i = 0; i < 40; i++) tick(h, 5, 1, 30);
  expect('the window keeps one sample more than its span', h.samples.down.length, 31);
}

check('both limits within reach count towards the ceiling', ceilingOf([MB], 2 * MB, 3 * MB) > 3 * MB);
expect('a far upload limit stays out while a near download limit counts', ceilingOf([MB], 2 * MB, 100 * MB), 2.4 * MB);

expect('a figure goes over its mark where there is room', labelTops([30], H, LABEL), [30 - 2 - LABEL]);
expect('and under it at the top edge', labelTops([PAD], H, LABEL), [PAD + 2]);
for (const ys of [
  [PAD, PAD],
  [30, 31],
  [H - PAD, H - PAD],
]) {
  const [a, b] = labelTops(ys, H, LABEL);
  check(`two figures at ${ys} stay clear of each other, got ${a} and ${b}`, Math.abs(a - b) > LABEL);
  check(`two figures at ${ys} stay in the plot, got ${a} and ${b}`, Math.min(a, b) >= 0 && Math.max(a, b) + LABEL <= H);
}

expect(
  'the upload counts while a torrent downloads or seeds',
  uploadOf([
    { status: 'running', uploadSpeed: 100 },
    { status: 'done', seeding: true, uploadSpeed: 50 },
    { status: 'done', uploadSpeed: 999 },
    { status: 'paused', uploadSpeed: 999 },
    { status: 'running' },
  ]),
  150,
);

expect('the half-minute window', spanLabel(30), '-30s');
expect('a minute and a half', spanLabel(90), '-1.5min');
expect('an hour', spanLabel(3600), '-1h');

{
  const lane = { grace: GRACE_TICKS, carry: 700 };
  const got = [reading(lane, 0), reading(lane, 0), reading(lane, 0)];
  expect('a zero before the task list arrives stands in with the record, then means idle', got, [700, 700, 0]);
  const live = { grace: GRACE_TICKS, carry: 700 };
  expect('the first real reading ends the grace', [reading(live, 5), reading(live, 0)], [5, 0]);
}

expect('one sample is not a curve yet', busyOf([MB], [0]), false);
expect('nothing moving either way is idle', busyOf([0, 0, 0], [0, 0, 0]), false);
expect('seeding alone is busy', busyOf([0, 0, 0], [0, KB, 0]), true);
expect('a download is busy', busyOf([0, MB], []), true);

expect('the default gold accent gets the blue seeding line', seedingDefault('#FCC419', ['#FCC419', '#1D99F3', '#6FDC8C']), '#1D99F3');

{
  const w = 300;
  const samples = [0, 5 * MB, MB, 8 * MB, 8 * MB, 0, 2 * MB];
  const top = ceilingOf(samples);
  const pts = curve(samples, w, H, PAD, 30, top, 3);
  expect('three pieces per sample after the first', pts.length, 1 + (samples.length - 1) * 3);
  check('the newest sample sits on the right edge', Math.abs(pts[pts.length - 1].x - w) < 1e-9);
  check('the window fills from the right', pts[0].x === w - (samples.length - 1) * (w / 30));
  check(
    'the curve never swings below zero or above the ceiling',
    pts.every((p) => p.y >= PAD - 1e-9 && p.y <= H - PAD + 1e-9),
  );
  expect('fewer than two samples draw nothing', curve([MB], w, H, PAD, 30, top, 3), []);

  const cols = washOf(pts, H - PAD);
  expect('one column per piece', cols.length, pts.length - 1);
  check('every column reaches down to the ground', cols.every((c) => Math.abs(c.top + c.height - (H - PAD)) < 1e-9));
  check('the columns leave no gap between them', cols.every((c, i) => i === 0 || cols[i - 1].left + cols[i - 1].width >= c.left));
}

if (problems.length) {
  console.error(`${problems.length} problem(s):`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}
console.log('ok: speed graph scale, limit marks, live rates, seeding sum, seed seam, curve and its fill');
