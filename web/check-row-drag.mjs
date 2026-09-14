// The download list's drag arithmetic, on plain numbers.
//
// Three faults are guarded here, all of them reported from a chair rather than
// found in a test (jdp, 2026-09-13: "das drag and drop von ordern funktioniert
// nicht gut. das live verschieben geht nicht"):
//
//   1. A FOLDER AIMS AT WHOLE FOLDERS, NOT AT THEIR HEADERS. A folder header is
//      44px of a folder that is three hundred pixels tall once it is open, so
//      aiming at headers only left the other rows standing still for nearly the
//      whole gesture - the drag looked dead until the pointer happened to land
//      on another header.
//   2. WHICH HALF decides before-or-after is the half of the whole folder, for
//      the same reason.
//   3. The preview and the drop are the SAME splice, so a block lands where the
//      preview showed it.
//
// A fourth arrived with the JDownloader gesture (jdp, 2026-09-14: "wenn man
// einmal draufklickt markiert man, dann ein zweiter klick den man hält und man
// kann per drag and drop verschieben"): a move now carries THE WHOLE SELECTION,
// so the block is several units and the rows it holds are not landing places
// for themselves. selectedBlock decides what travels and aimAt takes the block
// rather than one row.
//
// Run by hand or from CI: `node web/check-row-drag.mjs`. Node reads the
// TypeScript module directly; nothing here touches React or a DOM, which is
// why the arithmetic was worth lifting out of the component in the first place.
import {
  aimAt,
  pastThreshold,
  previewOrder,
  selectedBlock,
  stackOffsets,
  GESTURE_THRESHOLD_PX,
} from './src/components/rowDrag.ts';

let failed = 0;
function check(name, got, want) {
  const a = JSON.stringify(got);
  const b = JSON.stringify(want);
  if (a === b) return;
  failed++;
  console.error(`FAIL  ${name}\n        got:  ${a}\n        want: ${b}`);
}

// Two folders, drawn one after the other, the shape the report came from: a
// folder header is taller than a link row, and an open folder is many rows.
//
//   Alpha  header  0..44,  links 44..80, 80..116, 116..152
//   Bravo  header  152..196, links 196..232, 232..268
const task = (id) => ({ kind: 'task', id });
const pkg = (name) => ({ kind: 'package', name });
const slots = [
  { unit: pkg('Alpha'), top: 0, bottom: 44 },
  { unit: task('a1'), top: 44, bottom: 80 },
  { unit: task('a2'), top: 80, bottom: 116 },
  { unit: task('a3'), top: 116, bottom: 152 },
  { unit: pkg('Bravo'), top: 152, bottom: 196 },
  { unit: task('b1'), top: 196, bottom: 232 },
  { unit: task('b2'), top: 232, bottom: 268 },
];
// The same list with a third folder under it, for the tests that need a folder
// the moved block does not already contain.
//
//   Charlie header 268..312, links 312..348, 348..384
const slots3 = [
  ...slots,
  { unit: pkg('Charlie'), top: 268, bottom: 312 },
  { unit: task('c1'), top: 312, bottom: 348 },
  { unit: task('c2'), top: 348, bottom: 384 },
];
const owner = { a1: 'Alpha', a2: 'Alpha', a3: 'Alpha', b1: 'Bravo', b2: 'Bravo', c1: 'Charlie', c2: 'Charlie' };
const ctx = { packageOf: (id) => owner[id], canTarget: () => true };
const flat = ['a1', 'a2', 'a3', 'b1', 'b2'];

// --- 1. A folder dragged over another folder's LINKS aims at that folder ----
//
// y=60 is the first link of Alpha. Alpha's own extent is 0..152, so the pointer
// is in its upper half and the drag lands BEFORE it. Aiming at headers only
// answered "after Alpha" here (60 is far below the header's own midpoint at 22),
// which is where Bravo already sits - so nothing moved, all the way down.
check('folder over the links of another folder', aimAt(slots, 60, [pkg('Bravo')], ctx), {
  target: pkg('Alpha'),
  after: false,
});
check('and the lower half of that same folder', aimAt(slots, 140, [pkg('Bravo')], ctx), {
  target: pkg('Alpha'),
  after: true,
});
// The header itself still works, and still by the folder's half, not its own.
check('folder over another folder header', aimAt(slots, 20, [pkg('Bravo')], ctx), {
  target: pkg('Alpha'),
  after: false,
});

// --- 2. A link keeps aiming at single rows --------------------------------
check('link over a link, upper half', aimAt(slots, 50, [task('b1')], ctx), {
  target: task('a1'),
  after: false,
});
check('link over a link, lower half', aimAt(slots, 76, [task('b1')], ctx), {
  target: task('a1'),
  after: true,
});

// --- 3. A row the queue cannot move is not a landing place ----------------
//
// Over Alpha's first link with Alpha refused, and Bravo is the block in flight:
// the only folder left to answer with is Charlie, far below. This used to
// answer "Bravo, before" - the block aiming at itself - which stopped being a
// legal answer when the block stopped being a landing place for itself.
const noAlpha = { ...ctx, canTarget: (u) => !(u.kind === 'package' && u.name === 'Alpha') };
check('a folder with nothing movable in it is skipped', aimAt(slots3, 60, [pkg('Bravo')], noAlpha), {
  target: pkg('Charlie'),
  after: false,
});

