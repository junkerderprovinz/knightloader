// GlimStone 1.14.0: in a row of buttons the one that goes ahead ends the row
// and the one that retreats opens it. The end is where the pointer and the
// thumb are, so Cancel there puts the destructive choice under the hand. There
// is no way for a row to opt out.
//
// A button is read by its visible child text, the `{t('...')}` between its
// tags, never by title= or aria-label=: glyph buttons such as QueueBar's
// transport column carry words only in title=, and a button without visible
// words is ignored. The one exception is a `labelled` Button, a window's way
// out among them: the label engine prints its title as its words, so the title
// is read like child text.
//
// A retreat is a key whose last segment contains one of the camel words
// cancel, back, skip, close, dismiss, revert, undo, abort or pause. Whole
// words, so `settings.look.background` is not a retreat, and no list of key
// names to keep up to date.
//
// A row is a list of slots. `{ok && <Button/>}` may or may not draw and still
// counts. The branches of `{a ? <>…</> : <>…</>}` never draw together, so each
// branch is walked from the state before the slot.
//
// Not checked: vertical or reversed rows, menus and tab strips, grid rows,
// lowercase <button>, a Button inside another component, wording that is not
// a t() key, and the runtime order of a .map().
//
// Run: `node web/check-button-order.mjs`.
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
 * blankComments replaces comments with spaces, keeping offsets and line
 * numbers, so tag names mentioned in comments are not scanned.
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
 * isTag reports whether the `<` at `i` starts an element rather than a
 * comparison or a type argument. The character before decides: comparisons
 * and type arguments follow an identifier, `)` or `]` (`n < 3`,
 * `Record<string, string>`); a tag follows a brace, an operator or a newline.
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
    if (text[j] !== '=') {
      // A bare attribute such as `labelled`, which holds no value to read.
      attrs.push({ name: attr[0], from: j, to: j });
      continue;
    }
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

/** The keys in a `labelled` Button's title, which the label engine prints. */
function engineLabels(text, open) {
  if (!open.attrs.some((a) => a.name === 'labelled')) return [];
  const title = open.attrs.find((a) => a.name === 'title');
  return title ? labels(text.slice(title.from, title.to)) : [];
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
 * branchesOf reads one position in a row as `{ branches }`, each branch a
 * slot list: two for a ternary, which never draw together, one for `&&`.
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
 * slots cuts a region into slots, left to right. A nested element is opaque;
 * a fragment is walked through, since `<>` draws nothing of its own.
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
      if (open.tag === 'Button') out.push({ at: i, keys: engineLabels(text, open) });
      i = open.openEnd;
      continue;
    }
    const close = closeTag(text, open.tag, open.openEnd);
    if (open.tag === 'Button') {
      out.push({ at: i, keys: [...labels(text.slice(open.openEnd, close)), ...engineLabels(text, open)] });
    }
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
    // Modal lays out its footer prop as a row itself (ui.tsx).
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
 * walk goes through one row carrying the last advancing button seen and
 * returns what the row ends on. Each branch starts from the state before its
 * slot, so a Cancel in one branch never counts as behind a Save in the other.
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
    // The last key names the button in messages: with
    // `{busy ? t('saving') : t('save')}` that is the resting word.
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
