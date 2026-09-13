// GlimStone 1.14.0: the button that goes ahead ends the row, the one that
// retreats opens it.
//
// WHAT GOES WRONG WITHOUT IT. A horizontal row of answers is read from its end,
// because the end is where the hand already is: the pointer arrives there, the
// thumb reaches there, and on a narrow window it is the half of the row anybody
// actually looks at. Put Cancel there and the button under the hand is the one
// that throws the work away. This app shipped that shape in several places at
// once. CaptchaModal had the countdown sitting to the RIGHT of the button that
// answers the challenge, so the control somebody came for was not at the end of
// its row. CookieJarDialog had no spacer at all, so both buttons huddled at the
// start with the server's refusal beside them. Of the twenty-one footers ui.tsx
// counted when it took the alignment away from the callers, eleven pushed
// themselves to the end by hand and eight did not, and one file had it both ways
// in two windows of the same kind.
//
// WHY A SCRIPT AND NOT THE NOTE. The note exists twice over. GlimStone states the
// order, and ui.tsx's Modal states it again in a comment sitting directly above
// the div that draws every footer in the app. Both were in place while the order
// was wrong, and the fix was a hand sweep. What the sweep left behind is the
// tell: half a dozen files now carry a paragraph above their button pair
// explaining which end the advancing one belongs on, which is what a call site
// writes AFTER getting it wrong and arguing it out. A rule that has to be
// re-argued at every call site is a rule nothing is holding. There is no
// exemption switch in here and there is not going to be one. 1.13.0 deleted the
// last approved carve-out in the language on the grounds that from the outside a
// documented exemption and a control that simply ignores the rule look
// identical, and a file that let a row opt out would hand that straight back.
//
// HOW IT READS A BUTTON. Only the VISIBLE child text, the `{t('...')}` standing
// between the Button's own tags. Never title= and never aria-label=, and that is
// not caution, it is the difference between this check and a broken one:
// QueueBar's Play, Pause and Stop carry their wording in title= alone because
// they are glyphs, they stand in a deliberate COLUMN, and a check that read
// attributes would report the app's most carefully argued control as its worst
// offender. A button with no visible words is invisible here, neither advancing
// nor retreating, so it can never push a neighbour into a finding either.
//
// HOW IT KNOWS A RETREAT. By the last dot segment of the key, cut into its camel
// words and matched against cancel, back, skip, close, dismiss, revert, undo,
// abort and pause. Whole words rather than substrings, because
// `settings.look.background` ends in a segment that contains "back" and is a
// heading, not a way out. Cutting the camel words is what catches `importClose`
// and `queue.hardStopConfirmCancel` without anybody keeping a list of key names,
// and a list is the thing to avoid: it goes stale the first time somebody names a
// button something sensible that nobody thought to add. `undo` is in the
// vocabulary beside `revert` for that same reason and not to spare any one row.
// It is the plainer word for the same verb, it is the one this app actually uses,
// and QuickAdd's finished-state row reads Undo then Close, which is the retracting
// button at the start and the way onward at the end, exactly as asked.
//
// WHAT A ROW IS MADE OF. Slots, not buttons. `{ok && <Button/>}` is a slot that
// may or may not draw, which still counts, because the combination where it does
// draw is a real screen. `{picked ? <>…</> : <>…</>}` is one slot with two
// branches that can NEVER draw together, and reading those as one row is how the
// first version of this file accused Accounts.tsx and HosterLoginSection.tsx of
// putting Cancel behind Save: both windows have a full footer for the picked
// state and a lone Cancel for the unpicked one, and flattening the two into a
// single list produced a Cancel behind a Save that nothing can ever render. So a
// branch is walked with what stood before the slot and never with what its
// sibling branch left behind.
//
// WHAT IT DOES NOT SEE. Vertical rows, because flex-col has no end the hand
// favours, and the same goes for anything reversed, which is left alone rather
// than guessed at. Menu entries and tab strips, which are lists rather than
// answers. Rows laid out with grid instead of flex. Plain lowercase `<button>`,
// which in this tree is chips, tabs and menu rows, never an answer pair. A Button
// wrapped in some other component inside the row, which counts as being inside
// that component rather than in the row. Anything whose wording is not resolved
// from a t() key, and the runtime order of a `.map()`, which is one Button in the
// source whatever it draws.
//
// Run by hand or from CI: `node web/check-button-order.mjs`.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const src = join(dirname(fileURLToPath(import.meta.url)), 'src');