// --- 4. THE BLOCK IS NOT A LANDING PLACE FOR ITSELF -----------------------
//
// The whole selection travels now, so the rows under the pointer are usually
// the rows being carried. Left in the list of targets, the aim answers "onto
// yourself" for the whole middle of the gesture, previewOrder returns null for
// that, and the list stands still - the same dead drag the folder aim was
// written against, arriving from the other side.
check('a moved link is not a target', aimAt(slots, 130, [task('a1'), task('a3')], ctx), {
  target: task('a2'),
  after: true,
});
// Every row of a moved FOLDER is out too, header and links alike: the folder is
// travelling whole, so none of its pixels is a place to land.
check('no row of a moved folder is a target', aimAt(slots, 100, [pkg('Alpha')], ctx), {
  target: pkg('Bravo'),
  after: false,
});
// A block of folders still aims at whole folders - and at neither of its own.
check('two folders aim at the third', aimAt(slots3, 60, [pkg('Alpha'), pkg('Bravo')], ctx), {
  target: pkg('Charlie'),
  after: false,
});
// A mixed block (a folder and a loose link) aims at single rows, because that
// is the finer of the two and the one a link needs.
check('a mixed block aims at rows', aimAt(slots3, 330, [pkg('Alpha'), task('b1')], ctx), {
  target: task('c1'),
  after: false,
});
check('a block that holds everything has nowhere to go', aimAt(slots, 100, [pkg('Alpha'), pkg('Bravo')], ctx), null);

// --- 5. What travels when the pointer goes down on a selected row ---------
//
// A FOLDER WHOSE LINKS ARE ALL SELECTED TRAVELS AS A FOLDER, and its links do
// not travel a second time on their own. That is the answer to "zieht ein
// markierter Ordner seine Links mit": clicking a folder header puts every one
// of its links in the selection, so the folder is selected exactly when its
// links are, and it moves as one block that keeps its own internal order.
const drawn = [
  { unit: pkg('Alpha'), ids: ['a1', 'a2', 'a3'] },
  { unit: task('a1'), ids: ['a1'] },
  { unit: task('a2'), ids: ['a2'] },
  { unit: task('a3'), ids: ['a3'] },
  { unit: pkg('Bravo'), ids: ['b1', 'b2'] },
  { unit: task('b1'), ids: ['b1'] },
  { unit: task('b2'), ids: ['b2'] },
];
check('a fully selected folder travels as one unit', selectedBlock(drawn, new Set(['a1', 'a2', 'a3', 'b1'])), [
  pkg('Alpha'),
  task('b1'),
]);
check('a half selected folder sends its links loose', selectedBlock(drawn, new Set(['a2', 'a3'])), [
  task('a2'),
  task('a3'),
]);
check('nothing selected carries nothing', selectedBlock(drawn, new Set()), []);
// A folder with no movable link of its own is not a unit - the caller hands in
// the MOVABLE ids, so an empty list means there is nothing here to carry.
const withDead = [{ unit: pkg('Dead'), ids: [] }, { unit: task('d1'), ids: ['d1'] }];
check('a folder with nothing movable is not a unit', selectedBlock(withDead, new Set(['d1'])), [task('d1')]);
// Drawn order and nothing else: the block is spliced in as one contiguous run,
// so the order it is collected in is the order it lands in.
check('the block keeps the order the list draws', selectedBlock(drawn, new Set(['b2', 'a2'])), [task('a2'), task('b2')]);

// --- 6. When a press becomes a gesture ------------------------------------
check('the threshold is five pixels', GESTURE_THRESHOLD_PX, 5);
check('a press that has not moved is still a click', pastThreshold(0, 0), false);
check('four pixels is still a click', pastThreshold(-4, 4), false);
check('five pixels down is a gesture', pastThreshold(0, 5), true);
check('five pixels sideways is a gesture too', pastThreshold(-5, 0), true);

// --- 7. The preview is the splice the drop makes --------------------------
check('folder lands before the folder it was aimed at', previewOrder(flat, ['b1', 'b2'], ['a1', 'a2', 'a3'], false), [
  'b1',
  'b2',
  'a1',
  'a2',
  'a3',
]);
check('folder lands after it', previewOrder(flat, ['a1', 'a2', 'a3'], ['b1', 'b2'], true), [
  'b1',
  'b2',
  'a1',
  'a2',
  'a3',
]);
check('a drop on its own footprint is not a move', previewOrder(flat, ['b1', 'b2'], ['a1', 'a2', 'a3'], true), null);
check('a drop on itself is not a move', previewOrder(flat, ['b1', 'b2'], ['b1'], false), null);
check('one link across a folder boundary', previewOrder(flat, ['a1'], ['b1'], false), ['a2', 'a3', 'a1', 'b1', 'b2']);
// A selection scattered across the list gathers at the drop point as one run -
// what the whole-selection move promises, on the same splice.
check('a scattered selection lands as one run', previewOrder(flat, ['a1', 'b1'], ['a3'], true), [
  'a2',
  'a3',
  'a1',
  'b1',
  'b2',
]);

// --- 8. The rows slide to exactly the boxes the snapshot measured ---------
const key = (u) => (u.kind === 'task' ? `task:${u.id}` : `pkg:${u.name}`);
const wanted = ['pkg:Bravo', 'task:b1', 'task:b2', 'pkg:Alpha', 'task:a1', 'task:a2', 'task:a3'];
const offsets = stackOffsets(slots, wanted, key);
check('the moved folder header slides to the top of the list', offsets?.get('pkg:Bravo'), -152);
check('the folder it passed slides down by the moved block', offsets?.get('pkg:Alpha'), 116);
check('every drawn row gets an offset', offsets?.size, slots.length);
check('a snapshot that no longer describes the list gives up whole', stackOffsets(slots, wanted.slice(1), key), null);

if (failed > 0) {
  console.error(`\n${failed} drag check(s) failed.`);
  process.exit(1);
}
console.log('row drag: aim, block, threshold, preview and offsets agree.');
