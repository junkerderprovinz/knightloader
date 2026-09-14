// A card is a surface, not a control: no page hands one a hover state.
//
// WHAT BREAKS WITHOUT IT. GlimStone's motion engine names the five things that
// are allowed to move - arriving, ongoing, refused - and then says what is not:
// "a settled page doesn't idle-animate, a card doesn't breathe, hover states
// change instantly". Rule 21 says the same thing from the colour side: hover
// moves UP THE SURFACE RAMP, which is a TONE and never a position. A card lifted
// under the pointer disobeys both, and the way it disobeys them is invisible in
// a screenshot, because a screenshot has no pointer in it.
//
// It is worse than decorative. The instance cards sit in a grid row that shares
// one top edge, and lifting one of them by two pixels breaks that edge for as
// long as the pointer is over it - measured at 16px vs 14px on a row of three
// (jdp: "wenn man auf die card hoovert wandert sie nach oben"). And a lift is a
// PROMISE: it is the gesture a whole-card link makes. The card that carried it
// here could not be clicked at all - clicking its body did nothing, because the
// only click target it ever had was the Open button inside it.
//
// A SCRIPT RATHER THAN A NOTE, and the note is the reason. The rule is written
// out twice in the design language, in the two sections anybody styling a hover
// would open, and it still shipped: the prop reads `hover`, it lives on the
// shared Card, and passing it looks from the call site exactly like asking for
// the house's own hover - the one thing it is not. Nothing fails, nothing type-
// errors, and the defect only exists while a pointer is resting on the element.
// Prose that sits in the file somebody is about to copy from does not stop this.
//
// WHAT IT CHECKS. One question: does any `<Card …>` element carry a `hover`
// attribute. That is the whole rule, and the call site is deliberately the unit
// rather than the class list. What a card does under the pointer is decided by
// the page that renders it, so the page is where the rule has to bite; a check
// written against the rendered class list would be accusing the shared
// component of a choice it does not make.
//
// WHAT IT DELIBERATELY DOES NOT SEE.
//
//   The `hover` prop's own declaration and implementation in ui.tsx. With no
//   call site left it is dead code, and dead code is not a defect this check
//   can honestly report as one - but it is also the only thing standing between
//   this rule and a wider one. Once the prop is gone from ui.tsx, the question
//   can widen to "no element carrying `glim-card` moves vertically on hover",
//   which would also catch a lift written by hand into a `className`.
//   A hover lift spelled out in a Card's own `className` today. See above: the
//   rule can only widen once the prop it would collide with is removed.
//   A HORIZONTAL hover nudge, which is a different thing and is allowed. The
//   sidebar's rail rows carry `motion-safe:hover:translate-x-0.5` with a
//   reasoned comment beside them: a row that ALSO takes a colour step gets two
//   pixels along the reading direction, toward the thing it opens, in a
//   vertical stack where a sideways move breaks no shared edge. None of those
//   three conditions holds for a card in a grid row.
//
// Run by hand or from CI: `node web/check-card-hover.mjs`.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const src = join(dirname(fileURLToPath(import.meta.url)), 'src');
const show = (path) => path.slice(src.length + 1).split('\\').join('/');

function sources(dir) {
  const found = [];
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) found.push(...sources(path));
    else if (/\.tsx$/.test(entry)) found.push(path);
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
    // A raw newline cannot appear in '' or "": the opening quote was not one.
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

/**
 * The attribute text of the tag opening at `i`, or null if it never closes.
 *
 * Strings and `{…}` are skipped whole rather than scanned for `>`, because an
 * attribute value routinely contains one: `onOpen={() => navigate(...)}` has an
 * arrow in it, and a scan that stopped at the first `>` would cut the tag in
 * half and miss every attribute written after the first handler.
 */
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

const files = sources(src);
if (files.length < 40) {
  console.error(`check-card-hover: only ${files.length} .tsx files found - wrong directory?`);
  process.exit(1);
}

// `<Card` and not `<CardSomething`: a component whose name merely starts with
// the word is a different component with different rules.
const CARD = /<Card(?![A-Za-z0-9_$])/g;
const HOVER = /(^|[\s{])hover(\s*=|[\s/]|$)/;

const problems = [];
let cards = 0;
for (const path of files) {
  const text = blankComments(readFileSync(path, 'utf8'));
  for (const found of text.matchAll(CARD)) {
    const attrs = attrsOf(text, found.index);
    if (attrs === null) continue; // unbalanced: say nothing rather than guess
    cards += 1;
    if (!HOVER.test(attrs)) continue;
    const line = text.slice(0, found.index).split('\n').length;
    problems.push(`${show(path)}:${line} -> <Card hover>: a card is a surface, not a control`);
  }
}

// Cards exist in this app by the dozen. Nought found means the tag scanner went
// blind, and a check reporting "ok: 0" is the one failure it cannot catch about
// itself.
if (cards === 0) {
  console.error('check-card-hover: no <Card> element found at all - the tag scanner went blind.');
  process.exit(1);
}

problems.sort();
if (problems.length) {
  console.error(`check-card-hover: ${problems.length} card(s) asked to react to the pointer.`);
  console.error('A card does not move or recolour under the pointer - the control inside it does.');
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}

console.log(`check-card-hover: ${cards} <Card> element(s) across ${files.length} sources, none taking a hover state.`);