/** A camel word that means this button walks the action back. */
const RETREAT = /^(cancel|back|skip|close|dismiss|revert|undo|abort|pause)$/i;

/** Class tokens that make an element a row whose end is its right-hand side. */
const ROW_CLASS = /^(?:[\w-]+:)*(?:inline-)?flex$/;
/** Class tokens that take the question away: a column, or a reversed order. */
const NOT_A_ROW = /^(?:[\w-]+:)*flex-(?:col|col-reverse|row-reverse|wrap-reverse)$/;

function sources(dir) {
  const found = [];
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) found.push(...sources(path));
    else if (/\.tsx$/.test(entry)) found.push(path);
  }
  return found;
}

/** Step over a quoted string or template, `i` standing on its opening mark. */
function pastString(text, i) {
  const quote = text[i++];
  while (i < text.length && text[i] !== quote) i += text[i] === '\\' ? 2 : 1;
  return i + 1;
}

/**
 * Comments blanked, everything else left where it stood so a line number still
 * counts. Blanking rather than deleting is the whole of it: the offsets have to
 * survive, and the prose in this repo's comments is thick with `<select>` and
 * `<Button>` written out by name, which a tag scanner would otherwise walk into.
 */
function blankComments(text) {
  const out = [...text];
  let i = 0;
  while (i < text.length) {
    const c = text[i];
    if (c === "'" || c === '"' || c === '`') {
      i = pastString(text, i);
      continue;
    }
    if (c === '/' && text[i + 1] === '/') {
      while (i < text.length && text[i] !== '\n') out[i++] = ' ';
      continue;
    }
    if (c === '/' && text[i + 1] === '*') {
      const end = text.indexOf('*/', i + 2);
      const stop = end === -1 ? text.length : end + 2;
      while (i < stop) {
        if (text[i] !== '\n') out[i] = ' ';
        i++;
      }
      continue;
    }
    i++;
  }
  return out.join('');
}

/** The `}` closing the `{` at `i`, with strings stepped over. */
function closeBrace(text, i) {
  let depth = 0;
  while (i < text.length) {
    const c = text[i];
    if (c === "'" || c === '"' || c === '`') {
      i = pastString(text, i);
      continue;
    }
    if (c === '{') depth++;
    else if (c === '}' && --depth === 0) return i;
    i++;
  }
  return text.length;
}

/**
 * Is the `<` at `i` the start of an element, or is it a comparison or a type
 * argument?
 *
 * The character in front decides, and it decides cleanly. A tag is always
 * preceded by something that opened a slot for it: a brace, a bracket, an arrow,
 * an operator, a newline. A comparison and a type argument are both preceded by
 * the thing being compared or parameterised, so `n < 3`, `useState<Settings>` and
 * `Record<string, string>` all end on an identifier character, a `)` or a `]`.
 * Without this test `Record<string, string>` parses as an element named `string`
 * whose closing tag never arrives, and the rest of the file goes with it.
 */
function isTag(text, i) {
  if (!/[A-Za-z>/]/.test(text[i + 1] || '')) return false;
  let j = i - 1;
  while (j >= 0 && /\s/.test(text[j])) j--;
  return j < 0 || !/[\w$)\]]/.test(text[j]);
}

const NAME = /[A-Za-z][\w.]*/y;
const ATTR = /[A-Za-z][\w:.-]*/y;

