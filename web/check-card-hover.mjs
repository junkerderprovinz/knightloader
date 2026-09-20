// A card is a surface, not a control: no page hands one a hover state.
//
// GlimStone's motion engine says a settled page does not idle-animate, a card
// does not breathe and hover states change instantly. Rule 21 says the same
// from the colour side: hover moves up the surface ramp, which is a tone and
// never a position. A lifted card disobeys both, and it does so invisibly in a
// screenshot, because a screenshot has no pointer in it. The instance cards
// share one top edge across a grid row, and lifting one of them by two pixels
// breaks that edge for as long as the pointer rests on it. A lift is also the
// gesture a whole-card link makes, on a card whose only click target is the
// Open button inside it.
//
// It takes a script because the prop reads `hover`, lives on the shared Card,
// and from the call site looks like asking for the house's own hover: nothing
// type-errors, and the defect exists only while a pointer rests on the element.
//
// The unit is the call site rather than the class list. What a card does under
// the pointer is the page's choice, so the page is where the rule bites; a
// check written against the rendered classes would accuse the shared component
// of a decision it does not make.
//
// Not seen: the `hover` prop's own declaration in ui.tsx, which with no call
// site left is dead code rather than a defect; a lift written by hand into a
// Card's `className`, which the question can only widen to cover once the prop
// is gone; and a horizontal nudge, which is allowed. The sidebar's rail rows
// carry `motion-safe:hover:translate-x-0.5` with their own reasoning: a row
// that also takes a colour step moves two pixels along the reading direction,
// in a vertical stack where a sideways move breaks no shared edge.
//
// Run: `node web/check-card-hover.mjs`.
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

// Cards exist here by the dozen, so none found means the tag scanner went
// blind.
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
