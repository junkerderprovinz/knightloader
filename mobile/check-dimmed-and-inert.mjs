// Nothing on this app's screens is greyed out and switched off at the same time.
//
// GlimStone 1.16.0 ("Switches") settles it with one question: does the control
// still do anything? No state behind it, remove it. State that still shows
// somewhere, dim it and say who is in charge. A group that is dimmed and inert
// at once is the shape both answers rule out. If the control still acts, making
// it inert lies about a value that is in force, which is what happened to the
// accent row: rainbow mode only recolours one member of a set, so the download
// button, the speed curve and the add screen still paint the picked accent
// while nobody could press the row that chooses it. If it does not act, dimming
// it leaves furniture anybody can see, read and reach for that answers nothing.
//
// The pair carries a second fault: opacity composites a whole subtree in React
// Native as it does in CSS, so a child can never be less transparent than its
// parent. The one line that could explain why the group is dim fades with it,
// in exactly the state it exists for.
//
// The web's version of this script reads class strings, of which there are none
// here. The two halves on the phone are `pointerEvents`, a prop rather than a
// style, and a style entry whose StyleSheet body sets an opacity below 1. So
// the script reads the file's StyleSheet.create block first, learns which style
// names dim, and then asks of every element carrying a pointerEvents whether
// its own style prop names one of them.
//
// `pointerEvents` is also worse here than pointer-events-none is on the web: on
// Android it takes the subtree out of TalkBack's reach along with the finger's,
// so a row that is merely not chooseable right now becomes a row a screen
// reader cannot even read out.
//
// It judges shape, never behaviour. A dim without inertness is legitimate and
// invisible here, as is a style built by concatenating variables or an opacity
// driven by an Animated.Value. A `pointerEvents` with no dim beside it is an
// ordinary overlay that must not swallow touches.
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

/** Comments blanked with their offsets kept byte for byte, so a line number is
 *  the real one and a paragraph about a wrapper is not read as one. */
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
