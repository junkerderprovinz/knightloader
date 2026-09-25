// How a horizontal selector's pinned segments share their room.
//
// Every segment of a well is pinned to the width of the longest label, within
// a floor and a ceiling. A label past the ceiling would wrap onto two lines
// inside its segment while the short ones sit in wide, empty segments beside
// it, with the rest of the card free. So a selector with such a label spreads
// over its box, each segment as wide as its label, as long as they fit on one
// row, and one whose labels fit their pinned width keeps it.
//
// The widths are what Chrome measured on the settings pages, each segment at
// max-content and at min-content, in cards 860 pixels wide less the track's
// padding: the reclaim modes and the collision choices in German, and the
// reconnect methods, a small well at 7.44em of 12px in German and 8.68em in
// Japanese.
//
// Run: `node web/check-segment-layout.mjs`. Node reads the TypeScript module
// directly; the measuring stays in Tabs.tsx.
import { perRowFor, segmentLayout } from './src/components/segmentLayout.ts';

const problems = [];

function check(what, got, want) {
  const a = JSON.stringify(got);
  const b = JSON.stringify(want);
  if (a !== b) problems.push(`${what}\n      got  ${a}\n      want ${b}`);
}

const GAP = 3.2;
const ROOM = 853.6;
const pinned = (perRow) => ({ byContent: false, perRow });
const byContent = (count) => ({ byContent: true, perRow: count });
const seg = (oneLine, narrowest = oneLine) => ({ oneLine, narrowest });

// "Ein Eintrag, dass dieser Download sie geschrieben hat" is 377px on one line
// against the 352px ceiling.
const reclaim = [seg(198.938, 101.219), seg(377.266, 105), seg(253.562, 82.9219)];
const reclaimRow = 198.938 + 377.266 + 253.562 + 2 * GAP;

check('a label past the pinned width spreads the row', segmentLayout(ROOM, 352, reclaim, GAP), byContent(3));
check('so it does in a wider card', segmentLayout(1333.6, 352, reclaim, GAP), byContent(3));
check('a row that fits exactly spreads', segmentLayout(reclaimRow, 352, reclaim, GAP), byContent(3));
check(
  'half a pixel lost to a rounded clientWidth still spreads',
  segmentLayout(reclaimRow - 0.5, 352, reclaim, GAP),
  byContent(3),
);
check('a row two pixels short wraps at the pinned width', segmentLayout(reclaimRow - 2, 352, reclaim, GAP), pinned(2));
check('a card too narrow for the row wraps as before', segmentLayout(693.6, 352, reclaim, GAP), pinned(1));

const collision = [seg(123, 82.4375), seg(114.906), seg(120.719)];
check('labels within the pinned width keep it', segmentLayout(ROOM, 200, collision, GAP), pinned(3));
check('and keep it in a card too narrow for one row', segmentLayout(365.6, 200, collision, GAP), pinned(1));

// "Anfragen" is one word and 9px wider than its segment, so it runs into the
// padding; it has nowhere to break.
const methods = [seg(81.9062), seg(98.2031), seg(76.5156), seg(78.9062)];
check('one word too long for its segment keeps the pinned widths', segmentLayout(ROOM, 89.28, methods, GAP), pinned(4));
check('and so does the wrapped row of an 800px window', segmentLayout(365.6, 89.28, methods, GAP), pinned(2));

// Japanese breaks between any two characters, so "リクエスト" wraps.
const methodsJa = [seg(94, 58), seg(106, 58), seg(77.0312), seg(105.531, 58)];
check('a label without spaces that can break counts as wrapping', segmentLayout(ROOM, 104.16, methodsJa, GAP), byContent(4));

check('an empty strip has nothing to spread', segmentLayout(ROOM, 200, [], GAP), pinned(0));

// The even rows a pinned selector wraps into (GlimStone, "A selector that wraps
// fills its box").
check('all of them share a row while they fit', perRowFor(3, ROOM, 200, GAP), 3);
check('six that fit four to a row go three and three', perRowFor(6, 820, 200, GAP), 3);
check('seven go four and three', perRowFor(7, 820, 200, GAP), 4);
check('five that fit two to a row go two, two and one', perRowFor(5, 420, 200, GAP), 2);
check('a room narrower than one segment still holds one', perRowFor(3, 150, 200, GAP), 1);

if (problems.length > 0) {
  console.error(`${problems.length} segment layout problem(s):`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}
console.log('ok: pinned segments share their room as they should');
