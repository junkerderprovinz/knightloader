// Checks the two classes a branded control wears, and the marks that must not
// wear them. `.glim-brand-btn` spends three values that only
// `.glim-brand-<name>` supplies. Split them and nothing fails at build time:
// `fill: var(--brand)` resolves to nothing, the mark paints black (invisible
// on the dark theme only) and the hover fill disappears. The pair usually
// breaks when a neighbouring button is copied.
//
// Checks:
//   pairing     `.glim-brand-btn` comes with exactly one `.glim-brand-<name>`,
//               and the other way round.
//   defined     every named brand exists in src/index.css and sets --brand,
//               --brand-fill and --brand-ink.
//   own ground  no multi-coloured mark sits inside `.glim-brand-btn`, whose
//               fill rule reaches every svg and path and would flatten it.
//
// The unit is the element: one className attribute with the file's string
// constants substituted, since call sites write ``${ABOUT_BTN} glim-brand-x``.
//
// Not checked: a vendor mark left unclassed (nothing in the source says a
// drawing is a logo), brand classes built at runtime or by helpers or
// imported constants, whether a hex is the vendor's colour and its contrast,
// and dist/. Two brands chosen by a ternary on one element are reported.
//
// Run: `node web/check-brand-marks.mjs`.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const src = join(here, 'src');
const cssPath = join(src, 'index.css');

const BRAND = /^glim-brand-[a-z0-9-]+$/;
// The control itself, and the brand tile, which check-tile-hover.mjs checks.
const NOT_A_BRAND = new Set(['glim-brand-btn', 'glim-brand-tile']);
const NEEDED = ['--brand', '--brand-fill', '--brand-ink'];
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

// The brand blocks in the stylesheet.
const css = blankComments(readFileSync(cssPath, 'utf8'), false);
const defined = new Map();
for (const block of css.matchAll(/\.glim-brand-([a-z0-9-]+)\s*\{([^}]*)\}/g)) {
  // The lookahead keeps `--brand-coffee` from counting as `--brand`.
  const set = new Set(block[2].match(/--brand(?:-fill|-ink)?(?=\s*:)/g) || []);
  defined.set(block[1], { props: set, line: lineOf(css, block.index) });
}
if (!/\.glim-brand-btn\b/.test(css)) {
  console.error('check-brand-marks: src/index.css defines no .glim-brand-btn at all - wrong file?');
  process.exit(1);
}
if (defined.size < 2) {
  console.error(`check-brand-marks: only ${defined.size} brand block(s) found in src/index.css: the stylesheet scanner went blind.`);
  process.exit(1);
}

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

/** name -> its text, for `const NAME = 'a' + 'b';` and template forms. */
function constants(body) {
  const found = new Map();
  const LITERAL = String.raw`'[^'\n]*'|"[^"\n]*"|\`[^\`]*\``;
  const re = new RegExp(String.raw`\bconst\s+([A-Za-z_$][\w$]*)\s*=\s*((?:${LITERAL})(?:\s*\+\s*(?:${LITERAL}))*)\s*;`, 'g');
  for (const m of body.matchAll(re)) {
    const joined = [...m[2].matchAll(new RegExp(LITERAL, 'g'))].map((p) => p[0].slice(1, -1)).join('');
    found.set(m[1], joined);
  }
  return found;
}

