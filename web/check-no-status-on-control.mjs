// A status colour never lands on something you press (GlimStone 1.12.0, 1.13.0).
//
// WHY THIS EXISTS. ui.tsx's `ButtonKind` has no 'danger' member, and the comment
// above it says why: 1.12.0 took status-red off destructive controls and 1.13.0
// removed the one sanctioned exception instead of relocating it. What warns is
// the question, a window that names the stakes in words and counts, because a
// colour cannot say more than the sentence above it and red on every delete
// teaches people to read past red by the third time. Deleting the variant makes
// tsc the guard, and that guard covers exactly one door. `<Button kind="danger">`
// is a compile error at every call site at once. `<Button className="text-statusFail">`
// is not, a hand-rolled `<button className="... text-statusFail">` is not, and
// the first of those is one word long.
//
// WHY THE PROSE WAS NOT ENOUGH. It is written down three times already, in
// ui.tsx above the union, in index.css beside the tokens, and in GlimStone's own
// changelog, and it keeps coming back anyway because it is convincing one call
// site at a time. "This particular delete really is the dangerous one" is an
// argument nobody loses in isolation; the cost only appears across a whole app,
// where every delete is red and none of them means anything. ui.tsx gives that
// exact reasoning for deleting the variant rather than leaving it unused. This
// file is the same move applied to the doors tsc cannot stand in.
//
// THE HALF THAT IS ACTUALLY HARD. What REPORTS a state keeps its colour and must
// keep it. A status dot, a chip, an error toast, a badge reading "failed": red is
// the whole content of those, and this tree has 194 class lists of that kind. So
// the check needs a line between reporting and operating that it can draw from
// the class list alone, because the class list is all it is given.
//
// The line it draws is this. A class list belongs to a control when it also
// paints an OPERATION STATE. `disabled:`, `enabled:`, `checked:` and
// `indeterminate:` are pseudo-classes that only a form element ever enters.
// `focus:` and `focus-visible:` need an element that can take focus, which means
// a form element, a link, or something handed a tabindex, and all three are
// things a person works. `active:` is press feedback, and there is no reason to
// write it on anything that cannot be pressed. `cursor-pointer` is the
// affordance itself. None of those can be true of a dot that only reports. So
// the question this file asks is not "does this look like a button", which it
// cannot answer, but "does this same class list also style what happens when
// somebody uses it", which it can.
//
// WHAT IS DELIBERATELY LEFT OUT OF THAT SET, and each omission is a false alarm
// that was avoided rather than an oversight.
//
// `hover:` is out. Rows highlight, cards highlight, a chip with a tooltip
// highlights, and not one of them is a lever. Counting it would put this check
// on correct files, and a check that reports on correct files gets switched off,
// which costs more than the rule it was guarding.
//
// `group-*:` and `peer-*:` are out for a stronger version of the same reason:
// they describe ANOTHER element's state. `group-disabled:opacity-50` on a status
// glyph inside a disabled button is correct and always will be.
//
// Arbitrary variants like `[&:has(:focus-visible)]:` are out because this tree
// already holds the counter-example. TaskList.tsx uses that one on a CONTAINER
// reacting to a descendant, not on a control, and a substring test for ":focus"
// inside the brackets would have accused it on the day it was written.
//
// WHAT IT DOES NOT SEE.
//
// The element. It reads class lists and never the JSX tag, so a bare
// 'bg-statusFailBg text-statusFail' handed to a `<button>` is invisible to it.
// That is on purpose and not a gap that wants closing: the shape is LabelBadge,
// whose entire job is to report a connection as up or down and which renders as
// a real `<button>` the moment a call site hands it an onClick. Resolving the tag
// would flag it the day it was written, and the check would be gone by the
// afternoon.
//
// A class list split across two literals. Each quoted string is one list, and a
// template's static text is one list with its `${...}` holes taken out, so a
// button's base classes and a `${err ? 'text-statusFail' : ''}` written beside
// them are read as two and slip past. check-hover-ramp names the same limit, and
// here it is load-bearing rather than incidental: those holes are where OTHER
// complete class lists live, a variant table or a tone map, and welding them to
// the surrounding text is precisely what would produce the LabelBadge false
// alarm above.
//
// index.css itself. Nothing there paints a status token onto anything; those
// tokens appear only in the @theme block that declares the Tailwind colour keys.
// A handwritten `.glim-*` rule that reached for one would not be seen.
//
// It also counts the control class lists it found and fails if that number
// collapses, because a marker set that has stopped matching anything is a green
// check that is no longer checking.
//
// Run by hand or from CI: `node web/check-no-status-on-control.mjs`.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const web = dirname(fileURLToPath(import.meta.url));
const src = join(web, 'src');

/**
 * The status colour utilities, read out of the theme rather than listed here.
 *
 * index.css's @theme block is what decides which `*-statusX` classes Tailwind
 * emits at all, so it is the only honest source for the list. A hand-kept copy
 * in this file would go stale the next time GlimStone adds a tone, and it would
 * go stale silently, which is the failure mode this whole file exists to avoid.
 */
const theme = readFileSync(join(src, 'index.css'), 'utf8');
const STATUS = new Set([...theme.matchAll(/--color-(status[A-Za-z]*)\s*:/g)].map((m) => m[1]));
if (STATUS.size < 6) {
  console.error(
    `check-no-status-on-control: only ${STATUS.size} --color-status* token(s) in src/index.css - did the @theme block move?`,
  );
  process.exit(1);
}

