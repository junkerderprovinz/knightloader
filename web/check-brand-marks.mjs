// Checks the marks on the README buttons, which paint a single-colour mark at
// rest through a class on the mark's box (`markClass`, such as
// `glim-paypal-mark`) and light the whole button in the brand's colour under
// the pointer (check-tile-hover.mjs). Split the pieces and nothing fails at
// build time: a class index.css does not define leaves the mark in the
// button's ink, and one put on a mark that brings its own colours reaches none
// of them.
//
// Checks:
//   gone        the brand button classes GlimStone dropped, `.glim-brand-btn`
//               and its `.glim-brand-<name>` blocks, are in no stylesheet rule
//               and no source; `.glim-brand-tile` is the tile and stays.
//   defined     every `markClass` names a class index.css defines as a colour.
//   own ground  a button with a `markClass` holds a mark that paints in
//               currentColor, followed through components and the markup
//               constants passed to BrandMark.
//   every mark  every button on the About card carries a mark or the vendor's
//               artwork, passed at its call site.
//
// Not checked: a mark with its own colours worn without a class, which is
// allowed; brand classes built at runtime or by helpers; whether a hex is the
// vendor's colour and its contrast, which check-tile-hover.mjs measures; and
// dist/.
//
// Run: `node web/check-brand-marks.mjs`.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const src = join(here, 'src');
const cssPath = join(src, 'index.css');

const show = (path) => path.slice(src.length + 1).split('\\').join('/');
const lineOf = (text, at) => text.slice(0, at).split('\n').length;

// The scanner below is mutually recursive, because templates hold `${…}`
// expressions that hold more strings; Help.tsx nests a template inside a
// template inside an attribute.

