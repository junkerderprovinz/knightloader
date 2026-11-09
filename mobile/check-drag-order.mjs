// A row carried through a list on the Downloads screen lands where its leading
// edge says (GlimStone, "Reordering by dragging").
//
// A package with its files open is one block much taller than a closed one.
// Placed by its centre, it could never pass the last row of its band: the
// band's end stops the carry before the centre reaches the last row's middle.
// It passes a row once its edge in the direction of travel crosses that row's
// middle, so a block of any height reaches both ends of the band.
//
// The check feeds landeplatz in src/components/dragOrder.ts measured boxes of
// one band with a package open in it.
//
// Run by CI and by hand: `node mobile/check-drag-order.mjs`.
import { registerHooks } from 'node:module';

// The app imports its own modules without an extension, which Metro resolves
// and Node does not, so a relative import without one is tried as a .ts file.
const src = new URL('./src/', import.meta.url).href;
registerHooks({
  resolve(specifier, context, next) {
    const ours = context.parentURL?.startsWith(src);
    if (ours && /^\.\.?\//.test(specifier) && !/\.\w+$/.test(specifier)) return next(`${specifier}.ts`, context);
    return next(specifier, context);
  },
});

const { landeplatz, spielraum } = await import(new URL('./src/components/dragOrder.ts', import.meta.url).href);

const problems = [];
const check = (what, got, want) => {
  if (got !== want) problems.push(`${what}: landed on row ${got}, want row ${want}`);
};

// Four packages, 60 points tall with 8 between them; the second one is open
// and its two files hang under it.
const rows = [
  { key: 'A', band: 'q' },
  { key: 'B', band: 'q' },
  { key: 'b1', band: 'B', parent: 'B' },
  { key: 'b2', band: 'B', parent: 'B' },
  { key: 'C', band: 'q' },
  { key: 'D', band: 'q' },
];
const kaesten = Object.fromEntries(rows.map((_, i) => [i, { y: i * 68, h: 60 }]));
const [A, B, C, D] = [0, 1, 4, 5];

const [oben, unten] = spielraum(kaesten, rows, B);
check('the open package carried to the band end reaches the last row', landeplatz(kaesten, rows, B, unten), D);
check('and carried to its top reaches the first', landeplatz(kaesten, rows, B, oben), A);
check('a short carry leaves it in place', landeplatz(kaesten, rows, B, 20), B);
check('a closed package passes the next once its bottom is past that one’s middle', landeplatz(kaesten, rows, C, 40), D);
check('but not before', landeplatz(kaesten, rows, C, 30), C);
check('carried up, its top edge decides', landeplatz(kaesten, rows, D, -40), C);

if (problems.length > 0) {
  for (const p of problems) console.error(`✗ ${p}`);
  process.exit(1);
}
console.log('ok: a carried row lands where its leading edge says');