/** The open tag starting at `i`, or null if it never closes. A fragment is ''. */
function openTag(text, i) {
  let j = i + 1;
  NAME.lastIndex = j;
  const named = NAME.exec(text);
  const tag = named ? named[0] : '';
  if (named) j = NAME.lastIndex;
  const attrs = [];
  while (j < text.length) {
    const c = text[j];
    if (c === '>') return { tag, attrs, openEnd: j + 1, selfClosing: false };
    if (c === '/' && text[j + 1] === '>') return { tag, attrs, openEnd: j + 2, selfClosing: true };
    if (/\s/.test(c)) {
      j++;
      continue;
    }
    if (c === '{') {
      j = closeBrace(text, j) + 1;
      continue;
    }
    ATTR.lastIndex = j;
    const attr = ATTR.exec(text);
    if (!attr) {
      j++;
      continue;
    }
    j = ATTR.lastIndex;
    if (text[j] !== '=') continue;
    j++;
    if (text[j] === '{') {
      const end = closeBrace(text, j);
      attrs.push({ name: attr[0], from: j + 1, to: end });
      j = end + 1;
    } else if (text[j] === '"' || text[j] === "'") {
      const end = text.indexOf(text[j], j + 1);
      const stop = end === -1 ? text.length : end;
      attrs.push({ name: attr[0], from: j + 1, to: stop });
      j = stop + 1;
    }
  }
  return null;
}

/** Where the `</tag>` matching the element whose children start at `from` begins. */
function closeTag(text, tag, from) {
  let i = from;
  let depth = 1;
  while (i < text.length) {
    const lt = text.indexOf('<', i);
    if (lt === -1) return text.length;
    if (text[lt + 1] === '/') {
      const end = text.indexOf('>', lt);
      if (end === -1) return text.length;
      if (text.slice(lt + 2, end).trim() === tag && --depth === 0) return lt;
      i = end + 1;
      continue;
    }
    if (!isTag(text, lt)) {
      i = lt + 1;
      continue;
    }
    const open = openTag(text, lt);
    if (!open) return text.length;
    if (open.tag === tag && !open.selfClosing) depth++;
    i = open.openEnd;
  }
  return text.length;
}

/** The span an element occupies, closing tag included. */
function element(text, at, open) {
  if (open.selfClosing) return [at, open.openEnd];
  const close = closeTag(text, open.tag, open.openEnd);
  const end = text.indexOf('>', close);
  return [at, end === -1 ? text.length : end + 1];
}

