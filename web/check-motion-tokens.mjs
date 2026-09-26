// Every motion level declares every dial, and every dial a rule reads is one.
//
// The motion engine in src/index.css is one token set read by one set of
// keyframes, so less motion is a smaller number and never a second animation.
// A dial is a custom property the top level, the bare :root inside the
// no-preference gate, declares: --motion-*, --glim-toast-slide and the drag
// lift's scale and slide. The other three levels are
// `:root[data-motion="subtle"]` and `:root[data-motion="off"]` inside the gate
// and `:root[data-motion="storm"]` outside it (check-hidden-motion-level.mjs
// says why). Each of them has to declare every dial the top level does, and no
// dial of its own.
//
// A dial a level forgets fails silently. It still resolves, falling through to
// the top level's number: "off" keeps the 18px page slide, and the storm, which
// asks for more motion, quietly runs the top level's gentler one. A dial that
// is set but never read at a level, because a substitute rule replaces the
// animation there, is declared all the same, so the next rule that reads it
// cannot fall through either.
//
// A token a rule reads that the top level never declares makes its whole
// declaration invalid at computed-value time, so
// `animation: glim-toast-in var(--motion-toast-dur) ...` is no animation at all
// and the element simply appears. A read with a fallback, `var(--x, 0ms)`, is
// allowed, since it names what happens without the dial.
//
// --egg-* tokens are an easter egg's own numbers, read at any level, and are
// not dials.
//
// Not seen: values, so a "subtle" number larger than the "wild" one passes; a
// token spent in an inline style in a .tsx file, since only src/index.css is
// read; and the OS-level (prefers-reduced-motion: reduce) block, which
// hard-codes its values.
//
// Run from web/: `node check-motion-tokens.mjs`
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const rel = 'src/index.css';
const file = join(dirname(fileURLToPath(import.meta.url)), rel);
// Comments blanked, offsets kept byte for byte, so line numbers stay right and
// a token merely named in a paragraph does not count.
const css = readFileSync(file, 'utf8').replace(/\/\*[\s\S]*?\*\//g, (c) => c.replace(/[^\n]/g, ' '));

const lineAt = (i) => css.slice(0, i).split('\n').length;
const DIAL = /^--(?:motion-[\w-]+|glim-toast-slide|drag-lift-scale|drag-settle-dur)$/;
/** A read, with the comma that marks `var(--x, fallback)`. */
const READ = /var\(\s*(--(?:motion-[\w-]+|glim-toast-slide|drag-lift-scale|drag-settle-dur))\s*(,?)/g;

const die = (why) => {
  console.error(`check-motion-tokens: ${why}`);
  process.exit(1);
};

/** The [start, end) of the block whose `{` is at `at`, braces counted. */
function block(at) {
  let depth = 0;
  for (let i = at; i < css.length; i++) {
    if (css[i] === '{') depth++;
    else if (css[i] === '}' && --depth === 0) return [at + 1, i];
  }
  return die(`unclosed block at ${rel}:${lineAt(at)}`);
}

/** Top-level `selector { ... }` rules between two offsets, quotes normalised. */
function rulesIn(from, to) {
  const out = [];
  let i = from;
  while (i < to) {
    const open = css.indexOf('{', i);
    if (open < 0 || open >= to) break;
    const [bs, be] = block(open);
    out.push({ selector: css.slice(i, open).trim().replace(/\s+/g, ' ').replace(/'/g, '"'), at: open, text: css.slice(bs, be) });
    i = be + 1;
  }
  return out;
}

const declared = (text) => new Set([...text.matchAll(/(--[\w-]+)\s*:/g)].map((d) => d[1]).filter((d) => DIAL.test(d)));

const gate = css.indexOf('@media (prefers-reduced-motion: no-preference)');
if (gate < 0) die('the no-preference gate is gone from index.css, and every dial with it');
const [gs, ge] = block(css.indexOf('{', gate));

const LEVEL = { ':root': 'wild', ':root[data-motion="subtle"]': 'subtle', ':root[data-motion="off"]': 'off' };
const levels = {};
for (const rule of rulesIn(gs, ge)) {
  const level = LEVEL[rule.selector];
  if (!level) continue;
  if (levels[level]) die(`${rel}:${lineAt(rule.at)} a second ${rule.selector} block inside the gate; one block per level keeps its dials in one list`);
  levels[level] = { dials: declared(rule.text), line: lineAt(rule.at) };
}

// The storm's block: the bare selector outside every @media, holding only
// custom properties. check-hidden-motion-level.mjs holds it to that shape.
for (const m of css.matchAll(/:root\[data-motion=["']storm["']\]\s*\{/g)) {
  const open = css.indexOf('{', m.index);
  const body = css.slice(...block(open));
  if (body.replace(/--[\w-]+\s*:[^;]*;/g, '').trim() !== '') continue;
  if (levels.storm) die(`${rel}:${lineAt(m.index)} a second storm token block; one block per level keeps its dials in one list`);
  levels.storm = { dials: declared(body), line: lineAt(m.index) };
}

for (const level of ['wild', 'subtle', 'off', 'storm']) {
  if (!levels[level]) die(`the ${level} level has no block of its own in ${rel}, so it has no numbers`);
}
const dials = levels.wild.dials;
if (dials.size < 25) die(`only ${dials.size} dials declared by the top level, which is too few to be this file`);

const problems = [];
for (const level of ['subtle', 'off', 'storm']) {
  const { dials: own, line } = levels[level];
  for (const dial of dials) {
    if (!own.has(dial)) {
      problems.push(`${rel}:${line} the ${level} level never declares ${dial}, so it runs the top level's number`);
    }
  }
  for (const dial of own) {
    if (!dials.has(dial)) {
      problems.push(`${rel}:${line} the ${level} level declares ${dial}, which the top level does not: a level is a different number, never a dial of its own`);
    }
  }
}

let reads = 0;
const seen = new Set();
for (const m of css.matchAll(READ)) {
  reads++;
  if (m[2] === ',' || dials.has(m[1]) || seen.has(m[1])) continue;
  seen.add(m[1]);
  problems.push(`${rel}:${lineAt(m.index)} reads ${m[1]}, which no level declares: the declaration is invalid and the element does not animate at all`);
}

if (problems.length) {
  console.error(`check-motion-tokens: ${problems.length} problem(s) with the motion dials.`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}

console.log(`ok: ${dials.size} dials, each declared at all four motion levels, and ${reads} reads of them in ${rel}`);
