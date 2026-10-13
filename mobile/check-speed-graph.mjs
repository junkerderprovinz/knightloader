// The speed graph on the Downloads screen draws by the web interface's rules:
// the same ceiling, the same seeding sum, the same window label. A limit is a
// line only where it falls inside the scale; one far above a slow transfer
// keeps only its mark on the top edge, since a line there, where the peak
// ends, reads as a transfer running at the limit. Two figures never cover
// each other.
//
// src/components/speedPlot.ts imports nothing at run time, so node strips its
// types and these checks call the real functions.
//
// Run by hand and by CI, from mobile/: `node check-speed-graph.mjs`
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
  reading,
  seedingDefault,
  spanLabel,
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

const KIB = 1024;
const MIB = 1024 * KIB;
const H = 56;
const PAD = 4;
const LABEL = 11;

expect('the ceiling is the peak without a limit', ceilingOf([MIB, 3 * MIB, 2 * MIB]), 3 * MIB);
expect('a trickle is drawn against the floor, not blown up to full height', ceilingOf([KIB, 2 * KIB]), 64 * KIB);
expect('the seeding samples count towards the ceiling', ceilingOf([MIB, 2 * MIB, 5 * MIB]), 5 * MIB);
expect('a peak above the limit keeps the scale', ceilingOf([MIB, 9 * MIB], 2 * MIB), 9 * MIB);
check('a limit just above the peak is taken in with room over it', ceilingOf([4 * MIB, 5 * MIB], 5 * MIB) > 5 * MIB);
check('an idle box lets the limit set the scale', ceilingOf([0, 0, 0], 100 * MIB) > 100 * MIB);

{
  // The case seen on a phone: 1 MiB/s under a 30 MiB/s limit.
  const samples = new Array(31).fill(MIB);
  const top = ceilingOf(samples, 30 * MIB);
  expect('a limit far above a steady download stays out of the scale', top, MIB);
  expect('and sits on the top edge with no line', limitMark(30 * MIB, top, H, PAD), { y: PAD, over: true });
}

{
  const top = ceilingOf([4 * MIB, 5 * MIB], 5 * MIB);
  const m = limitMark(5 * MIB, top, H, PAD);
  check('a limit inside the scale gets a line', m !== null && !m.over);
  if (m) check(`the line sits inside the plot, got y ${m.y}`, m.y > PAD && m.y < H - PAD);
}

expect('a low limit stands a quarter of the way up', limitMark(MIB, 4 * MIB, H, PAD), { y: H - PAD - 12, over: false });

{
  const top = ceilingOf([0, 0, 0], 5 * MIB);
  expect('an idle box draws its limit inside the plot', limitMark(5 * MIB, top, H, PAD)?.over, false);
}

expect('no limit, no mark', limitMark(0, MIB, H, PAD), null);

check('both limits within reach count towards the ceiling', ceilingOf([MIB], 2 * MIB, 3 * MIB) > 3 * MIB);
expect('a far upload limit stays out while a near download limit counts', ceilingOf([MIB], 2 * MIB, 100 * MIB), 2.4 * MIB);

expect('a figure goes over its line where there is room', labelTops([30], H, LABEL), [30 - 2 - LABEL]);
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

expect('one sample is not a curve yet', busyOf([MIB], [0]), false);
expect('nothing moving either way is idle', busyOf([0, 0, 0], [0, 0, 0]), false);
expect('seeding alone is busy', busyOf([0, 0, 0], [0, KIB, 0]), true);
expect('a download is busy', busyOf([0, MIB], []), true);

expect('the default gold accent gets the blue seeding line', seedingDefault('#FCC419', ['#FCC419', '#1D99F3', '#6FDC8C']), '#1D99F3');

{
  const w = 300;
  const samples = [0, 5 * MIB, MIB, 8 * MIB, 8 * MIB, 0, 2 * MIB];
  const top = ceilingOf(samples);
  const pts = curve(samples, w, H, PAD, 30, top, 3);
  expect('three pieces per sample after the first', pts.length, 1 + (samples.length - 1) * 3);
  check('the newest sample sits on the right edge', Math.abs(pts[pts.length - 1].x - w) < 1e-9);
  check('the window fills from the right', pts[0].x === w - (samples.length - 1) * (w / 30));
  check(
    'the curve never swings below zero or above the ceiling',
    pts.every((p) => p.y >= PAD - 1e-9 && p.y <= H - PAD + 1e-9),
  );
  expect('fewer than two samples draw nothing', curve([MIB], w, H, PAD, 30, top, 3), []);

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
console.log('ok: speed graph scale, limit marks, seeding sum, curve and its fill');
