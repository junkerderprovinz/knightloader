// The ground behind a floating window is a token, and no component types a number for it.
//
// GlimStone 1.11.0 made the scrim `--glim-scrim`, spent by one class,
// `.glim-modal-backdrop`. Every element that floats a window carries that class
// and paints no ground of its own. Both of this UI's floating windows, the
// command palette and the Modal that sixteen call sites open, sat on a
// hand-written `bg-black/50` instead, a strength the language uses nowhere: it
// asks for .72 on a dark ground and .62 on a light one over a blurred page, and
// at .50 the card in front and the page behind sit close enough in value that
// the eye keeps reading the page, which is what a scrim exists to stop.
//
// The blur is the class's too (GlimStone 2.11.0): `--glim-scrim-blur` through
// `backdrop-filter`, with the -webkit- spelling Safari still reads, and zero
// for a system that asks for reduced transparency. A window that paints the
// darkening without the blur is half of what the language asks for.
//
// A paragraph in the stylesheet is read by somebody editing the stylesheet,
// while the person who types the number is adding a dialog in a component file
// and gets it the way both of these scrims got it, by copying the class list
// off the window next to it. A value that arrives by copy is invisible to
// review too, since the reviewer reads the new file and finds it agreeing with
// the old one.
//
// What it asks of a layer: a class list that is `fixed inset-0` and lays its
// child out (flex or grid) is floating a window and has to carry
// `glim-modal-backdrop`. Any `fixed inset-0` class list, laying out or not, is
// refused a black wash of its own, because a full viewport black over the page
// is the scrim however the element is arranged, the shape where the wash is one
// div and the card its sibling included.
//
// Left alone: a `fixed inset-0` layer that holds no window. The click-outside
// catcher behind an open menu is the usual one, a transparent full viewport div
// whose job is to receive one click, and it rightly paints nothing. So is a
// `pointer-events-none` layer, which cannot be the ground behind a window,
// since that ground is the thing you click to dismiss it. Both fall out of the
// flex-or-grid test rather than out of a list of exceptions.
//
// Not seen: anything painted from JavaScript, which covers
// `style={{ background: … }}` and a colour assembled at runtime; anything
// outside web/src, so dist/ and a scrim inside a dependency are past it; the
// window itself, whose surface, arrival and z-order are another subject; and,
// as in check-hover-ramp, a class list split across two literals joined with
// `+`, so a float in one half and its ground in the other slips through.
//
// The stylesheet half is read with comments masked. index.css's own commentary
// quotes the literal text `.glim-modal-backdrop { background: var(--glim-scrim) }`
// while explaining where the fade belongs, and a check that grepped the raw file
// would report the rule as present after the rule itself was deleted. The same
// masking stops the component half accusing a comment that quotes a bad class
// list as an example.
//
// Run: `node web/check-scrim.mjs`.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const src = join(here, 'src');

// Class tokens, matched at their own boundaries. A variant prefix still counts,
// so `sm:fixed` is `fixed`, while `inset-0.5` and `-inset-0` are not `inset-0`.
const FIXED = /(?<![\w-])fixed(?![\w.-])/;
const INSET0 = /(?<![\w-])inset-0(?![\w.-])/;
/** The layer lays its child out, which is how a ground differs from a catcher. */
const LAYS_OUT = /(?<![\w-])(?:flex|grid)(?![\w.-])/;
const UNCLICKABLE = /(?<![\w-])pointer-events-none(?![\w.-])/;
const BACKDROP = 'glim-modal-backdrop';
/**
 * A black wash written by hand, `bg-black/50` and the arbitrary-value spellings
 * of the same colour.
 */
const TYPED_GROUND =
  /(?<![\w-])bg-black(?:\/[\w.[\]]+)?|(?<![\w-])bg-\[[^\]]*(?:rgba?\(\s*0\s*,\s*0\s*,\s*0|#000|black)[^\]]*\]/;

/**
 * Comments blanked, everything else left where it was.
 *
 * Strings come first in the alternation, so a `//` inside a quoted value is a
 * value and not the start of a comment. Comment text is replaced space for
 * space and newline for newline, which keeps every later offset, and therefore
 * every reported line number, the one the file actually has.
 */
const MASKABLE = /'(?:[^'\\\n]|\\.)*'|"(?:[^"\\\n]|\\.)*"|`(?:[^`\\]|\\.)*`|\/\*[\s\S]*?\*\/|\/\/[^\n]*/g;
const maskComments = (text) =>
  text.replace(MASKABLE, (m) => (m[0] === '/' ? m.replace(/[^\n]/g, ' ') : m));

const lineOf = (text, at) => text.slice(0, at).split('\n').length;

function sources(dir) {
  const found = [];
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) found.push(...sources(path));
    else if (/\.tsx?$/.test(entry)) found.push(path);
  }
  return found;
}

