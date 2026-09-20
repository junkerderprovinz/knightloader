// Checks that every theme token stands in all three theme blocks of
// src/index.css, and that every colour class has an @theme key.
//
// The palette is written three times: the dark ramp on `:root,
// [data-theme="dark"]`, the system light ramp in `@media
// (prefers-color-scheme: light)`, and `[data-theme="light"]`. A token missing
// from a light block falls back to the dark value on :root, which nobody
// working in dark mode sees.
//
// Tailwind v4 emits a utility only for a key in @theme, so a class such as
// `hover:bg-carbon-hoverRaised` without --color-carbon-hoverRaised produces
// nothing, silently.
//
// One exemption: a token may stand in a single block when it is derived (it
// reads a var() of a token themed in all three blocks) and a comment
// introduces it, as --accent-ink does. One comment covers the declarations
// under it up to the next blank line.
//
// Not checked: whether values are right or readable; class lists split across
// literals or built at runtime (quoted phrases in comments are read too);
// colour utilities outside UTIL, and arbitrary values such as
// `bg-[var(--x)]`; unused @theme keys. If a block's selector is reworded the
// script stops with an error rather than passing.
//
// Run: `node web/check-theme-tokens.mjs`.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const src = join(here, 'src');
const cssPath = join(src, 'index.css');
const css = readFileSync(cssPath, 'utf8');
const cssLines = css.split('\n');

