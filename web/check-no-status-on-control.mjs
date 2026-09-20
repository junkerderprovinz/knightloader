// A status colour never lands on something you press (GlimStone 1.12.0, 1.13.0).
//
// ui.tsx's `ButtonKind` has no 'danger' member: what warns is the question, a
// window naming the stakes in words and counts, because a colour cannot say
// more than the sentence above it and red on every delete teaches people to
// read past red. Deleting the variant makes tsc the guard, and that guard
// covers one door. `<Button kind="danger">` is a compile error at every call
// site at once; `<Button className="text-statusFail">` is not, and a
// hand-rolled `<button className="... text-statusFail">` is not either.
//
// The rule stands in ui.tsx above the union, in index.css beside the tokens and
// in GlimStone's changelog, and it comes back anyway, because it convinces one
// call site at a time. "This particular delete really is the dangerous one" is
// an argument nobody loses in isolation, while the cost appears across a whole
// app where every delete is red and none of them means anything.
//
// The hard half is that what reports a state keeps its colour: a status dot, a
// chip, an error toast, a badge reading "failed". So the line between reporting
// and operating has to be drawn from the class list, which is all this is
// given. A class list belongs to a control when it also paints an operation
// state. `disabled:`, `enabled:`, `checked:` and `indeterminate:` are
// pseudo-classes only a form element enters; `focus:` and `focus-visible:` need
// an element that can take focus, so a form element, a link or something handed
// a tabindex; `active:` is press feedback; `cursor-pointer` is the affordance
// itself. None of them can be true of a dot that only reports. The question is
// therefore not whether something looks like a button, which cannot be answered
// from here, but whether the same class list styles what happens when somebody
// uses it.
//
// Left out of that set, each one a false alarm avoided: `hover:`, because rows,
// cards and a chip with a tooltip all highlight without being levers;
// `group-*:` and `peer-*:`, which describe another element's state, so
// `group-disabled:opacity-50` on a status glyph inside a disabled button is
// correct; and arbitrary variants such as `[&:has(:focus-visible)]:`, which
// TaskList.tsx carries on a container reacting to a descendant.
//
// Not seen: the element, since class lists are read and never the JSX tag, so a
// bare 'bg-statusFailBg text-statusFail' handed to a `<button>` slips past. That
// keeps LabelBadge out of it, whose job is to report a connection as up or down
// and which renders as a real `<button>` as soon as a call site hands it an
// onClick.
//
// Nor a class list split across two literals. Each quoted string is one list,
// and a template's static text is one list with its `${...}` holes taken out,
// so a button's base classes and a `${err ? 'text-statusFail' : ''}` beside
// them are read as two. The holes are where other complete class lists live, a
// variant table or a tone map, and welding them to the surrounding text is what
// would produce the LabelBadge false alarm. check-hover-ramp names the same
// limit.
//
// Nor index.css: nothing there paints a status token onto anything, those
// tokens appearing only in the @theme block that declares the Tailwind colour
// keys, so a handwritten `.glim-*` rule reaching for one would not be seen.
//
// It also counts the control class lists it found and fails if that number
// collapses, because a marker set that has stopped matching is a green check
// that is no longer checking.
//
// Run: `node web/check-no-status-on-control.mjs`.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const web = dirname(fileURLToPath(import.meta.url));
const src = join(web, 'src');

/**
 * The status colour utilities, read out of the theme rather than listed here.
 *
 * index.css's @theme block decides which `*-statusX` classes Tailwind emits, so
 * it is the only source for the list. A hand-kept copy here would go stale, and
 * go stale quietly, the next time GlimStone adds a tone.
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
 * Bare only. `group-disabled` and `peer-checked` stay out, being a statement
 * about a different element: a status glyph inside a disabled button is
 * entitled to both.
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
 * Every piece of text that is one class list, as the chunks it is written in.
 *
 * A plain quoted string is one chunk. A template is all of its static text
 * together, `${...}` holes removed, because those chunks are one element's
 * className however many interpolations are threaded through them. The holes
 * stay out: what sits inside them is another class list, a variant table value
 * or a tone map entry, and reading those as part of the surrounding text is the
 * false alarm the header describes.
 *
 * Each chunk carries its real offset in the file, stepped over the holes rather
 * than past their removal, so a reported line points at the class it names.
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
// Ten rather than a number near today's count, so this catches the markers
// collapsing, a Tailwind release renaming a variant or the tokenizer breaking,
// and not a few hand-rolled buttons moving into ui.tsx.
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
