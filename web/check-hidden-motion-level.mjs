// The hidden fourth motion level stays hidden, stays switchable off, and is
// the one level exempt from reduced motion, with its animations restored.
//
// GlimStone 2.0.0 adds `storm`, a fourth intensity nobody is offered, reached
// by setting motion to the top level and tapping that option five more times.
// The rule behind it is that an easter egg which changes behaviour can be
// switched back off and does not become a permanent entry in a settings list.
// GlimStone 2.1.0 lets it outrank an OS request for reduced motion, because
// five taps on a chosen option are a request and the three offered levels are
// not. Each failure below is invisible in a build, a type check and a
// screenshot:
//
//   gate      The storm's token block sits outside every @media block. Inside
//             the no-preference gate, a machine asking for less motion never
//             resolves its numbers, so the animations the reduce block restores
//             for it read undefined tokens and play nothing. Everybody who
//             tests without the OS setting sees a working storm either way.
//
//   exempt    Inside a (prefers-reduced-motion: reduce) block every substitute
//             that plays something is written under
//             `:root:not([data-motion="storm"])`, and the same element has a
//             `:root[data-motion="storm"]` rule that restores the full
//             animation. Exempting without restoring leaves the storm with no
//             animation at all, quieter than the substitute it lost. A restore
//             never plays an infinite animation: wanting more movement is not
//             wanting something that never stops. Anywhere else, a selector
//             naming the storm is a rule the three offered levels miss.
//
//   tokens    A level is a different number, never a different animation. A
//             storm block that invents a token nothing else defines is a forked
//             animation in an intensity's clothes; one that forgets the five
//             the language names for this level arrives as a slightly longer
//             wild and gets reported as an egg that does nothing.
//
//   picker    The list a picker renders does not contain storm. A stored "found
//             it" flag would let one gesture put a fourth option in the settings
//             for ever, turning a secret into a setting somebody has to explain
//             to themselves later with no memory of how it got there.
//
//   stored    A stored storm is still accepted at boot, or the gesture produces
//             a setting that forgets itself on the next reload. Validating a
//             stored value and populating a picker are two questions, so the two
//             lists are two lists.
//
//   memory    The "found it" fact lives in the settings screen's own state and
//             not in storage, for the reason the picker line gives. The check is
//             grep-shaped: a storage write whose key or value mentions the
//             hidden level is the failure, wherever it is written.
//
// Not seen: values. A storm block whose numbers are smaller than wild's passes
// here and is caught by looking at the screen. Nor whether the gesture works,
// which is a live measurement; the tap counter sits in one function (stormTap
// in lib/appearance.ts) so it can be read at a glance. A substitute that stops
// an element outright (`animation: none`) is taken for the stop of an infinite
// animation, so a one-shot animation hidden that way without the exemption
// passes too.
//
// Run from web/: `node check-hidden-motion-level.mjs`
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const read = (rel) => readFileSync(join(here, rel), 'utf8');

const problems = [];
const fail = (why) => problems.push(why);

// The CSS half.