/** Every class token an attribute value names, whether literal or interpolated. */
function classes(text, attr) {
  const raw = text.slice(attr.from, attr.to);
  const words = [];
  for (const piece of raw.match(/'[^'\n]*'|"[^"\n]*"|`[^`]*`/g) || []) words.push(...piece.slice(1, -1).split(/\s+/));
  if (!/['"`]/.test(raw)) words.push(...raw.split(/\s+/));
  return words.filter(Boolean);
}

/** Does this element lay its children out left to right? */
function isRow(text, open) {
  const attr = open.attrs.find((a) => a.name === 'className');
  if (!attr) return false;
  const tokens = classes(text, attr);
  return tokens.some((c) => ROW_CLASS.test(c)) && !tokens.some((c) => NOT_A_ROW.test(c));
}

/** The keys a Button shows as its own child text, in source order. */
function labels(children) {
  return [...children.matchAll(/\bt\(\s*'([^'\n]+)'/g)].map((m) => m[1]);
}

/** Does this key name a button that walks the action back? */
function retreats(key) {
  return key
    .split('.')
    .pop()
    .replace(/([a-z0-9])([A-Z])/g, '$1 $2')
    .split(/[^A-Za-z0-9]+/)
    .some((word) => RETREAT.test(word));
}

/**
 * One position in a row and everything that can stand there: `{ branches }`,
 * each branch a slot list of its own. A ternary has two branches and they never
 * draw together; a `&&` has one, which may draw or not, and either way the
 * buttons around it keep their order.
 */
function branchesOf(text, from, to) {
  const branches = [];
  let i = from;
  while (i < to) {
    const lt = text.indexOf('<', i);
    if (lt === -1 || lt >= to) break;
    if (text[lt + 1] === '/' || !isTag(text, lt)) {
      i = lt + 1;
      continue;
    }
    const open = openTag(text, lt);
    if (!open) break;
    const [, end] = element(text, lt, open);
    branches.push(slots(text, lt, Math.min(end, to)));
    i = end;
  }
  return { branches };
}

/**
 * A region cut into slots, left to right. A nested element is opaque, because
 * its children are its own business and not this row's; a fragment is walked
 * through rather than over, since `<>` draws nothing and a footer's `<>…</>` is
 * the footer's row rather than a row inside it.
 */
function slots(text, from, to, out = []) {
  let i = from;
  while (i < to) {
    const c = text[i];
    if (c === '{') {
      const end = Math.min(closeBrace(text, i), to);
      const slot = branchesOf(text, i + 1, end);
      if (slot.branches.length) out.push(slot);
      i = end + 1;
      continue;
    }
    if (c !== '<' || text[i + 1] === '/' || !isTag(text, i)) {
      i++;
      continue;
    }
    const open = openTag(text, i);
    if (!open) break;
    if (open.selfClosing) {
      i = open.openEnd;
      continue;
    }
    const close = closeTag(text, open.tag, open.openEnd);
    if (open.tag === 'Button') out.push({ at: i, keys: labels(text.slice(open.openEnd, close)) });
    else if (open.tag === '') slots(text, open.openEnd, Math.min(close, to), out);
    i = Math.min(element(text, i, open)[1], to);
  }
  return out;
}

/** Every left-to-right button row in one file, each as its own slot list. */
function rows(text) {
  const found = [];
  let i = 0;
  while (i < text.length) {
    const lt = text.indexOf('<', i);
    if (lt === -1) break;
    if (text[lt + 1] === '/' || !isTag(text, lt)) {
      i = lt + 1;
      continue;
    }
    const open = openTag(text, lt);
    if (!open) break;
    // Modal draws its footer in `flex items-center justify-end` itself (ui.tsx),
    // so that row is the prop rather than anything written at the call site.
    // Every other row says what it is in its own className.
    if (open.tag === 'Modal') {
      const footer = open.attrs.find((a) => a.name === 'footer');
      if (footer) found.push([branchesOf(text, footer.from, footer.to)]);
    }
    if (!open.selfClosing && isRow(text, open)) found.push(slots(text, open.openEnd, closeTag(text, open.tag, open.openEnd)));
    i = open.openEnd;
  }
  return found;
}

/**
 * Walks one row carrying the last advancing button seen, and hands back what the
 * row ends on. Branches are given the state from BEFORE their slot and their
 * answers are collected afterwards, so a Cancel in one branch is never read as
 * standing behind a Save in the other.
 */
function walk(list, ahead, seen, problems, where) {
  for (const slot of list) {
    if (slot.branches) {
      let after = ahead;
      for (const branch of slot.branches) {
        const end = walk(branch, ahead, seen, problems, where);
        if (end !== ahead) after = end;
      }
      ahead = after;
      continue;
    }
    if (slot.keys.length === 0) continue;
    seen.count++;
    const back = slot.keys.find(retreats);
    if (back) {
      if (ahead) problems.push(`${where(slot.at)} ${back} after ${ahead}`);
      continue;
    }
    // The LAST key, because the shape that carries several is
    // `{busy ? t('saving') : t('save')}` and the resting word is the one the
    // button is called by. This only picks the wording of the message; whether
    // there is a message at all was settled above, by all of the keys at once.
    ahead = slot.keys.at(-1);
  }
  return ahead;
}

const files = sources(src);
if (files.length < 20) {
  console.error(`check-button-order: only ${files.length} component files found - wrong directory?`);
  process.exit(1);
}

const problems = [];
const seen = { count: 0 };
let rowCount = 0;

for (const path of files) {
  const text = blankComments(readFileSync(path, 'utf8'));
  const where = (at) => `${path.slice(src.length + 1)}:${text.slice(0, at).split('\n').length}`;
  for (const row of rows(text)) {
    const before = seen.count;
    walk(row, null, seen, problems, where);
    if (seen.count > before) rowCount++;
  }
}
problems.sort();

if (problems.length) {
  console.error(`check-button-order: ${problems.length} button(s) walking the action back from the end of a row.`);
  console.error('Each line names the retreating button and the advancing one it was put behind:');
  for (const p of problems) console.error(`  ${p}`);
  console.error('Move the retreating one in front; GlimStone 1.14.0 wants the advancing button at the end.');
  process.exit(1);
}

console.log(
  `ok: ${files.length} components, ${rowCount} left-to-right rows carrying buttons, none of the ${seen.count} labelled buttons retreats behind an advancing one`,
);
