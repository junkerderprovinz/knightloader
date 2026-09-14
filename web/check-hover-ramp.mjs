// GlimStone rule 21: hover moves UP the surface ramp.
//
// `--carbon-hover` (#353535) is the hover fill for something carrying no fill
// of its own - a transparent row on a card. It sits BELOW `--carbon-surface2`
// (#393939), so putting it on an element that is ALREADY filled with surface2
// makes that element four units darker under the pointer. It dims at the one
// moment somebody is looking straight at it, which reads as no hover at all.
// Anything already filled with surface2 hovers to `--carbon-surface3`
// (#525252), which is the step this repo's own secondary button takes, and
// anything filled with surface3 to `--carbon-hover-raised` (#6f6f6f), which was
// added for exactly that: the ramp used to stop at surface3, so the controls
// sitting ON it reached back down to `--carbon-hover` - a 29-unit drop.
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

/** Each resting fill, and the hover it is allowed to take (rule 21). */
const RAMP = [
  { filled: 'bg-carbon-surface2', hover: 'hover:bg-carbon-surface3' },
  { filled: 'bg-carbon-surface3', hover: 'hover:bg-carbon-hoverRaised' },
];
// Anchored, because `hover:bg-carbon-hoverRaised` CONTAINS
// `hover:bg-carbon-hover`: a plain substring test reports every correctly
// written surface3 control as the very mistake it avoids.
const WRONG = /hover:bg-carbon-hover(?![A-Za-z-])/;

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

// THE SECOND SHAPE, and the reason this file grew a second half: a row that
// publishes its own ground as a CUSTOM PROPERTY names no `bg-carbon-*` class at
// all, so everything above is blind to it. The download list's folder header is
// written exactly that way -
//
//   [--row-ground:color-mix(in_srgb,var(--carbon-surface2)_80%,var(--carbon-surface))]
//   hover:[--row-ground:var(--carbon-surface2)]
//
// - and that is rule 21 broken in the one form its own guard could not see: a
// row resting ON the surface2 tier, hovering to surface2 flat, which is a step
// of 4/255 in the dark theme (measured on a running instance: rgb(53,53,53) at
// rest, rgb(57,57,57) under the pointer, ΔL* 1.8 against the link row's own
// 3.8 plain and 12.3 in Rainbow). jdp saw it as no hover at all: "bei den
// ordnern sieht man es kaum".
//
// The tier of a value is the HIGHEST ramp token it names, so a mix reading
// "surface2 80%, surface" counts as surface2 - the tone it is mostly made of.
// That is an approximation in one direction only (a 10% mix would be reported
// as its strong token too), and it is the safe direction: it can ask for a
// bigger step than strictly needed, never for a smaller one.
const TIERS = [
  [/--carbon-hover-raised|--carbon-hoverRaised/, 4, '--carbon-hover-raised'],
  [/--carbon-surface3/, 3, '--carbon-surface3'],
  [/--carbon-surface2/, 2, '--carbon-surface2'],
  [/--carbon-hover(?![A-Za-z-])/, 1, '--carbon-hover'],
];
/** The ramp tier a ground expression sits on. 0 is "the card, or nothing". */
function tierOf(value, lists) {
  // One level of indirection: a row may hover to `var(--row-hover)` and define
  // `--row-hover` in the same class list. Following it is what keeps the LINK
  // row (transparent at rest, --carbon-hover at half alpha under the pointer)
  // read as the tier it actually paints instead of as tier 0.
  const seen = new Set();
  let v = value;
  for (let i = 0; i < 4; i++) {
    const ref = /^var\(\s*(--[A-Za-z0-9-]+)\s*\)$/.exec(v.trim());
    if (!ref || seen.has(ref[1]) || TIERS.some(([re]) => re.test(v))) break;
    seen.add(ref[1]);
    const next = lists.get(ref[1]);
    if (next === undefined) break;
    v = next;
  }
  for (const [re, rank, name] of TIERS) if (re.test(v)) return [rank, name];
  return [0, 'the card'];
}

/** Every `[--name:value]` in a class list, by name, rest and raised apart. */
function grounds(piece) {
  const rest = new Map();
  const raised = [];
  // `[--name:value]`, optionally behind variants (`hover:`, `has-[:x]:`). The
  // value may hold brackets of its own - color-mix() does not, but var() plus a
  // fallback would - so the scan counts them rather than stopping at the first
  // `]`.
  const re = /(^|\s)((?:[A-Za-z0-9:_.-]+(?:\[[^\]]*\])?:)*)\[(--[A-Za-z0-9-]+):/g;
  let m;
  while ((m = re.exec(piece)) !== null) {
    let depth = 1;
    let i = re.lastIndex;
    for (; i < piece.length && depth > 0; i++) {
      if (piece[i] === '[' || piece[i] === '(') depth++;
      else if (piece[i] === ']' || piece[i] === ')') depth--;
    }
    const value = piece.slice(re.lastIndex, i - 1).replace(/_/g, ' ');
    const variants = m[2];
    if (variants === '') rest.set(m[3], value);
    else if (/(^|:)hover:|focus-visible/.test(variants)) raised.push([m[3], value, variants]);
  }
  return { rest, raised };
}

const problems = [];
for (const path of files) {
  const text = readFileSync(path, 'utf8');
  for (const [piece, at] of classLists(text)) {
    const line = () => text.slice(0, at).split('\n').length;
    if (WRONG.test(piece)) {
      const tier = RAMP.find((t) => piece.includes(t.filled));
      if (tier) problems.push(`${path.slice(src.length + 1)}:${line()} -> ${tier.hover}`);
    }
    const { rest, raised } = grounds(piece);
    for (const [name, value, variants] of raised) {
      if (!rest.has(name)) continue;
      const [resting, restName] = tierOf(rest.get(name), rest);
      const [up, upName] = tierOf(value, rest);
      if (up > resting) continue;
      problems.push(
        `${path.slice(src.length + 1)}:${line()} -> ${variants}[${name}:…] rests on ${restName} and ` +
          `rises to ${upName}: a row filled with one tone has to hover to the tone ABOVE it`,
      );
    }
  }
}
problems.sort();

if (problems.length) {
  console.error(`check-hover-ramp: ${problems.length} filled element(s) hovering to the tone BELOW their own.`);
  console.error("Each line names the class it should carry instead:");
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}

console.log(`check-hover-ramp: ${files.length} files, every filled element hovers up the ramp.`);
