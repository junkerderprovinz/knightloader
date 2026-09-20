// Checks that nothing on screen is greyed out and switched off at once.
//
// GlimStone 1.16.0 ("Switches") decides between the two by asking whether the
// control still does anything: no state behind it, remove it; state that still
// shows somewhere, dim it and say who is in charge. A wrapper carrying
// `pointer-events-none` beside an `opacity-*` is the shape both answers rule
// out. If the control still acts, making it inert hides a value that is still
// in force, the way the delete beside a stored cookie jar was unreachable while
// the jars stayed on disk. If it does not act, dimming it leaves something to
// read and reach for that answers nothing.
//
// A control that refuses says so through its own `disabled`, which a screen
// reader reads, keyboard focus still reaches and a tooltip can explain;
// `pointer-events-none` is invisible to the keyboard, so the row is
// unreachable by mouse and reachable by Tab. Opacity also composites the whole
// subtree (GlimStone 1.9.0), so the (i) that would explain the dim renders at
// 40% along with it.
//
// Not checked: a wrapper built from variables or `style={{ opacity }}`; a dim
// without inertness, which is legitimate and undecidable from here.
// `disabled:`-prefixed utilities are ignored, since those are the control's own
// refusal.
//
// Run from web/: `node check-dimmed-and-inert.mjs`
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const root = join(here, 'src');

const walk = (dir) => {
  const out = [];
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) out.push(...walk(p));
    else if (/\.(ts|tsx)$/.test(name)) out.push(p);
  }
  return out;
};

/** Class lists, as they are written: a quoted run of utility words. */
const CLASSES = /(["'`])((?:[^"'`\\\n]|\\.)*?)\1/g;
const DIM = /(^|\s)opacity-\d+(\s|$)/;
const INERT = /(^|\s)pointer-events-none(\s|$)/;

const problems = [];
for (const file of walk(root)) {
  const text = readFileSync(file, 'utf8');
  // Comments blanked, offsets kept, so a paragraph about such a wrapper is not
  // read as one.
  const code = text
    .replace(/\/\*[\s\S]*?\*\//g, (c) => c.replace(/[^\n]/g, ' '))
    .replace(/^([^\n'"`]*?)\/\/.*$/gm, (_, head) => head);
  for (const m of code.matchAll(CLASSES)) {
    // A class list, not prose: every word is a utility.
    const words = m[2].trim();
    if (!words || /[.,;:!?]/.test(words)) continue;
    // The control's own refusal is not this defect.
    const plain = words.replace(/(^|\s)disabled:[\w:[\]/.-]+/g, ' ');
    if (!DIM.test(plain) || !INERT.test(plain)) continue;
    const line = code.slice(0, m.index).split('\n').length;
    problems.push(
      `${relative(here, file).replace(/\\/g, '/')}:${line} dimmed and inert at once: "${words.slice(0, 80)}" - ` +
        `either the control still acts, in which case it may not be switched off, or it does not, in which case it goes`,
    );
  }
}

if (problems.length) {
  console.error(`check-dimmed-and-inert: ${problems.length} control group(s) both greyed out and switched off.`);
  console.error('GlimStone 1.16.0: the test is whether the control still does anything.');
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}

const files = walk(root).length;
console.log(`ok: ${files} files, no control group is both dimmed and inert; every refusal is a control's own disabled state`);