/**
 * Variants that only an element somebody operates can ever be in.
 *
 * Bare only. `group-disabled` and `peer-checked` do not appear here and must
 * not: they are a statement about a DIFFERENT element, and a status glyph
 * inside a disabled button is entitled to both.
 */
const OPERATED = new Set(['disabled', 'enabled', 'active', 'focus', 'focus-visible', 'checked', 'indeterminate']);

/** The affordance written as a utility rather than as a variant. */
const POINTER = 'cursor-pointer';

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
if (files.length < 20) {
  console.error(`check-no-status-on-control: only ${files.length} source files found - wrong directory?`);
  process.exit(1);
}

const QUOTED = /'(?:[^'\\\n]|\\.)*'|"(?:[^"\\\n]|\\.)*"/g;
const TEMPLATE = /`(?:[^`\\]|\\.)*`/g;

/**
 * Every piece of text that is ONE class list, as the chunks it is written in.
 *
 * A plain quoted string is one chunk. A template is all of its static text
 * together, `${...}` holes removed, because those chunks are one element's
 * className however many interpolations are threaded through them. The holes
 * themselves are NOT joined in: what sits inside them is a whole other class
 * list, a variant table value or a tone map entry, and reading those as part of
 * the surrounding text is the false alarm this file's header describes.
 *
 * Each chunk carries its TRUE offset in the file, stepped over the holes rather
 * than past their removal, because a reported line that is three off from the
 * class it names sends the reader to the wrong place in exactly the templates
 * this check is most likely to catch something in.
 */
function classLists(text) {
  const lists = [];
  for (const found of text.matchAll(QUOTED)) lists.push([[found[0], found.index]]);
  for (const found of text.matchAll(TEMPLATE)) {
    const parts = [];
    let cut = 0;
    for (const hole of found[0].matchAll(/\$\{[\s\S]*?\}/g)) {
      parts.push([found[0].slice(cut, hole.index), found.index + cut]);
      cut = hole.index + hole[0].length;
    }
    parts.push([found[0].slice(cut), found.index + cut]);
    lists.push(parts);
  }
  return lists;
}

/**
 * A class token's variant chain, split on the colons that are not inside
 * brackets: `motion-safe:active:scale-[.98]` is three, and `bg-[url(a:b)]` is
 * one. The last element is the utility, everything before it is a variant.
 */
function chain(token) {
  const out = [];
  let depth = 0;
  let cur = '';
  for (const ch of token) {
    if (ch === '[' || ch === '(') depth += 1;
    else if (ch === ']' || ch === ')') depth -= 1;
    if (ch === ':' && depth === 0) {
      out.push(cur);
      cur = '';
      continue;
    }
    cur += ch;
  }
  out.push(cur);
  return out;
}

/** The colour name a utility ends in, with `!`, `/40` and the prefix taken off. */
function colour(utility) {
  const bare = utility.replace(/^!/, '').replace(/!$/, '').split('/')[0];
  const cut = bare.lastIndexOf('-');
  return cut < 0 ? bare : bare.slice(cut + 1);
}

/** Every whitespace-separated token of a class list, with where each one starts. */
function tokens(parts) {
  const out = [];
  for (const [chunk, at] of parts) {
    for (const found of chunk.matchAll(/[^\s'"`]+/g)) out.push([found[0], at + found.index]);
  }
  return out;
}

const problems = [];
let statusLists = 0;
let controlLists = 0;

for (const path of files) {
  const text = readFileSync(path, 'utf8');
  for (const parts of classLists(text)) {
    let status = null;
    let control = null;
    for (const [token, at] of tokens(parts)) {
      const link = chain(token);
      const utility = link[link.length - 1];
      if (!status && STATUS.has(colour(utility))) status = [token, at];
      if (!control) {
        if (utility === POINTER) control = token;
        else for (const variant of link.slice(0, -1)) if (OPERATED.has(variant)) control = token;
      }
    }
    if (status) statusLists += 1;
    if (control) controlLists += 1;
    if (!status || !control) continue;
    const line = text.slice(0, status[1]).split('\n').length;
    problems.push(`${path.slice(src.length + 1)}:${line} ${status[0]} on a control (${control})`);
  }
}
problems.sort();

// A marker set that matches nothing reports nothing, and reports it in green.
// Ten rather than a number close to today's count, because this is meant to
// catch the markers COLLAPSING, a Tailwind release renaming a variant or the
// tokenizer breaking, and not the ordinary drift of a few hand-rolled buttons
// moving into ui.tsx where the shared primitive carries the classes for them.
if (controlLists < 10) {
  console.error(
    `check-no-status-on-control: only ${controlLists} control class list(s) recognised - the markers stopped matching, so this check is blind.`,
  );
  process.exit(1);
}

if (problems.length) {
  console.error(`check-no-status-on-control: ${problems.length} status colour(s) on something you press.`);
  console.error('GlimStone 1.12.0: the question warns, not the colour. Each line names the class and what made the element a control:');
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}

console.log(
  `check-no-status-on-control: ${files.length} files, ${statusLists} status class lists and ${controlLists} control class lists, no class list is both.`,
);