/** Every class named by one className attribute, constants substituted in. */
function classesOf(expr, consts) {
  let e = expr;
  for (let pass = 0; pass < 4; pass += 1) {
    const next = e.replace(/\$\{\s*([A-Za-z_$][\w$]*)\s*\}/g, (whole, id) => (consts.has(id) ? consts.get(id) : whole));
    if (next === e) break;
    e = next;
  }
  const bare = e.trim();
  if (consts.has(bare)) return consts.get(bare).split(/\s+/).filter(Boolean);

  const pieces = [];
  for (const m of e.matchAll(/'([^'\\\n]*)'|"([^"\\\n]*)"/g)) pieces.push(m[1] ?? m[2]);
  for (const m of e.matchAll(/`((?:[^`\\]|\\.)*)`/g)) pieces.push(m[1].replace(/\$\{[\s\S]*?\}/g, ' '));
  return pieces.join(' ').split(/\s+/).filter(Boolean);
}

/** What sits between an element's opening and closing tag, or null if unclear. */
function childrenOf(body, tagStart) {
  const name = /^<([A-Za-z][\w.$]*)/.exec(body.slice(tagStart))?.[1];
  if (!name) return null;
  const open = tagEnd(body, tagStart);
  if (!open) return null;
  if (open.self) return '';
  const marker = new RegExp(`</?${name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}(?=[\\s/>])`, 'g');
  marker.lastIndex = open.end + 1;
  let depth = 1;
  let m;
  while ((m = marker.exec(body))) {
    if (body[m.index + 1] === '/') {
      depth -= 1;
      if (depth === 0) return body.slice(open.end + 1, m.index);
      marker.lastIndex = m.index + m[0].length;
      continue;
    }
    const nested = tagEnd(body, m.index);
    if (!nested) return null;
    if (!nested.self) depth += 1;
    marker.lastIndex = nested.end + 1;
  }
  return null; // unbalanced: say nothing rather than guess at an extent
}

const problems = [];
let controls = 0;

for (const [path, body] of text) {
  const consts = constants(body);
  for (const attr of body.matchAll(/\bclassName\s*=\s*/g)) {
    const at = attr.index + attr[0].length;
    const opener = body[at];
    let expr;
    if (opener === '{') expr = body.slice(at + 1, endOfBraces(body, at) - 1);
    else if (opener === '"' || opener === "'") expr = body.slice(at, endOfString(body, at));
    else continue;

    const classes = classesOf(expr, consts);
    const named = [...new Set(classes.filter((c) => BRAND.test(c) && !NOT_A_BRAND.has(c)))];
    const wearsBtn = classes.includes('glim-brand-btn');
    if (!wearsBtn && named.length === 0) continue;

    // Back to the `<` this attribute belongs to, which is where the element is.
    let tagStart = body.lastIndexOf('<', attr.index);
    while (tagStart > 0 && !/[A-Za-z]/.test(body[tagStart + 1] ?? '')) tagStart = body.lastIndexOf('<', tagStart - 1);
    const where = `${show(path)}:${lineOf(body, attr.index)}`;

    if (wearsBtn && named.length === 0) {
      problems.push(`${where} -> .glim-brand-btn with no .glim-brand-<name>: nothing supplies --brand, so the mark paints black`);
    } else if (!wearsBtn && named.length > 0) {
      problems.push(`${where} -> ${named[0]} with no .glim-brand-btn: the values are set and nothing spends them`);
    } else if (named.length > 1) {
      problems.push(`${where} -> two brands on one element (${named.join(', ')}): the later one wins and the other is a lie`);
    }

    for (const name of named) {
      const brand = defined.get(name.replace(/^glim-brand-/, ''));
      if (!brand) {
        problems.push(`${where} -> ${name} is named here and defined nowhere in src/index.css`);
        continue;
      }
      const missing = NEEDED.filter((prop) => !brand.props.has(prop));
      if (missing.length) {
        problems.push(`index.css:${brand.line} -> .${name} sets no ${missing.join(' and no ')} (named at ${where})`);
      }
    }

    if (!wearsBtn || tagStart < 0) continue;
    controls += 1;
    const inside = childrenOf(body, tagStart);
    if (inside === null) continue;
    const paint = ownGround(inside, path);
    if (paint) {
      problems.push(`${where} -> a mark with its own ground under .glim-brand-btn (${paint}): the button's fill rule would flatten it to one ink`);
    }
  }
}

// index.css defines brand classes, so zero wearers means the scanner went blind.
if (controls === 0) {
  console.error(`check-brand-marks: src/index.css defines ${defined.size} brand classes and no element was found wearing one: the source scanner went blind.`);
  process.exit(1);
}

problems.sort();
if (problems.length) {
  console.error(`check-brand-marks: ${problems.length} problem(s).`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}

console.log(
  `ok: ${controls} branded control(s) across ${files.length} sources, each paired with one of ${defined.size} brand classes in index.css, none wearing a mark of its own colour.`,
);
