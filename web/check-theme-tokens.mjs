// Every theme token stands in all three theme blocks, and every colour class has a key.
//
// WHAT GOES WRONG WITHOUT IT. src/index.css carries the palette three times over:
// the dark ramp on `:root, [data-theme="dark"]`, the system light ramp inside
// `@media (prefers-color-scheme: light)`, and the explicit light ramp on
// `[data-theme="light"]`. A token added to one of them and not to the other two is
// invisible to the person who added it, because somebody works in ONE colour mode
// and sees the one block they touched. The other two modes then fall back to
// whatever the cascade happens to leave on the element, which here is the dark
// value sitting on :root, so a light-mode reader gets a dark grey chip on a white
// card and nothing in the build has an opinion about it. The file's own comments
// say this out loud twice, once beside --status-warn-bg-strong and once beside the
// brand marks ("an explicit light choice has to reach them too"), and a rule that
// has to be restated beside individual tokens is a rule that has already been got
// wrong.
//
// THE SECOND HALF IS THE ONE THAT ALREADY COST SOMETHING. Tailwind v4 emits a
// utility only for a key that stands in the @theme block, so
// `hover:bg-carbon-hoverRaised` written against a missing --color-carbon-hoverRaised
// produces no class at all. Nothing notices: not tsc, which sees a string, not the
// build, which holds no list of intended classes, and not the page, because a class
// that produces nothing looks exactly like a class that was never added.
// CryptoDonateDialog's selected-chain chip carried exactly that and had no hover for
// months, and the same shape came back a second time with --color-statusWarnBgStrong.
//
// WHY PROSE DID NOT REACH IT. Both halves are already written down in index.css, in
// comments sitting directly above the two tokens that were added to repair the two
// misses. Both notes are therefore the RECORD of a mistake rather than the thing
// that stopped it: a rule stated where the fix landed is read by the person who
// landed it and by nobody afterwards. web/check-hover-ramp.mjs exists in this repo
// for the same reason, one layer up.
//
// THE ONE EXEMPTION, and it is narrow on purpose. A token may stand in a single
// block when its value is DERIVED, meaning it reads a var() of another token that
// itself stands in all three blocks, and when a comment introduces it. Derivation
// is the half with teeth. --accent-ink is
// `color-mix(in srgb, var(--accent) var(--ink-mix), black)`, and because both
// --accent and --ink-mix are themed, that one declaration is already correct in
// every mode; a light twin would be a copy of a formula, which is the shape drift
// takes. The comment is the second gate, and it is a gate rather than the whole
// test because this file is mostly comments: a check that exempted any token with a
// comment near it would exempt nearly every token here and guard nothing. A derived
// token with nothing written above it is still reported, since a derived value
// nobody explained is indistinguishable from one somebody forgot to mirror.
//
// WHAT IT DELIBERATELY DOES NOT SEE.
//
// It checks that a token is PRESENT in all three blocks, never that its value is
// right. A light block that copies a dark hex straight across passes here, and so
// does an unreadable pairing; contrast is a different check and a different file.
//
// It reads class lists out of string literals, so a list split across two literals
// and joined, or assembled at runtime from a lookup table, is read as pieces or not
// at all. check-hover-ramp.mjs records the same limit for the same reason. The
// flip side is that a quoted phrase inside a comment is read as a class list too,
// which is harmless as long as comments quote sentences rather than dead class
// names.
//
// It knows the colour utilities listed in UTIL below and no others, and it ignores
// arbitrary values such as `bg-[var(--carbon-surface2)]`, which reach the token
// directly and need no @theme key at all.
//
// It does not report an @theme key that nothing uses. A dead key costs a line of
// CSS and misleads nobody, and flagging them would be the noise that teaches people
// to skip this check.
//
// The comment gate asks whether a comment is there, never whether it says anything
// about the exemption, and one comment covers the run of declarations under it up to
// the next blank line, because that is how these blocks are already grouped. So a
// derived token appended to an already-commented run inherits that cover. What it
// inherits is only the SECOND gate: derivation is still checked on its own, so the
// worst such a token can be is correct and unexplained.
//
// The three blocks are found by their selector text, and if a selector is reworded
// this script STOPS with an error instead of passing. That is the point: a guard
// that quietly finds nothing left to guard is worse than no guard, because the
// green line it prints is a claim it is no longer making.
//
// Run by hand or from CI: `node web/check-theme-tokens.mjs`.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const src = join(here, 'src');
const cssPath = join(src, 'index.css');
const css = readFileSync(cssPath, 'utf8');
const cssLines = css.split('\n');

