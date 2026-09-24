// The app's own mark is never drawn larger than the rail draws it.
//
// KnightLoader's shield appears in three places, and one of them is the brand:
// the mark at the top of the sidebar, beside the app's name. The other two, on
// a card and in a row, repeat an identity already established one column to
// the left. A repeat that out-sizes the original is a second, louder brand
// mark on the same screen, and the eye goes to it instead of to the reading
// the card exists to show.
//
// It goes wrong in one direction only, and by request: a card's mark grows a
// step at a time, each step small and each granted, and nobody reopens
// Sidebar.tsx to see what the number is measured against. It ended at 144px
// against the rail's 112px, the largest drawing of the app's identity anywhere
// in the app sitting on a card in a grid.
//
// A comment cannot check itself against the code beneath it: the sentence "it
// grows to 7rem where the card allows it" went on standing one line above a
// class that had meanwhile become `h-36`.
//
// Every element that draws the mark, an `<img>` whose `src` is the binding a
// file imported from `assets/logo.svg` or the inline `.kl-egg` span the same
// file is parsed into, declares its height in Tailwind's `h-<n>` units. The
// largest one in the sidebar is the ceiling and nothing elsewhere exceeds it.
// Equal is fine: a card's mark matching the rail's is coherent.
//
// The ceiling is derived rather than typed in, so changing the rail moves it.
//
// Not seen: a mark sized by its container (`h-full`, `h-auto`, `h-screen`),
// where the slot decides and its size cannot be read from the class list, so
// those are counted and reported rather than judged; width, since every one of
// these is `w-auto` or square; `max-h-*`, a clamp rather than a size; whether
// the rail's own size is right, it being the reference; and a mark drawn by a
// component in another file or from a class list assembled by a helper, which
// none of the three call sites does.
//
// Run: `node web/check-mark-scale.mjs`.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const src = join(dirname(fileURLToPath(import.meta.url)), 'src');
const show = (path) => path.slice(src.length + 1).split('\\').join('/');
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

/** Index just past the string starting at `i`. */
function endOfString(text, i) {
  const quote = text[i];
  i += 1;
  while (i < text.length) {
    const c = text[i];
    if (c === '\\') { i += 2; continue; }
    if (c === quote) return i + 1;
    if (quote === '`' && c === '$' && text[i + 1] === '{') { i = endOfBraces(text, i + 1); continue; }
    if (quote !== '`' && c === '\n') return i;
    i += 1;
  }
  return i;
}

/** Index just past the `}` matching the `{` at `i`. */
function endOfBraces(text, i) {
  let depth = 0;
  while (i < text.length) {
    const c = text[i];
    if (c === '"' || c === "'" || c === '`') { i = endOfString(text, i); continue; }
    if (c === '{') depth += 1;
    else if (c === '}') { depth -= 1; if (depth === 0) return i + 1; }
    i += 1;
  }
  return i;
}

/** The attribute text of the tag opening at `i`, or null if it never closes. */
function attrsOf(text, i) {
  let at = i + 1;
  while (at < text.length && /[A-Za-z0-9_$.]/.test(text[at])) at += 1;
  const from = at;
  while (at < text.length) {
    const c = text[at];
    if (c === '"' || c === "'" || c === '`') { at = endOfString(text, at); continue; }
    if (c === '{') { at = endOfBraces(text, at); continue; }
    if (c === '>') return text.slice(from, at);
    at += 1;
  }
  return null;
}

/** Comments blanked one character for one, so every offset stays put. */
function blankComments(text) {
  const out = text.split('');
  let i = 0;
  while (i < text.length) {
    const c = text[i];
    if (c === '"' || c === "'" || c === '`') { i = endOfString(text, i); continue; }
    if (c === '/' && text[i + 1] === '/') {
      while (i < text.length && text[i] !== '\n') { out[i] = ' '; i += 1; }
      continue;
    }
    if (c === '/' && text[i + 1] === '*') {
      const close = text.indexOf('*/', i + 2);
      const stop = close === -1 ? text.length : close + 2;
      for (let k = i; k < stop; k += 1) if (out[k] !== '\n') out[k] = ' ';
      i = stop;
      continue;
    }
    i += 1;
  }
  return out.join('');
}

