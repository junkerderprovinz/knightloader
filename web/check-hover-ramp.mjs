// GlimStone rule 21: hover moves UP the surface ramp.
//
// `--carbon-hover` (#353535) is the hover fill for something carrying no fill
// of its own - a transparent row on a card. It sits BELOW `--carbon-surface2`
// (#393939), so putting it on an element that is ALREADY filled with surface2
// makes that element four units darker under the pointer. It dims at the one
// moment somebody is looking straight at it, which reads as no hover at all.
// Anything already filled with surface2 hovers to `--carbon-surface3`
// (#525252), which is the step this repo's own secondary button takes.
//
// A script rather than a note, because the note existed: GlimStone's token
// table has said "hover on surface2" beside `--carbon-surface3` since the
// beginning, and thirteen places across three apps still had it the other way,
// one of them here. Prose that has already failed to stop a mistake does not
// stop it by being repeated.
//
// Run by hand or from CI: `node web/check-hover-ramp.mjs`.
//
// It looks inside each STRING LITERAL, not at lines. That distinction is the
// whole accuracy of it: a variant table writes one class list per line, so
//
//   secondary: 'bg-carbon-surface2 … hover:bg-carbon-surface3',
//   ghost:     'text-carbon-textSub hover:bg-carbon-hover …',
//
// has the fill and the hover on ADJACENT lines belonging to different
// variants, and a line window flags the ghost variant - which has no fill at
// all and is written exactly right. Both of this repo's near-misses were that
// shape.
//
// KNOWN LIMIT: a class list split across two literals joined with `+` is read
// as two, so a fill in one half and a hover in the other slips past.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const src = join(dirname(fileURLToPath(import.meta.url)), 'src');

const FILLED = 'bg-carbon-surface2';
const WRONG = 'hover:bg-carbon-hover';

function sources(dir) {
  const found = [];
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) found.push(...sources(path));
    else if (/\.tsx?$/.test(entry)) found.push(path);
  }
  return found;
}

const files = sources(src);
if (files.length < 20) {
  console.error(`check-hover-ramp: only ${files.length} source files found - wrong directory?`);
  process.exit(1);
}

/** Plain quoted strings, and the static halves of template strings. */
const QUOTED = /'(?:[^'\\\n]|\\.)*'|"(?:[^"\\\n]|\\.)*"/g;
const TEMPLATE = /`(?:[^`\\]|\\.)*`/g;

/** Every piece of text that is ONE class list, with where it starts. */
function classLists(text) {
  const pieces = [];
  for (const found of text.matchAll(QUOTED)) pieces.push([found[0], found.index]);
  for (const found of text.matchAll(TEMPLATE)) {
    // A template's `${…}` holds its own quoted strings, and QUOTED above has
    // already taken those one by one. What is left here is the static text
    // around them, which is a class list of its own.
    let at = found.index;
    for (const chunk of found[0].split(/\$\{[\s\S]*?\}/)) {
      pieces.push([chunk, at]);
      at += chunk.length;
    }
  }
  return pieces;
}

const problems = [];
for (const path of files) {
  const text = readFileSync(path, 'utf8');
  for (const [piece, at] of classLists(text)) {
    if (!piece.includes(WRONG) || !piece.includes(FILLED)) continue;
    const line = text.slice(0, at).split('\n').length;
    problems.push(`${path.slice(src.length + 1)}:${line}`);
  }
}
problems.sort();

if (problems.length) {
  console.error(`check-hover-ramp: ${problems.length} filled element(s) hovering to the tone BELOW their own.`);
  console.error('Use hover:bg-carbon-surface3 at:');
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}

console.log(`check-hover-ramp: ${files.length} files, every filled element hovers up the ramp.`);