/** Index just past the string starting at `i`. */
function endOfString(text, i) {
  const quote = text[i];
  i += 1;
  while (i < text.length) {
    const c = text[i];
    if (c === '\\') { i += 2; continue; }
    if (c === quote) return i + 1;
    if (quote === '`' && c === '$' && text[i + 1] === '{') { i = endOfBraces(text, i + 1); continue; }
    // A newline cannot occur in '' or "", so the quote was not one (a lone '
    // in a regex, say). Stop at the line end.
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

/** Where the tag opening at `i` ends, and whether it closed itself. */
function tagEnd(text, i) {
  let at = i + 1;
  while (at < text.length) {
    const c = text[at];
    if (c === '"' || c === "'" || c === '`') { at = endOfString(text, at); continue; }
    if (c === '{') { at = endOfBraces(text, at); continue; }
    if (c === '>') return { end: at, self: text[at - 1] === '/' };
    at += 1;
  }
  return null;
}

/**
 * blankComments replaces comments with spaces, keeping offsets and line
 * numbers, so hex colours mentioned in comments are not read as paint.
 */
function blankComments(text, slashSlash = true) {
  const out = text.split('');
  let i = 0;
  while (i < text.length) {
    const c = text[i];
    if (c === '"' || c === "'" || c === '`') { i = endOfString(text, i); continue; }
    if (slashSlash && c === '/' && text[i + 1] === '/') {
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

// The mark classes in the stylesheet, and the brand button classes that must be
// gone from it.
const css = blankComments(readFileSync(cssPath, 'utf8'), false);
const defined = new Set([...css.matchAll(/(?:^|\n)\.(glim-[a-z0-9]+-mark)\s*\{\s*color\s*:/g)].map((m) => m[1]));
if (defined.size < 5) {
  console.error(`check-brand-marks: only ${defined.size} mark class(es) found in src/index.css: the stylesheet scanner went blind.`);
  process.exit(1);
}
const leftover = [...css.matchAll(/\.glim-brand-(?!tile\b)[a-z0-9-]+/g)];

function sources(dir) {
  const found = [];
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) found.push(...sources(path));
    else if (/\.tsx?$/.test(entry)) found.push(path);
  }
  return found;
}

const files = sources(src);
if (files.length < 50) {
  console.error(`check-brand-marks: only ${files.length} source files found - wrong directory?`);
  process.exit(1);
}
const text = new Map();
for (const path of files) text.set(path, blankComments(readFileSync(path, 'utf8')));

/**
 * What each file declares, and where each name it uses is imported from.
 * Resolved per file, because names repeat across files (ColumnMenu.tsx and
 * donateMarks.tsx both declare a `Mark`). A declaration runs to the next one
 * at column zero, which is enough to see what colours it paints.
 */
const TOP = /^(?:export\s+)?(?:const|let|var|function|class|type|interface|enum)\s+([A-Za-z_$][\w$]*)/gm;
const IMPORTS = /import\s+(?:type\s+)?([\s\S]*?)\s+from\s+['"]([^'"]+)['"]/g;
const declares = new Map();
const imports = new Map();

function moduleAt(from, spec) {
  const base = join(dirname(from), spec);
  for (const guess of [`${base}.tsx`, `${base}.ts`, join(base, 'index.tsx'), join(base, 'index.ts')]) {
    if (text.has(guess)) return guess;
  }
  return null;
}

for (const [path, body] of text) {
  const decls = [...body.matchAll(TOP)];
  const mine = new Map();
  decls.forEach((decl, i) => {
    mine.set(decl[1], body.slice(decl.index, i + 1 < decls.length ? decls[i + 1].index : body.length));
  });
  declares.set(path, mine);

  const from = new Map();
  for (const line of body.matchAll(IMPORTS)) {
    const target = line[2].startsWith('.') ? moduleAt(path, line[2]) : null;
    if (!target) continue; // react and the other packages: not where a mark lives
    const braced = /\{([^}]*)\}/.exec(line[1]);
    for (const part of braced ? braced[1].split(',') : []) {
      const [origin, alias] = part.trim().replace(/^type\s+/, '').split(/\s+as\s+/);
      if (origin) from.set((alias ?? origin).trim(), { path: target, name: origin.trim() });
    }
  }
  imports.set(path, from);
}

/** The declaration a name means in this file: its own first, then its imports. */
function declarationOf(path, name) {
  const mine = declares.get(path);
  if (mine?.has(name)) return { path, body: mine.get(name) };
  const imported = imports.get(path)?.get(name);
  const theirs = imported && declares.get(imported.path);
  if (theirs?.has(imported.name)) return { path: imported.path, body: theirs.get(imported.name) };
  return null;
}

// Whether a drawing brings its own colours. The attribute name has to end at
// the equals sign, so fillRule and stroke-width are not paint. Any gradient
// counts as more than one colour.
const PAINT = /\b(fill|stroke|stopColor|stop-color)\s*=\s*(?:"([^"]*)"|'([^']*)'|\{([^}]*)\})/g;
const FLAT = /^(currentColor|none|inherit|transparent)$/i;

function literalPaint(body) {
  if (/<(?:linearGradient|radialGradient)\b/.test(body)) return 'a gradient';
  for (const paint of body.matchAll(PAINT)) {
    if (paint[4] !== undefined) {
      // `fill={tone}` is up to the caller; a quoted colour inside one counts.
      if (/['"]\s*(?:#|rgb|hsl)/i.test(paint[4])) return `${paint[1]}={${paint[4].trim()}}`;
      continue;
    }
    const value = (paint[2] ?? paint[3]).trim();
    if (value === '' || FLAT.test(value) || value.startsWith('var(')) continue;
    return `${paint[1]}="${value}"`;
  }
  return null;
}

/** The first colour of its own in this markup, following the components in it. */
function ownGround(body, path, seen = new Set(), depth = 0) {
  const direct = literalPaint(body);
  if (direct) return direct;
  if (depth >= 5) return null;
  for (const used of body.matchAll(/<([A-Z][\w$]*)/g)) {
    const found = declarationOf(path, used[1]);
    if (!found || seen.has(`${found.path}#${used[1]}`)) continue;
    seen.add(`${found.path}#${used[1]}`);
    const deeper = ownGround(found.body, found.path, seen, depth + 1);
    if (deeper) return `${used[1]} draws ${deeper}`;
  }
  return null;
}

/** Every `<ReadmeButton ... />` in a file, whole, with where it starts. */
function readmeButtons(body) {
  const out = [];
  for (const m of body.matchAll(/<ReadmeButton\b/g)) {
    const end = tagEnd(body, m.index);
    if (end) out.push({ at: m.index, tag: body.slice(m.index, end.end + 1) });
  }
  return out;
}

/** The expression inside `attr={...}` on a tag, or null. */
function attrExpr(tag, attr) {
  const m = new RegExp(`(?<![\\w-])${attr}=\\{`).exec(tag);
  if (!m) return null;
  const open = m.index + m[0].length - 1;
  return tag.slice(open + 1, endOfBraces(tag, open) - 1);
}

/** The first colour of its own a mark expression paints, following components and markup constants. */
function markPaint(expr, path) {
  for (const svg of expr.matchAll(/\bsvg=\{\s*([A-Za-z_$][\w$]*)\s*\}/g)) {
    const found = declarationOf(path, svg[1]);
    const paint = found && literalPaint(found.body);
    if (paint) return `${svg[1]} draws ${paint}`;
  }
  return ownGround(expr, path);
}

const problems = leftover.map((m) => `index.css:${lineOf(css, m.index)} -> ${m[0]}: the brand button classes are gone, a README button names its brand instead`);
let buttons = 0;
let classed = 0;

for (const [path, body] of text) {
  const where = (at) => `${show(path)}:${lineOf(body, at)}`;
  for (const m of body.matchAll(/\bglim-brand-(?!tile\b)[a-z0-9-]+/g)) {
    problems.push(`${where(m.index)} -> ${m[0]}: the brand button classes are gone, a README button names its brand instead`);
  }
  for (const { at, tag } of readmeButtons(body)) {
    buttons += 1;
    const mark = attrExpr(tag, 'mark');
    const art = attrExpr(tag, 'art');
    const cls = /\bmarkClass="([\w-]+)"/.exec(tag)?.[1];
    if (cls) {
      classed += 1;
      if (!defined.has(cls)) problems.push(`${where(at)} -> markClass ${cls}, which src/index.css does not define`);
      if (!mark) problems.push(`${where(at)} -> markClass ${cls} on a button without a mark`);
      else {
        const paint = markPaint(mark, path);
        if (paint) problems.push(`${where(at)} -> ${cls} on a mark with colours of its own (${paint}): the class reaches none of them`);
      }
    }
    // The About card: every button carries a mark, passed at its call site.
    if (show(path) === 'pages/settings/Help.tsx' && !mark && !art) {
      problems.push(`${where(at)} -> an About card button without a mark: a row where four wear a logo and one does not reads as a missing image`);
    }
  }
}

if (buttons < 12 || classed < 5) {
  console.error(`check-brand-marks: only ${buttons} README buttons and ${classed} mark classes found - the source scanner went blind.`);
  process.exit(1);
}

problems.sort();
if (problems.length) {
  console.error(`check-brand-marks: ${problems.length} problem(s).`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}

console.log(
  `ok: ${buttons} README buttons across ${files.length} sources, ${classed} of them painting a single-colour mark with one of ${defined.size} mark classes in index.css, and no brand button class left.`,
);
