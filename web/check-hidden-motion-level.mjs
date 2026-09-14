// The hidden fourth motion level stays hidden, stays switchable off, and stays
// inside the accessibility gate.
//
// GlimStone 1.17.0 adds `storm`: a fourth intensity nobody is offered, reached
// by setting motion to the top level and tapping that same option five more
// times. The numbers are the cheap half. The rule it exists to establish is the
// expensive one, and it is what this script guards:
//
//   AN EASTER EGG THAT CHANGES BEHAVIOUR MUST BE SWITCHABLE BACK OFF, AND MUST
//   NOT QUIETLY BECOME A PERMANENT ENTRY IN A SETTINGS LIST.
//
// WHAT BREAKS WITHOUT IT, one failure per check, and every one of them is
// invisible in a build, a type check and a screenshot alike:
//
//   gate      A "more animation" switch outside @media (prefers-reduced-motion:
//             no-preference) is the one way this level could genuinely do harm:
//             a machine whose owner asked the operating system for less motion
//             would start evaluating a data-motion selector again and get MORE
//             movement than the three visible levels can produce. The other
//             three are safe because of where the block sits, not because of a
//             check in front of them, so the fourth has to sit in the same
//             place. A storm block written one brace further out still works
//             perfectly for everybody who never set the OS preference, which is
//             everybody who tests it.
//
//   tokens    A level is a different NUMBER, never a different animation. A
//             storm block that invents a token nothing else defines is a forked
//             animation wearing an intensity's clothes; one that forgets the
//             five the language names for this level is a "storm" that arrives
//             as a slightly longer wild and gets reported as "the egg does
//             nothing".
//
//   picker    The list a picker renders must not contain storm. This is the
//             defect the whole rule came from: the first build stored a "found
//             it" flag and one gesture put a fourth option in the settings for
//             ever after, which turns a secret into a setting somebody has to
//             explain to themselves months later with no memory of how it got
//             there.
//
//   stored    ...while a STORED storm is still accepted at boot, or the gesture
//             produces a setting that silently forgets itself on the next
//             reload. Validating a stored value and populating a picker are two
//             different questions and this is the axis where treating them as
//             one shows up, so the two lists have to be two lists.
//
//   memory    The "found it" fact lives in the settings screen's own state and
//             NEVER in storage. It is the same mistake as the picker one, one
//             layer down: persist it and the option comes back on the next
//             load, which is exactly the permanent entry the rule forbids.
//             Grep-shaped on purpose - any storage write whose key or value
//             mentions the hidden level is the failure, wherever it is written.
//
// WHAT IT DOES NOT SEE. It reads three files and judges shape, never value: a
// storm block whose numbers are SMALLER than wild's passes here and is caught
// by looking at the screen. It cannot tell whether the gesture itself works -
// that is a live measurement, and the tap counter is deliberately in one
// function (stormTap in lib/appearance.ts) so it can be read at a glance.
//
// Run by CI and by hand, from web/: `node check-hidden-motion-level.mjs`
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const read = (rel) => readFileSync(join(here, rel), 'utf8');

const problems = [];
const fail = (why) => problems.push(why);

// --- the CSS half -----------------------------------------------------------

const cssRel = 'src/index.css';
// Comments blanked, offsets kept byte for byte, so a line number is the real
// one AND the paragraphs that DESCRIBE the level are not read as code.
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

const STORM = /:root\[data-motion=["']storm["']\]/g;
const storms = [...css.matchAll(STORM)];
if (!storms.length) {
  fail(`${cssRel}: no :root[data-motion="storm"] block at all - the hidden fourth level has no numbers behind it`);
}
for (const m of storms) {
  if (m.index < gs || m.index > ge) {
    fail(
      `${cssRel}:${lineAt(m.index)} a storm selector sits OUTSIDE @media (prefers-reduced-motion: no-preference) ` +
        `(the gate runs ${lineAt(gs)}-${lineAt(ge)}): a machine asking the OS for less motion would evaluate it`,
    );
  }
}

/** The declarations `--x: y` a block sets, by name. */
const declared = (from) => new Set([...css.slice(...block(from)).matchAll(/(--[\w-]+)\s*:/g)].map((d) => d[1]));

if (storms.length && storms[0].index >= gs && storms[0].index <= ge) {
  const stormOpen = css.indexOf('{', storms[0].index);
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
  for (const tok of ['--motion-page-dur', '--motion-page-travel', '--motion-page-scale', '--motion-curve', '--glim-toast-slide']) {
    if (!stormTokens.has(tok)) {
      fail(`${cssRel}:${lineAt(stormOpen)} the storm block never sets ${tok}, so it inherits the top visible level's own value`);
    }
  }
}

// --- the TypeScript half ----------------------------------------------------

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

// The boot reader has to validate against the STORED list, not the picker's.
const reader = app.match(/export function readCachedMotionIntensity\s*\([\s\S]*?\n\}/);
if (!reader) fail(`${appRel}: readCachedMotionIntensity is gone - nothing reads the stored intensity at boot any more`);
else if (!/MOTION_STORED/.test(reader[0])) {
  fail(`${appRel}: readCachedMotionIntensity does not check against MOTION_STORED, so a stored storm is thrown away at boot`);
}

if (!/export function stormTap\s*\(/.test(app)) {
  fail(`${appRel}: no stormTap() - the gesture is the whole mechanism and it belongs in one function`);
}

// --- the "found it" flag never reaches storage ------------------------------

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
  // Comments blanked, offsets kept byte for byte, so a line number is the real
  // one AND a paragraph EXPLAINING what is not persisted is not read as a
  // persistence. Written the naive way this check failed on its own doctrine:
  // the comment above stormFound in Look.tsx says the words "localStorage" and
  // "storm" in one sentence, which is the correct file describing the correct
  // behaviour, reported as the defect.
  const text = readFileSync(file, 'utf8')
    .replace(/\/\*[\s\S]*?\*\//g, (c) => c.replace(/[^\n]/g, ' '))
    .replace(/^([^\n'"`]*?)\/\/.*$/gm, (_, head) => head);
  text.split('\n').forEach((line, i) => {
    if (!/localStorage|sessionStorage|uistate|UIState|setItem/i.test(line)) return;
    if (!/storm|stormFound|motionFound/i.test(line)) return;
    problems.push(
      `${relative(here, file).replace(/\\/g, '/')}:${i + 1} a storage write mentions the hidden level: ` +
        `"${line.trim().slice(0, 90)}" - the CHOSEN value persists like any other, the "found it" fact never does`,
    );
  });
}

if (problems.length) {
  console.error(`check-hidden-motion-level: ${problems.length} problem(s) with the hidden fourth motion level.`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}

console.log(
  'ok: storm has its own token block inside the reduced-motion gate, no picker offers it, ' +
    'a stored one is accepted at boot, and nothing writes the discovery to storage',
);
