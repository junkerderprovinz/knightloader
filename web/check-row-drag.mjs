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
// Run by hand or from CI: `node web/check-row-drag.mjs`. Node reads the
// TypeScript module directly; nothing here touches React or a DOM, which is
// why the arithmetic was worth lifting out of the component in the first place.
import { aimAt, previewOrder, stackOffsets } from './src/components/rowDrag.ts';

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
const owner = { a1: 'Alpha', a2: 'Alpha', a3: 'Alpha', b1: 'Bravo', b2: 'Bravo', c1: 'Charlie' };
const ctx = { packageOf: (id) => owner[id], canTarget: () => true };
const flat = ['a1', 'a2', 'a3', 'b1', 'b2'];

// --- 1. A folder dragged over another folder's LINKS aims at that folder ----
//
// y=60 is the first link of Alpha. Alpha's own extent is 0..152, so the pointer
// is in its upper half and the drag lands BEFORE it. Aiming at headers only
// answered "after Alpha" here (60 is far below the header's own midpoint at 22),
// which is where Bravo already sits - so nothing moved, all the way down.
check('folder over the links of another folder', aimAt(slots, 60, pkg('Bravo'), ctx), {
  target: pkg('Alpha'),
  after: false,
});
check('and the lower half of that same folder', aimAt(slots, 140, pkg('Bravo'), ctx), {
  target: pkg('Alpha'),
  after: true,
});
// The header itself still works, and still by the folder's half, not its own.
check('folder over another folder header', aimAt(slots, 20, pkg('Bravo'), ctx), {
  target: pkg('Alpha'),
  after: false,
});

// --- 2. A link keeps aiming at single rows --------------------------------
check('link over a link, upper half', aimAt(slots, 50, task('b1'), ctx), {
  target: task('a1'),
  after: false,
});
check('link over a link, lower half', aimAt(slots, 76, task('b1'), ctx), {
  target: task('a1'),
  after: true,
});

// --- 3. A row the queue cannot move is not a landing place ----------------
const noAlpha = { ...ctx, canTarget: (u) => !(u.kind === 'package' && u.name === 'Alpha') };
check('a folder with nothing movable in it is skipped', aimAt(slots, 60, pkg('Bravo'), noAlpha), {
  target: pkg('Bravo'),
  after: false,
});

// --- 4. The preview is the splice the drop makes --------------------------
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

// --- 5. The rows slide to exactly the boxes the snapshot measured ---------
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
console.log('row drag: aim, preview and offsets agree.');
