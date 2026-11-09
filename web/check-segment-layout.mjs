// How a horizontal selector keeps to its one row.
//
// A strip never wraps and no label takes a second line. Its segments keep
// their pinned width while every label fits it, share the row by their
// one-line widths after that, show their glyphs alone where every segment has
// one, and then turn into the dropdown, or for a strip that toggles several
// values cut the long labels short (GlimStone, "It keeps to one row").
//
// The widths are what Chrome measured on the settings pages, each segment at
// max-content, in cards 860 pixels wide less the track's padding: the reclaim
// modes and the collision choices in German, and the reconnect methods, a
// small well at 7.44em of 12px in German.
//
// Run: `node web/check-segment-layout.mjs`. Node reads the TypeScript module
// directly; the measuring stays in Tabs.tsx.
import { segmentFit } from './src/components/segmentLayout.ts';

const problems = [];

function check(what, got, want) {
  const a = JSON.stringify(got);
  const b = JSON.stringify(want);
  if (a !== b) problems.push(`${what}\n      got  ${a}\n      want ${b}`);
}

const GAP = 3.2;
const ROOM = 853.6;
const whole = (fit, count) => ({ fit, cut: Array(count).fill(false) });

// "Ein Eintrag, dass dieser Download sie geschrieben hat" is 377px on one line
// against the 352px ceiling.
const reclaim = [198.938, 377.266, 253.562];
const reclaimRow = reclaim.reduce((a, b) => a + b) + 2 * GAP;

check('labels within the pinned width keep it', segmentFit(ROOM, 200, [123, 114.906, 120.719], GAP, false, true), whole('even', 3));
check('a label past the pinned width shares the row by the labels', segmentFit(ROOM, 352, reclaim, GAP, false, true), whole('content', 3));
check('a row that fits exactly shares it by the labels', segmentFit(reclaimRow, 352, reclaim, GAP, false, true), whole('content', 3));
check(
  'half a pixel lost to a rounded clientWidth still fits',
  segmentFit(reclaimRow - 0.5, 352, reclaim, GAP, false, true),
  whole('content', 3),
);
check('a strip of one two pixels short turns into the dropdown', segmentFit(reclaimRow - 2, 352, reclaim, GAP, false, true), whole('list', 3));
check(
  'glyphs on every segment go alone before the dropdown',
  segmentFit(reclaimRow - 2, 352, reclaim, GAP, true, true),
  whole('squeezed', 3),
);
check(
  'a strip of several cuts the labels longer than an even share',
  segmentFit(610, 352, reclaim, GAP, false, false),
  { fit: 'truncated', cut: [false, true, true] },
);

// "Anfragen" is one word and 9px wider than its pinned segment. Pinned, it
// would run into the padding, so the row is shared by the labels.
const methods = [81.9062, 98.2031, 76.5156, 78.9062];
check('one word too long for its segment shares the row by the labels', segmentFit(ROOM, 89.28, methods, GAP, false, true), whole('content', 4));
check('and so it does in the card of an 800px window', segmentFit(365.6, 89.28, methods, GAP, false, true), whole('content', 4));
check('a hugging strip without a pinned width starts from its labels', segmentFit(ROOM, null, methods, GAP, false, true), whole('content', 4));
check('and turns into the dropdown once they do not fit', segmentFit(300, null, methods, GAP, false, true), whole('list', 4));

check('an empty strip has nothing to fit', segmentFit(ROOM, 200, [], GAP, false, true), whole('even', 0));

if (problems.length > 0) {
  console.error(`${problems.length} segment layout problem(s):`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}
console.log('ok: every strip keeps to its one row as it should');