/** Every literal class named anywhere inside one attribute's value. */
function classesIn(attrs) {
  const found = /\bclassName\s*=\s*/.exec(attrs);
  if (!found) return [];
  const at = found.index + found[0].length;
  const opener = attrs[at];
  let expr;
  if (opener === '{') expr = attrs.slice(at + 1, endOfBraces(attrs, at) - 1);
  else if (opener === '"' || opener === "'") expr = attrs.slice(at, endOfString(attrs, at));
  else return [];
  const pieces = [];
  // Every quoted piece, both branches of a ternary included - `h-10` and `h-28`
  // are two literals in one expression and both of them really render.
  for (const m of expr.matchAll(/'([^'\\\n]*)'|"([^"\\\n]*)"/g)) pieces.push(m[1] ?? m[2]);
  for (const m of expr.matchAll(/`((?:[^`\\]|\\.)*)`/g)) pieces.push(m[1].replace(/\$\{[\s\S]*?\}/g, ' '));
  return pieces.join(' ').split(/\s+/).filter(Boolean);
}

/** The heights a class list asks for, in px, and the ones it defers. */
const NUMERIC = /^(?:[a-z][a-z0-9-]*:)*h-(\d+(?:\.\d+)?)$/;
const ARBITRARY = /^(?:[a-z][a-z0-9-]*:)*h-\[(\d+(?:\.\d+)?)(px|rem)\]$/;
const DEFERRED = /^(?:[a-z][a-z0-9-]*:)*h-(full|auto|screen|min|max|fit|dvh|svh|lvh)$/;

function heightsOf(classes) {
  const px = [];
  let deferred = false;
  for (const c of classes) {
    const n = NUMERIC.exec(c);
    if (n) { px.push(Number(n[1]) * 4); continue; } // Tailwind's scale: n/4 rem
    const a = ARBITRARY.exec(c);
    if (a) { px.push(a[2] === 'rem' ? Number(a[1]) * 16 : Number(a[1])); continue; }
    if (DEFERRED.test(c)) deferred = true;
  }
  return { px, deferred };
}

const files = sources(src);
if (files.length < 40) {
  console.error(`check-mark-scale: only ${files.length} source files found - wrong directory?`);
  process.exit(1);
}

// Every element in the tree that draws the app's own mark.
const LOGO_IMPORT = /import\s+([A-Za-z_$][\w$]*)\s+from\s+['"][^'"]*assets\/logo\.svg[^'"]*['"]/g;
const drawings = [];
for (const path of files) {
  const text = blankComments(readFileSync(path, 'utf8'));
  const bindings = [...text.matchAll(LOGO_IMPORT)].map((m) => m[1]);
  for (const tag of text.matchAll(/<[A-Za-z][\w.$]*/g)) {
    const attrs = attrsOf(text, tag.index);
    if (attrs === null) continue;
    const classes = classesIn(attrs);
    // Two ways to draw it: an <img> pointed at the imported url, or the span
    // the raw file is injected into (lib/logoInline.ts, the sidebar egg).
    const isImg = bindings.some((b) => new RegExp(`\\bsrc\\s*=\\s*\\{\\s*${b}\\s*\\}`).test(attrs));
    const isInline = classes.includes('kl-egg');
    if (!isImg && !isInline) continue;
    const { px, deferred } = heightsOf(classes);
    drawings.push({ where: `${show(path)}:${lineOf(text, tag.index)}`, file: show(path), px, deferred });
  }
}

if (drawings.length < 2) {
  console.error(`check-mark-scale: only ${drawings.length} drawing(s) of the app mark found - the tag scanner went blind.`);
  process.exit(1);
}

const rail = drawings.filter((d) => d.file.endsWith('Sidebar.tsx')).flatMap((d) => d.px);
if (!rail.length) {
  console.error('check-mark-scale: the rail draws no mark at a readable height - nothing to measure against.');
  process.exit(1);
}
const ceiling = Math.max(...rail);

const problems = [];
for (const d of drawings) {
  if (d.file.endsWith('Sidebar.tsx')) continue; // the rail is the reference
  for (const px of d.px) {
    if (px <= ceiling) continue;
    problems.push(`${d.where} -> h-${px / 4} is ${px}px, past the rail's own ${ceiling}px`);
  }
}

const deferred = drawings.filter((d) => d.deferred && !d.px.length).length;
problems.sort();
if (problems.length) {
  console.error(`check-mark-scale: ${problems.length} drawing(s) of the app mark larger than the rail's own ${ceiling}px.`);
  console.error('The brand mark in the sidebar is the largest the app draws itself. A repeat on a card does not outgrow it.');
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}

console.log(
  `check-mark-scale: ${drawings.length} drawing(s) of the app mark, none past the rail's own ${ceiling}px` +
    `${deferred ? ` (${deferred} sized by its container, not judged)` : ''}.`,
);
