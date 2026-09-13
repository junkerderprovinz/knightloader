// The ground behind a floating window is a token, and no component types a number for it.
//
// GlimStone 1.11.0 made the scrim `--glim-scrim`, spent by one class,
// `.glim-modal-backdrop`. Every element that floats a window carries that class
// and paints no ground of its own. This repo reached that release with the value
// typed instead: both of the web UI's floating windows, the command palette and
// the Modal that sixteen call sites open, sat on a hand-written `bg-black/50`.
// That is a strength the language does not use anywhere. It asks for .65 on a
// dark ground and .55 on a light one, and at .50 the card in front and the page
// behind sit close enough in value that the eye keeps reading the page, which is
// the one thing a scrim exists to stop. The release itself was written after the
// same drift was found in another app, where four scrims stood at two different
// strengths with nothing in that codebase able to notice.
//
// WHY A SCRIPT AND NOT A PARAGRAPH. There was no shortage of paragraphs. The
// token table has carried the rule since 1.11.0, and index.css now carries a
// block above the class saying "no bg-black/50, no second number a component
// invented for itself". A paragraph in the stylesheet is read by somebody
// editing the stylesheet. The person who types the number is adding a dialog in
// a component file, and they get the number the way both of this repo's scrims
// got it, by copying the class list off the window next to it. A value that
// arrives by copy is invisible to review as well, because the reviewer reads the
// new file and finds it agreeing with the old one. Nothing in that path passes
// the stylesheet, so nothing in that path meets the note.
//
// WHAT IT ASKS OF A LAYER. A class list that is `fixed inset-0` and lays its
// child out (flex or grid) is floating a window, and it has to carry
// `glim-modal-backdrop`. Any `fixed inset-0` class list at all, laying out or
// not, is forbidden a black wash of its own, because a full viewport black over
// the page IS the scrim however the element is arranged, including the shape
// where the wash is one div and the card is its sibling.
//
// WHAT IT DELIBERATELY LEAVES ALONE. A `fixed inset-0` layer that holds no
// window is asked for nothing. The click-outside catcher behind an open menu is
// the usual one, a transparent full viewport div whose whole job is to receive
// one click, and it is right that it paints nothing and carries no class. So is
// a `pointer-events-none` layer, which cannot be the ground behind a window
// because the ground behind a window is the thing you click to dismiss it. Both
// are skipped by the flex-or-grid test rather than by a list of exceptions,
// which is why there is no list of exceptions to keep current.
//
// WHAT IT DOES NOT SEE. Anything painted from JavaScript, which covers
// `style={{ background: … }}` and a colour assembled at runtime. Anything
// outside web/src, so dist/ and a scrim shipped inside a dependency are both
// past it. The window itself: the card's surface, its arrival and its z-order
// are somebody else's subject, this one is only the ground. And, exactly as in
// check-hover-ramp, a class list split across two literals joined with `+` is
// read as two, so a float in one half and its ground in the other slips through.
//
// THE STYLESHEET HALF IS READ WITH COMMENTS MASKED, and that is load-bearing
// here rather than tidiness. index.css's own commentary quotes the literal text
// `.glim-modal-backdrop { background: var(--glim-scrim) }` while explaining
// where the fade belongs. A check that grepped the raw file would keep reporting
// that the rule is present after the rule itself was deleted, standing green
// over the one failure it exists to catch. The same masking is what stops the
// component half accusing a comment that quotes a bad class list as an example.
//
// Run by hand or from CI: `node web/check-scrim.mjs`.
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
 * A black wash written by hand. `bg-black/50` is the spelling this repo used,
 * and the arbitrary-value forms are here because the first answer to being
 * stopped is to write the same number a way the check does not read.
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

/** Every piece of text that is ONE class list, with where it starts. */
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

// --- The stylesheet half: the class has to spend the token, and the token has
// --- to be answered by every theme, or one of them falls back to another
// --- theme's darkness and a light page opens under a blackout.
const cssPath = join(src, 'index.css');
const css = maskComments(readFileSync(cssPath, 'utf8'));

if (!/\.glim-modal-backdrop\s*(?:,[^{]*)?\{[^}]*background:\s*var\(--glim-scrim\)/.test(css)) {
  problems.push(`index.css: .${BACKDROP} does not spend var(--glim-scrim) -> every scrim above is unpainted`);
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
    `no ground typed by hand, and --glim-scrim answered in all ${themes.length} theme blocks.`,
);
