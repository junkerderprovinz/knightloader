// Nothing on this app's screens is greyed out AND switched off at the same time.
//
// THE RULE IT ENFORCES (GlimStone 1.16.0, "Switches"). Two passages in that
// document had been contradicting each other for six releases - a sub-switch
// under an off parent must be ABSENT, while an accent picker that rainbow mode
// has partly taken over stays DIMMED - and 1.16.0 settled it with one question:
//
//   DOES THE CONTROL STILL DO ANYTHING? No state behind it, remove it. State
//   that still shows somewhere, dim it and say who is in charge.
//
// A control group that is dimmed AND inert at once is the one shape both halves
// of that answer rule out:
//
//   - if the control still acts, making it inert is a lie about a value that is
//     still in force. This app's own accent row is the case that proves it: the
//     rainbow only recolours things that are one member of a SET, so the
//     download button, the speed curve and the add screen still paint the
//     picked accent while the mode is on - and the row that chooses that colour
//     was the one row nobody could press.
//   - if it does not act, dimming it is furniture: something anybody can see,
//     read and reach for that answers nothing, with the reason sitting one row
//     up or one screen away, where nobody looks once they have decided this row
//     is the interesting one.
//
// AND IT CARRIES A SECOND FAULT, which is why the pair is worth banning rather
// than arguing about case by case: opacity composites a whole subtree in React
// Native exactly as it does in CSS, so a child can never be less transparent
// than its parent. The one line that could explain why the group is dim fades
// with it, and it fades precisely in the state it exists for.
//
// THE PHONE'S OWN HALF OF THIS, and it is the reason the web's version of this
// script could not simply be copied: there are no class strings here. The two
// halves are `pointerEvents` (React Native's own prop, not a style) and a style
// entry whose StyleSheet body sets an opacity below 1. So the script reads the
// file's StyleSheet.create block first, learns which style names dim, and then
// asks of every element carrying a pointerEvents whether its own style prop
// names one of them.
//
// `pointerEvents` is also worse here than pointer-events-none is on the web: on
// Android it takes the subtree out of TalkBack's reach along with the finger's,
// so a row that is merely "not chooseable right now" becomes a row a screen
// reader cannot even read out.
//
// WHAT IT DOES NOT SEE. It judges shape, never behaviour: only the code behind
// a control answers 1.16.0's question, so a dim WITHOUT inertness is legitimate
// (a value that still acts, marked) and invisible to this script. A style built
// by concatenating variables, or an opacity driven by an Animated.Value, is
// invisible too. And a `pointerEvents` with no dim beside it is not this defect
// - an overlay that must not swallow touches is an ordinary thing to write.
//
// Run by hand and by CI, from mobile/: `node check-dimmed-and-inert.mjs`
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
    else if (/\.tsx?$/.test(name)) out.push(p);
  }
  return out;
};

/** Comments blanked, offsets kept byte for byte, so a line number is the real
 *  one AND the paragraphs that EXPLAIN why a wrapper like this was removed are
 *  not read as one still being there. */
const strip = (text) =>
  text
    .replace(/\/\*[\s\S]*?\*\//g, (c) => c.replace(/[^\n]/g, ' '))
    .replace(/^([^\n'"`]*?)\/\/.*$/gm, (_, head) => head);

/**
 * The style names in this file that dim, read out of its own StyleSheet.
 *
 * Read rather than guessed at: a project is free to call the style `dimmed`,
 * `faded` or `inaktiv`, and a script that only knew one of those names would
 * pass the next screen that picked a different word.
 */
function dimmingStyles(code) {
  const names = new Set();
  const at = code.indexOf('StyleSheet.create(');
  if (at < 0) return names;
  // Every `name: { ... }` entry at the top level of the create() object.
  for (const m of code.slice(at).matchAll(/([A-Za-z_$][\w$]*)\s*:\s*\{([^{}]*)\}/g)) {
    const opacity = m[2].match(/opacity\s*:\s*([\d.]+)/);
    if (opacity && Number(opacity[1]) < 1) names.add(m[1]);
  }
  return names;
}

/** The span of the JSX element whose attribute list contains `at`. */
function elementSpan(code, at) {
  let open = code.lastIndexOf('<', at);
  if (open < 0) return null;
  let depth = 0;
  for (let i = open; i < code.length; i++) {
    const ch = code[i];
    if (ch === '{') depth++;
    else if (ch === '}') depth--;
    else if (depth === 0 && ch === '>' && i >= at) return code.slice(open, i + 1);
  }
  return code.slice(open);
}

/** The text of one JSX attribute's value, braces balanced. */
function attr(el, name) {
  const m = el.match(new RegExp(`\\b${name}\\s*=\\s*`));
  if (!m) return null;
  let i = m.index + m[0].length;
  if (el[i] === '"' || el[i] === "'") {
    const q = el[i];
    const end = el.indexOf(q, i + 1);
    return end < 0 ? null : el.slice(i + 1, end);
  }
  if (el[i] !== '{') return null;
  let depth = 0;
  for (let j = i; j < el.length; j++) {
    if (el[j] === '{') depth++;
    else if (el[j] === '}' && --depth === 0) return el.slice(i + 1, j);
  }
  return null;
}

const problems = [];
for (const file of walk(root)) {
  const code = strip(readFileSync(file, 'utf8'));
  const dims = dimmingStyles(code);
  for (const m of code.matchAll(/\bpointerEvents\s*=/g)) {
    const el = elementSpan(code, m.index);
    if (!el) continue;
    const pe = attr(el, 'pointerEvents');
    // Only an element that can actually end up inert. A `pointerEvents` that
    // never says 'none' is not this defect.
    if (pe === null || !/['"]none['"]/.test(pe)) continue;
    const style = attr(el, 'style') ?? '';
    const named = [...dims].filter((d) => new RegExp(`\\b${d}\\b`).test(style));
    const literal = style.match(/opacity\s*:\s*([\d.]+)/);
    const dimsHere = named.length > 0 || (literal && Number(literal[1]) < 1);
    if (!dimsHere) continue;
    const line = code.slice(0, m.index).split('\n').length;
    problems.push(
      `${relative(here, file).replace(/\\/g, '/')}:${line} dimmed and inert at once ` +
        `(pointerEvents ${pe.trim().slice(0, 40)}, style ${(named[0] ?? `opacity ${literal[1]}`).slice(0, 40)}) - ` +
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

console.log(
  `ok: ${walk(root).length} files, no control group is both dimmed and inert; ` +
    'every group that is dim can still be pressed, and every group that cannot act is gone',
);
