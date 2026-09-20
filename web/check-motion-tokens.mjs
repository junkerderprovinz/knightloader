// Every motion token a keyframe or a utility class reads is set at all three
// motion intensities, or is left unset and has a substitute rule.
//
// The motion engine in src/index.css is one token set read by one set of
// keyframes, so less motion is a smaller number and never a second animation.
// A dropped token fails in two silent ways. A token no block defines makes its
// whole declaration invalid at computed-value time, so
// `animation: glim-toast-in var(--motion-toast-dur) ...` is no animation at all
// and the element simply appears. A token only the "off" block forgets still
// resolves, falling through to the :root defaults, which are the top level, so
// a reader who set motion to off keeps the 18px page slide and the 24px toast
// throw.
//
// The three blocks are flat lists fifty lines apart and are not meant to be
// identical: "off" leaves the shake and pulse tokens alone because it replaces
// both animations outright, and "subtle" leaves the pulse period alone because
// an infinite ambient loop cannot be calmed by running it faster. Gaps are
// therefore the normal state of the file, and an eye reading down the lists
// cannot tell an intended gap from an accidental one.
//
// A gap is forgiven only when every class that reads the token is switched off
// at that intensity by a `:root[data-motion="..."]` rule of its own, which is
// the mechanism the file uses: .glim-live and .glim-shake read no token under
// "off" because they are handed a different animation there. Delete the
// substitute and the exemption stops holding. The one exemption not derived
// from a substitute rule is the period of an `infinite` animation under
// "subtle"; it covers the duration named in the animation shorthand and nothing
// the keyframes read, so the pulse's amplitude still has to come down.
//
// Not seen: a token spent in an inline style in a .tsx file, since only
// src/index.css is read; values, so a "subtle" number larger than the "wild"
// one passes; the OS-level (prefers-reduced-motion: reduce) block, which
// hard-codes every value; the universal `transition-duration: 0s !important`
// belt, modelled only as a substitute for transitions; --dir-sign, a layout
// fact kept outside the motion gate; and the opposite defect, a token nobody
// reads.
//
// Run from web/: `node check-motion-tokens.mjs`
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const rel = 'src/index.css';
const file = join(dirname(fileURLToPath(import.meta.url)), rel);
// Comments blanked, offsets kept byte for byte, so line numbers stay right and
// a token merely named in a paragraph does not count as a read. The engine's
// own prose names half of them while explaining them.
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

const TIER_SELECTOR = { ':root': 'wild', ':root[data-motion="subtle"]': 'subtle', ':root[data-motion="off"]': 'off' };
const defs = { wild: new Set(), subtle: new Set(), off: new Set() };
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
// The top level's block is the bare :root, never `:root[data-motion="wild"]`:
// a rule names the quiet levels and not the lively one, or it stops covering
// storm (GlimStone 2.0.0, held by check-motion-top-level.mjs).
for (const tier of ['wild', 'subtle', 'off']) {
  if (tierAt[tier]) continue;
  const sel = Object.keys(TIER_SELECTOR).find((k) => TIER_SELECTOR[k] === tier);
  die(`no \`${sel}\` block inside the gate, so the ${tier} level has no numbers`);
}

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
  // A keyframe is read by the classes that play it, and those are what an
  // intensity can switch off, so its tokens inherit their fate. A keyframe
  // nothing plays has nothing to excuse it either.
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
if (tokens.size < 20 || defs.wild.size < 20) {
  die(`only ${tokens.size} token(s) read and ${defs.wild.size} defined, which is too few to be this file`);
}

const seen = new Set();
const problems = [];
/** Counted per token and intensity, not per read: one keyframe spends
    --motion-shake-scale four times and that is one gap, not four. */
const deliberate = new Set();
for (const r of reads) {
  if (!defs.wild.has(r.token)) {
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
        `and nothing switches that off here, so it keeps the top level's own value`,
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