// The CSS with comments blanked to spaces, keeping offsets and line numbers.
const masked = css.replace(/\/\*[\s\S]*?\*\//g, (c) => c.replace(/[^\n]/g, ' '));

const lineAt = (at) => css.slice(0, at).split('\n').length;

const problems = [];
const fail = (message) => {
  console.error(`check-theme-tokens: ${message}`);
  process.exit(1);
};

// The three theme blocks carry the same tokens.

/** Each block, named as the report should name it, and found by its selector. */
const BLOCKS = [
  { name: ':root (dark)', open: /:root\s*,\s*\[data-theme=(["'])dark\1\]\s*\{/g },
  { name: '@media light', open: /:root:not\(\[data-theme=(["'])dark\1\]\)\s*\{/g },
  { name: '[data-theme="light"]', open: /(?::root)?\[data-theme=(["'])light\1\]\s*\{/g },
];

/** From just after an opening brace to its match. */
function closes(from) {
  let depth = 1;
  for (let i = from; i < masked.length; i++) {
    if (masked[i] === '{') depth++;
    else if (masked[i] === '}' && --depth === 0) return i;
  }
  return -1;
}

/**
 * introduced reports whether a comment introduces this declaration, walking
 * back over the declarations above it, since one comment covers a run up to a
 * blank line.
 */
function introduced(at) {
  for (let i = lineAt(at) - 2; i >= 0; i--) {
    const text = cssLines[i].trim();
    if (text === '') return false;
    if (text.endsWith('*/')) return true;
    if (!/^--[A-Za-z0-9-]+\s*:/.test(text)) return false;
  }
  return false;
}

const DECL = /(--[A-Za-z0-9-]+)\s*:\s*([^;{}]*);/g;

/** token -> block name -> { at, value } */
const tokens = new Map();

for (const block of BLOCKS) {
  const found = [...masked.matchAll(block.open)];
  if (found.length !== 1) {
    fail(
      `expected exactly one "${block.name}" block in src/index.css, found ${found.length}. ` +
        'The selector was reworded or the block was removed; this check cannot run until the pattern above is brought back in step.',
    );
  }
  const from = found[0].index + found[0][0].length;
  const to = closes(from);
  if (to < 0) fail(`the "${block.name}" block in src/index.css is never closed.`);

  const body = masked.slice(from, to);
  for (const decl of body.matchAll(DECL)) {
    const at = from + decl.index;
    if (!tokens.has(decl[1])) tokens.set(decl[1], new Map());
    // From the masked copy, so a token named in an inline comment is not read.
    tokens.get(decl[1]).set(block.name, { at, value: decl[0] });
  }
}

const names = BLOCKS.map((b) => b.name);
if (tokens.size < 20) {
  fail(`only ${tokens.size} theme tokens found in src/index.css, which is too few to be the palette.`);
}

/** A token standing in every block is safe to derive from. */
const everywhere = new Set(
  [...tokens].filter(([, seen]) => names.every((n) => seen.has(n))).map(([token]) => token),
);

let exempt = 0;
for (const [token, seen] of tokens) {
  const missing = names.filter((n) => !seen.has(n));
  if (!missing.length) continue;

  const decl = [...seen.values()][0];
  const reads = [...decl.value.matchAll(/var\(\s*(--[A-Za-z0-9-]+)/g)].map((m) => m[1]);
  const derived = [...seen.values()].every(
    (d) =>
      [...d.value.matchAll(/var\(\s*(--[A-Za-z0-9-]+)/g)].some((m) => everywhere.has(m[1])),
  );
  const said = [...seen.values()].every((d) => introduced(d.at));

  if (derived && said) {
    exempt++;
    continue;
  }
  const why = derived
    ? `derived from ${reads.filter((r) => everywhere.has(r)).join(' and ')}, but no comment above it says so`
    : 'a value of its own, so each theme needs one';
  problems.push(
    `src/index.css:${lineAt(decl.at)} ${token} stands in ${[...seen.keys()].join(' and ')}, ` +
      `missing from ${missing.join(' and ')} (${why})`,
  );
}

// Every colour class used has a key in @theme.

const theme = masked.match(/@theme\s*\{/);
if (!theme) fail('no @theme block in src/index.css.');
const themeBody = masked.slice(theme.index + theme[0].length, closes(theme.index + theme[0].length));
const keys = new Set([...themeBody.matchAll(/--color-([A-Za-z0-9-]+)\s*:/g)].map((m) => m[1]));
if (keys.size < 10) fail(`only ${keys.size} --color-* keys in @theme, which is too few to be the palette.`);

/** The colour utilities (divide and ring are in use too) and the three stems
 *  of this app's palette. */
const UTIL =
  /^(?:bg|text|fill|stroke|shadow|caret|accent|decoration|placeholder|from|via|to|outline|ring|ring-offset|border|border-[trblxyse]|divide|divide-[xy])-((?:carbon-|accent|status)[A-Za-z0-9]*)$/;

function sources(dir) {
  const found = [];
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) found.push(...sources(path));
    else if (/\.(tsx?|html)$/.test(entry)) found.push(path);
  }
  return found;
}

const files = [...sources(src), join(here, 'index.html')];
if (files.length < 20) fail(`only ${files.length} source files found, wrong directory?`);

/**
 * Quoted strings and the static parts of template strings. Double quotes may
 * span lines, since a JSX `className="…"` wraps; single quotes stay on one
 * line, so an apostrophe in a comment does not open a string.
 */
const QUOTED = /'(?:[^'\\\n]|\\.)*'|"(?:[^"\\]|\\.)*"/g;
const TEMPLATE = /`(?:[^`\\]|\\.)*`/g;

/** Every piece of text that is one class list, with where it starts. */
function classLists(text) {
  const pieces = [];
  for (const found of text.matchAll(QUOTED)) pieces.push([found[0], found.index]);
  for (const found of text.matchAll(TEMPLATE)) {
    // QUOTED already took the strings inside `${…}`; the static text around
    // them is a class list of its own. Offsets keep the holes' lengths so line
    // numbers stay right.
    const body = found[0];
    let at = 0;
    for (const hole of body.matchAll(/\$\{[\s\S]*?\}/g)) {
      pieces.push([body.slice(at, hole.index), found.index + at]);
      at = hole.index + hole[0].length;
    }
    pieces.push([body.slice(at), found.index + at]);
  }
  return pieces;
}

/** A written class stripped back to the utility Tailwind has to emit. */
function utility(word) {
  // Variants such as `hover:` or `data-[state=on]:` end at the last colon.
  const bare = word.slice(word.lastIndexOf(':') + 1).replace(/^[!-]+/, '');
  if (bare.includes('[')) return null; // an arbitrary value reaches the token directly
  return bare.replace(/\/[\w.[\]%]+$/, ''); // the /opacity modifier is not part of the key
}

const seenClasses = new Set();
for (const path of files) {
  const text = readFileSync(path, 'utf8');
  for (const [piece, at] of classLists(text)) {
    for (const word of piece.matchAll(/[^\s'"`]+/g)) {
      const bare = utility(word[0]);
      if (!bare) continue;
      const hit = UTIL.exec(bare);
      if (!hit) continue;
      seenClasses.add(bare);
      if (keys.has(hit[1])) continue;
      // The class's own line, not the line where its list opened.
      const line = text.slice(0, at + word.index).split('\n').length;
      problems.push(
        `${path.slice(here.length + 1).replace(/\\/g, '/')}:${line} ` +
          `${bare} needs --color-${hit[1]} in @theme, which has no such key, so Tailwind emits nothing for it`,
      );
    }
  }
}

// A double-quoted list inside a template is read by both patterns.
const unique = [...new Set(problems)].sort();
problems.length = 0;
problems.push(...unique);

if (problems.length) {
  console.error(`check-theme-tokens: ${problems.length} problem(s).`);
  console.error('A token that stands in one theme block is absent from the other two, or a class has no key:');
  for (const p of problems.slice(0, 40)) console.error(`  ${p}`);
  if (problems.length > 40) console.error(`  ... and ${problems.length - 40} more`);
  process.exit(1);
}

console.log(
  `ok: ${BLOCKS.length} theme blocks agree on ${tokens.size} tokens ` +
    `(${exempt} derived and documented), ${keys.size} @theme keys cover ` +
    `${seenClasses.size} colour classes across ${files.length} files`,
);