const cssRel = 'src/index.css';
// Comments blanked, offsets kept byte for byte, so line numbers stay right and
// a paragraph describing the level is not read as code.
const css = read(cssRel).replace(/\/\*[\s\S]*?\*\//g, (c) => c.replace(/[^\n]/g, ' '));
const lineAt = (i) => css.slice(0, i).split('\n').length;

/** The [start, end) of the block whose `{` is at `at`, braces counted. */
function block(at) {
  let depth = 0;
  for (let i = at; i < css.length; i++) {
    if (css[i] === '{') depth++;
    else if (css[i] === '}' && --depth === 0) return [at + 1, i];
  }
  throw new Error(`unclosed block at ${cssRel}:${lineAt(at)}`);
}

const gateAt = css.indexOf('@media (prefers-reduced-motion: no-preference)');
if (gateAt < 0) fail(`${cssRel}: the no-preference gate is gone, and every motion token with it`);
const [gs, ge] = gateAt < 0 ? [0, 0] : block(css.indexOf('{', gateAt));

/** How many blocks are open at an offset. */
const depthAt = (i) => {
  let depth = 0;
  for (let j = 0; j < i; j++) {
    if (css[j] === '{') depth++;
    else if (css[j] === '}') depth--;
  }
  return depth;
};

/** Top-level `selector { ... }` rules between two offsets, quotes normalised. */
function rulesIn(from, to) {
  const out = [];
  let i = from;
  while (i < to) {
    const open = css.indexOf('{', i);
    if (open < 0 || open >= to) break;
    const [bs, be] = block(open);
    out.push({ selector: css.slice(i, open).trim().replace(/\s+/g, ' ').replace(/'/g, '"'), at: open, body: css.slice(bs, be) });
    i = be + 1;
  }
  return out;
}

const STORM = /\[data-motion=["']storm["']\]/g;
const reduceBlocks = [...css.matchAll(/@media \(prefers-reduced-motion: reduce\)/g)].map((m) => block(css.indexOf('{', m.index)));
const inReduce = (i) => reduceBlocks.some(([s, e]) => i >= s && i < e);

// The token block is recognised by shape: the bare selector, holding custom
// properties and nothing else.
let stormOpen = -1;
for (const m of css.matchAll(STORM)) {
  const open = css.indexOf('{', m.index);
  const selector = css.slice(Math.max(css.lastIndexOf('}', m.index), css.lastIndexOf('{', m.index)) + 1, open).trim();
  const onlyTokens = css.slice(...block(open)).replace(/--[\w-]+\s*:[^;]*;/g, '').trim() === '';
  if (/^:root\[data-motion=["']storm["']\]$/.test(selector) && onlyTokens) {
    if (depthAt(m.index) !== 0) {
      fail(
        `${cssRel}:${lineAt(m.index)} the storm's token block sits inside an @media block. Under reduced motion ` +
          `its numbers never resolve, and every animation the reduce block restores for it plays nothing`,
      );
    }
    stormOpen = open;
  } else if (!inReduce(m.index)) {
    fail(
      `${cssRel}:${lineAt(m.index)} "${selector}" names the storm outside a reduced-motion block. Its numbers ` +
        `belong in the token block and its exemption in the reduce block, nowhere else`,
    );
  }
}
if (stormOpen < 0) {
  fail(`${cssRel}: no :root[data-motion="storm"] token block at all - the hidden fourth level has no numbers behind it`);
}

// The exemption: every substitute that plays something excludes the storm, and
// every exclusion has its restore.
const EXEMPT = ':root:not([data-motion="storm"]) ';
const RESTORE = ':root[data-motion="storm"] ';
for (const [bs, be] of reduceBlocks) {
  const rules = rulesIn(bs, be);
  const restored = new Map(rules.filter((r) => r.selector.startsWith(RESTORE)).map((r) => [r.selector.slice(RESTORE.length), r]));
  for (const r of rules) {
    if (r.selector.startsWith(EXEMPT)) {
      if (!restored.has(r.selector.slice(EXEMPT.length))) {
        fail(
          `${cssRel}:${lineAt(r.at)} "${r.selector}" exempts the storm and nothing restores its animation, so under ` +
            `reduced motion the storm gets less than the substitute it was spared`,
        );
      }
    } else if (r.selector.startsWith(RESTORE)) {
      if (/\binfinite\b/.test(r.body)) {
        fail(`${cssRel}:${lineAt(r.at)} "${r.selector}" restores an infinite animation, which keeps its stop at every level`);
      }
    } else if (/animation\s*:(?!\s*none\b)/.test(r.body)) {
      fail(
        `${cssRel}:${lineAt(r.at)} "${r.selector}" is a reduced-motion substitute the storm is not exempt from: ` +
          `write it under ${EXEMPT.trim()} and restore the full animation under ${RESTORE.trim()}`,
      );
    }
  }
}

/** The declarations `--x: y` a block sets, by name. */
const declared = (from) => new Set([...css.slice(...block(from)).matchAll(/(--[\w-]+)\s*:/g)].map((d) => d[1]));

if (stormOpen >= 0) {
  const stormTokens = declared(stormOpen);
  // The full-intensity defaults: the first bare `:root {` inside the gate.
  const rootAt = css.slice(gs, ge).search(/:root\s*\{/);
  const fullTokens = rootAt < 0 ? new Set() : declared(css.indexOf('{', gs + rootAt));
  for (const tok of stormTokens) {
    if (!fullTokens.has(tok)) {
      fail(
        `${cssRel}:${lineAt(stormOpen)} storm sets ${tok}, which the full-intensity :root inside the gate never sets: ` +
          `an intensity is a different number, never a token of its own`,
      );
    }
  }
  // The five the language names for this level, so a storm cannot arrive as a
  // slightly longer wild.
  for (const tok of ['--motion-page-dur', '--motion-page-travel', '--motion-page-ease', '--motion-toast-ease', '--glim-toast-slide']) {
    if (!stormTokens.has(tok)) {
      fail(`${cssRel}:${lineAt(stormOpen)} the storm block never sets ${tok}, so it inherits the top visible level's own value`);
    }
  }
}

// The TypeScript half.

const appRel = 'src/lib/appearance.ts';
const app = read(appRel).replace(/\/\*[\s\S]*?\*\//g, '').replace(/^\s*\/\/.*$/gm, '');

const listOf = (name) => {
  const m = app.match(new RegExp(`${name}\\s*:[^=]*=\\s*\\[([^\\]]*)\\]`));
  return m ? [...m[1].matchAll(/'([^']+)'/g)].map((x) => x[1]) : null;
};

const picker = listOf('MOTION_LEVELS');
if (!picker) fail(`${appRel}: no MOTION_LEVELS array - the list a picker renders has to be a list of its own`);
else if (picker.includes('storm')) {
  fail(`${appRel}: MOTION_LEVELS contains 'storm', so the picker offers the hidden level as an ordinary fourth entry`);
}

const stored = listOf('MOTION_STORED');
if (!stored) fail(`${appRel}: no MOTION_STORED array - validating a stored value and populating a picker are two questions`);
else if (!stored.includes('storm')) {
  fail(`${appRel}: MOTION_STORED does not accept 'storm', so a found level forgets itself on the next reload`);
}

// The boot reader validates against the stored list, not the picker's.
const reader = app.match(/export function readCachedMotionIntensity\s*\([\s\S]*?\n\}/);
if (!reader) fail(`${appRel}: readCachedMotionIntensity is gone - nothing reads the stored intensity at boot any more`);
else if (!/MOTION_STORED/.test(reader[0])) {
  fail(`${appRel}: readCachedMotionIntensity does not check against MOTION_STORED, so a stored storm is thrown away at boot`);
}

if (!/export function stormTap\s*\(/.test(app)) {
  fail(`${appRel}: no stormTap() - the gesture is the whole mechanism and it belongs in one function`);
}

// The "found it" flag never reaches storage.

const walk = (dir) => {
  const out = [];
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) out.push(...walk(p));
    else if (/\.(ts|tsx)$/.test(name)) out.push(p);
  }
  return out;
};

for (const file of walk(join(here, 'src'))) {
  // Comments blanked, offsets kept byte for byte, so line numbers stay right
  // and a paragraph saying what is not persisted is not read as a write.
  const text = readFileSync(file, 'utf8')
    .replace(/\/\*[\s\S]*?\*\//g, (c) => c.replace(/[^\n]/g, ' '))
    .replace(/^([^\n'"`]*?)\/\/.*$/gm, (_, head) => head);
  text.split('\n').forEach((line, i) => {
    if (!/localStorage|sessionStorage|uistate|UIState|setItem/i.test(line)) return;
    if (!/storm|stormFound|motionFound/i.test(line)) return;
    problems.push(
      `${relative(here, file).replace(/\\/g, '/')}:${i + 1} a storage write mentions the hidden level: ` +
        `"${line.trim().slice(0, 90)}" - the chosen value persists like any other, the "found it" fact never does`,
    );
  });
}

if (problems.length) {
  console.error(`check-hidden-motion-level: ${problems.length} problem(s) with the hidden fourth motion level.`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}

console.log(
  'ok: storm has its own token block outside the gate, every reduced-motion substitute exempts it and restores ' +
    'its animation, no picker offers it, a stored one is accepted at boot, and nothing writes the discovery to storage',
);