/** Plain quoted strings, and the static halves of template strings. */
const QUOTED = /'(?:[^'\\\n]|\\.)*'|"(?:[^"\\\n]|\\.)*"/g;
const TEMPLATE = /`(?:[^`\\]|\\.)*`/g;

/** Every piece of text that is one class list, with where it starts. */
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

const files = sources(src);
if (files.length < 20) {
  console.error(`check-scrim: only ${files.length} source files found - wrong directory?`);
  process.exit(1);
}

const problems = [];
let overlays = 0;
let floats = 0;

for (const path of files) {
  const text = maskComments(readFileSync(path, 'utf8'));
  const where = path.slice(src.length + 1).replace(/\\/g, '/');
  for (const [piece, at] of classLists(text)) {
    if (!FIXED.test(piece) || !INSET0.test(piece)) continue;
    overlays++;
    const line = lineOf(text, at);
    const typed = piece.match(TYPED_GROUND);
    if (typed) {
      problems.push(
        `${where}:${line} paints its own ground with ${typed[0]} -> delete it and carry ${BACKDROP}`,
      );
    }
    if (!LAYS_OUT.test(piece) || UNCLICKABLE.test(piece)) continue;
    floats++;
    if (!piece.includes(BACKDROP)) {
      problems.push(`${where}:${line} floats a window on no ground -> add ${BACKDROP}`);
    }
  }
}

// A guard watching nothing reports the same green as a guard watching something
// correct. Both floating windows in this app are full viewport layers, so a run
// that finds none has lost its subject rather than passed.
if (overlays === 0) {
  console.error('check-scrim: no `fixed inset-0` layer anywhere in src - this check has lost its subject.');
  console.error('If the app genuinely stopped floating windows, .glim-modal-backdrop and this file go together.');
  process.exit(1);
}

// The stylesheet half: the class spends the token, and every theme answers it,
// or one of them borrows another theme's darkness and a light page opens under
// a blackout.
const cssPath = join(src, 'index.css');
const css = maskComments(readFileSync(cssPath, 'utf8'));

if (!/\.glim-modal-backdrop\s*(?:,[^{]*)?\{[^}]*background:\s*var\(--glim-scrim\)/.test(css)) {
  problems.push(`index.css: .${BACKDROP} does not spend var(--glim-scrim) -> every scrim above is unpainted`);
}
for (const prop of ['backdrop-filter', '-webkit-backdrop-filter']) {
  const spends = new RegExp(`\\.glim-modal-backdrop\\s*(?:,[^{]*)?\\{[^}]*(?<![\\w-])${prop}:\\s*blur\\(var\\(--glim-scrim-blur\\)\\)`);
  if (!spends.test(css)) {
    problems.push(`index.css: .${BACKDROP} sets no ${prop}: blur(var(--glim-scrim-blur)) -> the page behind a window stays sharp`);
  }
}
if (!/--glim-scrim-blur\s*:\s*[1-9]/.test(css)) {
  problems.push('index.css: --glim-scrim-blur is never given a length -> the blur is zero everywhere');
}
if (!/@media\s*\(\s*prefers-reduced-transparency:\s*reduce\s*\)\s*\{[^{}]*\{[^}]*--glim-scrim-blur\s*:\s*0/.test(css)) {
  problems.push('index.css: no prefers-reduced-transparency rule sets --glim-scrim-blur to 0 -> the blur ignores the system setting');
}

// Most windows are rendered inside the page they belong to, so the page's
// wrapper must be neither their containing block nor their backdrop root. Its
// enter animation ends on the wrapper's resting state, but a fill that holds
// the last frame keeps it as an effect: the movement as an identity matrix,
// which turns the wrapper into the box a `fixed inset-0` scrim fills, and the
// fade as an opacity layer, past which a backdrop filter cannot see. Either
// way the sidebar and the shell bar stay sharp beside a blurred page.
for (const found of css.matchAll(/\.glim-page-enter\s*\{([^}]*)\}/g)) {
  const animation = /animation\s*:([^;}]*)/.exec(found[1]);
  for (const part of animation ? animation[1].split(',') : []) {
    if (/\b(?:both|forwards)\b/.test(part)) {
      problems.push(
        `index.css:${lineOf(css, found.index)} .glim-page-enter holds the last frame of "${part.trim()}" -> fill backwards, or every window inside a page stops at the page's edge`,
      );
    }
  }
}

/** The `{ … }` enclosing `at`, with the selector standing in front of it. */
function block(text, at) {
  let depth = 0;
  let open = -1;
  for (let i = at; i >= 0; i--) {
    if (text[i] === '}') depth++;
    else if (text[i] === '{') {
      if (depth === 0) {
        open = i;
        break;
      }
      depth--;
    }
  }
  if (open < 0) return null;
  let close = text.length;
  let inner = 0;
  for (let i = open + 1; i < text.length; i++) {
    if (text[i] === '{') inner++;
    else if (text[i] === '}') {
      if (inner === 0) {
        close = i;
        break;
      }
      inner--;
    }
  }
  const head = text.slice(0, open);
  const from = Math.max(head.lastIndexOf('{'), head.lastIndexOf('}'), head.lastIndexOf(';'));
  return {
    selector: head.slice(from + 1).trim().replace(/\s+/g, ' '),
    body: text.slice(open + 1, close),
    line: lineOf(text, open),
  };
}

// `--elevation` is the yardstick rather than a hard-coded list of three
// selectors: it is the other per-theme surface value, so a fourth theme block
// added tomorrow is asked about the scrim without this file being touched.
const themes = [];
for (const found of css.matchAll(/--elevation\s*:/g)) {
  const b = block(css, found.index);
  if (!b) continue;
  themes.push(b);
  if (!/--glim-scrim\s*:/.test(b.body)) {
    problems.push(`index.css:${b.line} ${b.selector} sets --elevation but not --glim-scrim -> this theme borrows another one's scrim`);
  }
}
if (themes.length < 2) {
  console.error(`check-scrim: found ${themes.length} theme block(s) in index.css - the file changed shape and this check needs rereading.`);
  process.exit(1);
}

problems.sort();

if (problems.length) {
  console.error(`check-scrim: ${problems.length} problem(s). The ground behind a floating window is var(--glim-scrim), spent by .${BACKDROP}.`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}

console.log(
  `check-scrim: ${files.length} files, ${floats} floating window(s) of ${overlays} full-viewport layer(s) on .${BACKDROP}, ` +
    `no ground typed by hand, --glim-scrim answered in all ${themes.length} theme blocks, and the page blurred by --glim-scrim-blur.`,
);