/**
 * The same text with every comment blanked to spaces. Lengths and newlines are
 * kept, so an offset into this string is the same offset into the original and
 * line numbers stay honest, while a brace or a colon inside a comment can no
 * longer be mistaken for code.
 */
const masked = css.replace(/\/\*[\s\S]*?\*\//g, (c) => c.replace(/[^\n]/g, ' '));

const lineAt = (at) => css.slice(0, at).split('\n').length;

const problems = [];
const fail = (message) => {
  console.error(`check-theme-tokens: ${message}`);
  process.exit(1);
};

/* ---------------------------------------------------------------------------
   Half one: the three theme blocks carry the same tokens.
   --------------------------------------------------------------------------- */

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
 * True when a comment introduces this declaration.
 *
 * It walks back over the declarations directly above it and stops at the first
 * line that is neither, because one comment routinely introduces a RUN of
 * tokens: --accent-ink's own explanation sits above --ink-mix and covers the
 * pair. A blank line ends the run, which is how this file already separates one
 * group from the next.
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
    // The matched text, which came from the comment-masked copy, so a token
    // NAMED in an inline comment inside a value cannot be mistaken for a token
    // the value actually reads.
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

/* ---------------------------------------------------------------------------
   Half two: every colour class used has a key in @theme.
   --------------------------------------------------------------------------- */

const theme = masked.match(/@theme\s*\{/);
if (!theme) fail('no @theme block in src/index.css.');
const themeBody = masked.slice(theme.index + theme[0].length, closes(theme.index + theme[0].length));
const keys = new Set([...themeBody.matchAll(/--color-([A-Za-z0-9-]+)\s*:/g)].map((m) => m[1]));
if (keys.size < 10) fail(`only ${keys.size} --color-* keys in @theme, which is too few to be the palette.`);

/**
 * The colour utilities, and the three stems this app's palette uses. Wider than
 * the five the rule is usually quoted as (bg/text/border/fill/stroke), because
 * divide-carbon-border and ring-carbon-border are live in this app today and a
 * missing key kills them in exactly the same silence.
 */
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
 * Plain quoted strings, and the static halves of template strings.
 *
 * THE TWO QUOTES ARE NOT TREATED ALIKE, and that asymmetry is the difference
 * between seeing this app's classes and missing them. A JS string literal of
 * either quote ends at the end of its line, but a JSX attribute is not a JS
 * string: `className="…"` wraps over as many lines as it needs, and
 * LogViewerCard's own hover:bg-carbon-hoverRaised sits on the second line of
 * one. Written line-bound, this check reported two of that class's three call
 * sites and printed a green line about the third. Single quotes stay
 * line-bound on purpose, because an apostrophe in an English comment would
 * otherwise open a "string" that runs to the next apostrophe several lines
 * down and drag everything between them into the scan.
 */
const QUOTED = /'(?:[^'\\\n]|\\.)*'|"(?:[^"\\]|\\.)*"/g;
const TEMPLATE = /`(?:[^`\\]|\\.)*`/g;

/** Every piece of text that is ONE class list, with where it starts. */
function classLists(text) {
  const pieces = [];
  for (const found of text.matchAll(QUOTED)) pieces.push([found[0], found.index]);
  for (const found of text.matchAll(TEMPLATE)) {
    // A template's `${…}` holds its own quoted strings, and QUOTED above has
    // already taken those one by one. What is left is the static text around
    // them, which is a class list of its own. The hole's own length is counted
    // back in, or every line number after the first interpolation is reported
    // short by the width of the expression.
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
  // Variants first: `hover:`, `dark:`, `data-[state=on]:` and friends all end at
  // the last colon, and what follows is the utility itself.
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
      // The line of the CLASS, not of the quote that opened the list: a
      // className attribute runs over four lines here often enough that the
      // difference is the difference between a report somebody can act on and
      // one they have to go hunting in.
      const line = text.slice(0, at + word.index).split('\n').length;
      problems.push(
        `${path.slice(here.length + 1).replace(/\\/g, '/')}:${line} ` +
          `${bare} needs --color-${hit[1]} in @theme, which has no such key, so Tailwind emits nothing for it`,
      );
    }
  }
}

// A double-quoted list written inside a template is taken twice, once by each
// pattern. Deduped rather than made mutually exclusive, because the same class
// reported twice at the same line is noise and the overlap is harmless.
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
