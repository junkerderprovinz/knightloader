// Nothing on screen is greyed out AND switched off at the same time.
//
// THE RULE IT ENFORCES (GlimStone 1.16.0, "Switches"). Two passages in that
// document had been contradicting each other for six releases - a sub-switch
// under an off parent must be ABSENT, while an accent picker that rainbow mode
// has partly taken over stays DIMMED - and 1.16.0 settled it with one question:
//
//   DOES THE CONTROL STILL DO ANYTHING? No state behind it, remove it. State
//   that still shows somewhere, dim it and say who is in charge.
//
// A wrapper that is dimmed AND inert at once - `pointer-events-none` beside an
// `opacity-*` - is the one shape both halves of that answer rule out, which is
// why it is the shape a script can look for:
//
//   - if the control still acts, making it inert is a lie about a value that is
//     still in force. A stored cookie jar is the case that proves it: the jars
//     stay on disk while the switch is off, so the delete beside each one still
//     removes a real session - and it was unreachable behind exactly this
//     wrapper, which means the one control somebody needs when they change
//     their mind about a stored sign-in was the one control they could not
//     press.
//   - if it does not act, dimming it is furniture: something anybody can see,
//     read and reach for that answers nothing, with the reason sitting one row
//     up or one page away, where nobody looks once they have decided this row
//     is the interesting one.
//
// So the two never belong together. A control that is genuinely refusing says
// so through its OWN `disabled` (and the `disabled:opacity-*` its component
// already carries), which is a state a screen reader can read, keyboard focus
// still reaches, and a tooltip can still explain. A wrapper cannot say any of
// that: `pointer-events-none` is invisible to the keyboard, so the row it
// covers is unreachable by mouse and perfectly reachable by Tab.
//
// AND IT CARRIES A SECOND FAULT, which is why the pair is worth banning rather
// than arguing about case by case (GlimStone 1.9.0): opacity composites a whole
// subtree and a child can never be less transparent than its parent, so the one
// (i) that could explain why the row is dim renders at 40% along with it. The
// element that has to stay readable while everything around it recedes becomes
// the one nobody can read, and it bites precisely in the state it exists for.
//
// WHAT IT DOES NOT SEE. It reads class strings, so a wrapper built by
// concatenating two variables is invisible to it, and so is `style={{ opacity
// }}`. It says nothing about a dim WITHOUT inertness - that shape is legitimate
// (a value that still acts, marked) and undecidable from here, since only the
// behaviour behind the control answers 1.16's question. And it deliberately
// ignores `disabled:`-prefixed utilities: `disabled:opacity-35
// disabled:pointer-events-none` on a Button is the control's own refusal, which
// is the correct answer rather than the banned one.
//
// Run by CI and by hand, from web/: `node check-dimmed-and-inert.mjs`
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
  // Comments blanked, offsets kept, so the paragraphs that EXPLAIN why a
  // wrapper like this was removed are not read as one still being there.
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
