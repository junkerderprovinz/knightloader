// Every motion token a keyframe or a utility class reads is set at all three
// motion intensities, or is deliberately not set and has a substitute rule.
//
// WHAT BREAKS WITHOUT IT. The motion engine in src/index.css is built as one
// token set read by one set of keyframes, so "less motion" is a smaller number
// and never a second animation. A dropped token in that arrangement fails in
// two ways, and both are silent. A token no block defines at all makes its
// whole declaration invalid at computed-value time, so
// `animation: glim-toast-in var(--motion-toast-dur) ...` is not a slower toast
// and not a faster one, it is no animation whatsoever: the element appears.
// A token only the "off" block forgets is the quieter half, because it still
// resolves. It falls through to the :root defaults, and those defaults ARE the
// full intensity, so a reader who set Motion to off keeps the 18px page slide
// and the 24px toast throw. Neither shows up in a build, a type check or a
// screenshot, and both look in an editor exactly like the correct file.
//
// WHY THIS IS A SCRIPT AND NOT ANOTHER PARAGRAPH. index.css says the rule
// already, at length, in the section header above the engine. The rule is not
// what is hard here. The three blocks are three flat lists roughly fifty lines
// apart, they are NOT meant to be identical, and today they genuinely differ in
// five places on purpose: "off" leaves the shake and pulse tokens alone because
// it replaces both animations outright, and "subtle" leaves the pulse period
// alone because an infinite ambient loop cannot be calmed by running it faster.
// So "these lists do not line up" is the normal, correct state of this file,
// and an eye reading down them has no way to tell the fifth deliberate gap from
// the first accidental one. Prose cannot mark which gaps are intended. This can,
// because it does not take the gaps on trust: it goes and finds the substitute.
//
// HOW IT DECIDES A GAP IS DELIBERATE, which is the whole accuracy of it. A
// token missing from an intensity is forgiven only when every class that reads
// it is switched off at that intensity by a `:root[data-motion="..."]` rule of
// its own, which is exactly the mechanism the file uses: .glim-live and
// .glim-shake read no token under "off" because they are handed a different
// animation there. Delete that substitute rule and the exemption stops holding
// the same second, which is the coupling worth having. The one exemption not
// derived from a substitute rule is the period of an `infinite` animation under
// "subtle", and it is narrow on purpose: it covers the duration named in the
// animation shorthand and nothing the keyframes read, so the pulse's amplitude
// is still required to come down at "subtle" even though its period may stand.
//
// WHAT IT DOES NOT SEE. It reads src/index.css and nothing else, so a token
// spent in an inline style in a .tsx file is invisible to it. It judges
// presence and never value, so a "subtle" number larger than the "full" one
// passes without comment. It says nothing about the OS-level
// (prefers-reduced-motion: reduce) block, which hard-codes every value on
// purpose and must keep doing so. It does not model the universal
// `transition-duration: 0s !important` belt as anything but a substitute for
// transitions. It ignores --dir-sign, which is a layout fact deliberately kept
// outside the motion gate. And it does not look for the opposite defect, a
// token defined and read by nobody: two exist today, --motion-page-slide-scale
// and --motion-toast-slide-scale, left behind when the toast moved to
// --glim-toast-slide, and they cost a line each and nothing else.
//
// Run by hand from web/: `node check-motion-tokens.mjs`
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const rel = 'src/index.css';
const file = join(dirname(fileURLToPath(import.meta.url)), rel);
// Comments blanked, offsets kept byte for byte, so every reported line is the
// real one AND a token merely NAMED in a paragraph is not read by anything.
// The engine's own prose names half these tokens while explaining them.
const css = readFileSync(file, 'utf8').replace(/\/\*[\s\S]*?\*\//g, (c) => c.replace(/[^\n]/g, ' '));

const lineAt = (i) => css.slice(0, i).split('\n').length;
/** A read, with the comma that marks `var(--x, fallback)`. */
const READ = /var\(\s*(--motion-[\w-]*|--glim-toast-slide)\s*(,?)/g;

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

/** Top-level `selector { ... }` rules between two offsets. */
function rulesIn(from, to) {
  const out = [];
  let i = from;
  while (i < to) {
    const open = css.indexOf('{', i);
    if (open < 0 || open >= to) break;
    const head = css.slice(i, open);
    const [bs, be] = block(open);
    out.push({
      selector: head.trim().replace(/\s+/g, ' '),
      at: i + head.length - head.trimStart().length,
      bs,
      text: css.slice(bs, be),
    });
    i = be + 1;
  }
  return out;
}

const gate = css.indexOf('@media (prefers-reduced-motion: no-preference)');
if (gate < 0) die('the no-preference gate is gone from index.css, and every token with it');
const [gs, ge] = block(css.indexOf('{', gate));

const TIER_SELECTOR = { ':root': 'full', ':root[data-motion="subtle"]': 'subtle', ':root[data-motion="off"]': 'off' };
const defs = { full: new Set(), subtle: new Set(), off: new Set() };
const tierAt = {};
/** Rules that read tokens, and the keyframe each one plays. */
const users = [];
/** `:root[data-motion="x"] .cls` rules, which is how an intensity opts out. */
const substitutes = { subtle: [], off: [] };

for (const rule of rulesIn(gs, ge)) {
  const tier = TIER_SELECTOR[rule.selector];
  if (tier) {
    tierAt[tier] = lineAt(rule.at);
    for (const d of rule.text.matchAll(/(--[\w-]+)\s*:/g)) defs[tier].add(d[1]);
    continue;
  }
  const classes = [...new Set((rule.selector.match(/\.[A-Za-z][\w-]*/g) || []).map((c) => c.slice(1)))];
  const forced = rule.selector.match(/\[data-motion="(subtle|off)"\]/);
  if (forced) {
    substitutes[forced[1]].push({
      // A rule with no class of its own is the universal belt, `… * { }`.
      classes: classes.length ? classes : ['*'],
      animation: /animation\s*:/.test(rule.text),
      transition: /transition(-duration)?\s*:/.test(rule.text),
    });
    continue;
  }
  const plays = rule.text.match(/animation\s*:\s*([\w-]+)/);
  users.push({ ...rule, classes, plays: plays ? plays[1] : null, label: classes.length ? `.${classes[0]}` : rule.selector });
}
for (const tier of ['full', 'subtle', 'off']) if (!tierAt[tier]) die(`no :root block for data-motion="${tier}" inside the gate`);

/** The declaration a read sits in: its property, and its full text. */
function declaration(text, i) {
  const from = text.lastIndexOf(';', i) + 1;
  const end = text.indexOf(';', i);
  const prop = text.slice(from, i).match(/([-\w]+)\s*:[^;]*$/);
  return { prop: prop ? prop[1] : '', text: text.slice(from, end < 0 ? text.length : end) };
}

const reads = [];
for (const u of users) {
  for (const m of u.text.matchAll(READ)) {
    const decl = declaration(u.text, m.index);
    reads.push({
      token: m[1],
      fallback: m[2] === ',',
      where: u.label,
      line: lineAt(u.bs + m.index),
      classes: u.classes,
      transition: decl.prop.startsWith('transition'),
      // An ambient loop's period, which "subtle" may leave alone.
      loop: decl.prop.startsWith('animation') && /\binfinite\b/.test(decl.text),
    });
  }
}
const frames = new Set();
for (const m of css.matchAll(/@keyframes\s+([\w-]+)\s*\{/g)) {
  const [bs, be] = block(css.indexOf('{', m.index));
  const text = css.slice(bs, be);
  // A keyframe is read BY the classes that play it, and those classes are what
  // an intensity can switch off, so the token inherits their fate and not its
  // own. Nothing plays it means nothing excuses it either.
  const classes = [...new Set(users.filter((u) => u.plays === m[1]).flatMap((u) => u.classes))];
  for (const r of text.matchAll(READ)) {
    frames.add(m[1]);
    reads.push({
      token: r[1],
      fallback: r[2] === ',',
      where: `@keyframes ${m[1]}`,
      line: lineAt(bs + r.index),
      classes,
      transition: false,
      loop: false,
    });
  }
}

const tokens = new Set(reads.map((r) => r.token));
if (tokens.size < 20 || defs.full.size < 20) {
  die(`only ${tokens.size} token(s) read and ${defs.full.size} defined, which is too few to be this file`);
}

const seen = new Set();
const problems = [];
/** Counted per token and intensity, not per read: one keyframe spends
    --motion-shake-scale four times and that is one deliberate gap, not four. */
const deliberate = new Set();
for (const r of reads) {
  if (!defs.full.has(r.token)) {
    if (r.fallback || seen.has(r.token)) continue;
    seen.add(r.token);
    problems.push(
      `${rel}:${r.line} ${r.where} reads ${r.token}, which no intensity defines: the declaration is invalid and the element does not animate at all`,
    );
    continue;
  }
  for (const tier of ['subtle', 'off']) {
    if (defs[tier].has(r.token)) continue;
    const stopped =
      r.classes.length > 0 &&
      r.classes.every((cl) =>
        substitutes[tier].some(
          (s) => (s.classes.includes(cl) || s.classes.includes('*')) && (r.transition ? s.transition : s.animation),
        ),
      );
    if (stopped || (tier === 'subtle' && r.loop)) {
      deliberate.add(`${tier} ${r.token}`);
      continue;
    }
    const key = `${tier} ${r.token}`;
    if (seen.has(key)) continue;
    seen.add(key);
    problems.push(
      `${rel}:${tierAt[tier]} [data-motion="${tier}"] never sets ${r.token}, read by ${r.where} (${rel}:${r.line}) ` +
        `and nothing switches that off here, so it keeps the full-intensity value`,
    );
  }
}

if (problems.length) {
  console.error(`check-motion-tokens: ${problems.length} motion token(s) an intensity drops without meaning to.`);
  console.error('Each line names where the definition belongs and who is left reading past it:');
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}

const utilities = new Set(reads.filter((r) => !r.where.startsWith('@')).map((r) => r.where));
console.log(
  `ok: ${tokens.size} motion tokens, read by ${frames.size} keyframes and ${utilities.size} utilities, ` +
    `set at all three intensities (${deliberate.size} gaps left on purpose, each with its own substitute rule)`,
);
