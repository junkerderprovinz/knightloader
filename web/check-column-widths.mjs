// The two numbers a list column carries, checked against what its cell was
// measured to need.
//
// A column width is the one kind of number here that nobody can verify by
// reading it. It looks like taste and is argued back and forth in comments, so
// each round of "too wide" or "too narrow" moves it by whatever the last person
// guessed while the comment above it keeps the reasoning of the round before.
// The Variante column went from 132 to 236 in the commit that also stopped its
// cell shrinking and let it wrap, and the comment left behind claimed 236 was
// needed because at 132 the pickers were shaved to a single letter each. At 132
// nothing is shaved and nothing is clipped; the second picker moves to a second
// line.
//
// Every figure below was read off the live collector (seeded yt-dlp variant
// rows, Chromium at 1600x950) in all 42 shipped locales, measuring the cell's
// content on one line plus the cell's own 16px of padding:
//
//   video row       max 287.1  (lt: "Vaizdo įrašas" and two pickers reading
//                               "Automatinis"); 228.3 in English and German,
//                               with "webm (vp9)" and "2160p60"
//   audio row       max 229.7  (pl: label, format picker and bitrate picker,
//                               with "vorbis" and the widest bitrate label)
//   the other kinds max  86.8  (he)
//   widest single   max 129.1  (fi: the "Automaattinen" picker and padding).
//   control                     A picker cannot shrink (shrink-0), so below
//                               this the cell's own overflow clips it.
//
// The rules that follow from them, and nothing beyond them:
//
//   1. variant.minWidth >= 130. The floor is the widest single control rather
//      than the whole row: wrapping saves a narrow column, clipping does not,
//      and a picker is clipped rather than shrunk.
//   2. In the collector, 229 <= variant.width <= 288. The lower bound is the
//      video row, which every yt-dlp package has exactly one of, on one line
//      in English and German. In a language with a longer word for Auto its
//      quality picker wraps under the format picker, rather than the column
//      growing by 60px in every language. The upper bound is the widest cell
//      this column can ever hold in any language; above it the column is
//      reserving room for content that does not exist. The download list
//      shows a line of text there that truncates into its tooltip, so it only
//      needs the minWidth and keeps the upper bound.
//   3. In each list, the name column's default is the widest default of that
//      list's visible columns. It carries the file name, which is what the row
//      is for, and it is the one column that cannot be read anywhere else.
//
// It reads the source rather than the running app: the measurement is the
// expensive half and stands above, while what rots is the number in columns.tsx
// drifting away from it.
//
// Run: `node web/check-column-widths.mjs`.

import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const file = join(here, 'src', 'components', 'columns.tsx');

/**
 * Comments blanked to spaces, indices kept, for two reasons: the comments in
 * columns.tsx quote the patterns searched for below, and they hold unmatched
 * brackets, which would break the brace counting that finds the column entries.
 * Strings are left alone so `'https://...'` is not read as a line comment.
 */
function blankComments(src) {
  let out = '';
  let i = 0;
  while (i < src.length) {
    const two = src.slice(i, i + 2);
    if (two === '//') {
      const end = src.indexOf('\n', i);
      const stop = end < 0 ? src.length : end;
      out += ' '.repeat(stop - i);
      i = stop;
    } else if (two === '/*') {
      const end = src.indexOf('*/', i + 2);
      const stop = end < 0 ? src.length : end + 2;
      out += src.slice(i, stop).replace(/[^\n]/g, ' ');
      i = stop;
    } else if (src[i] === "'" || src[i] === '"' || src[i] === '`') {
      const quote = src[i];
      let j = i + 1;
      while (j < src.length && src[j] !== quote) j += src[j] === '\\' ? 2 : 1;
      out += src.slice(i, Math.min(j + 1, src.length));
      i = j + 1;
    } else {
      out += src[i];
      i++;
    }
  }
  return out;
}

const text = blankComments(readFileSync(file, 'utf8'));

// Measured, on the live instance, in all 42 locales. See the header.
const WRAP_FLOOR = 130; // widest single control + the cell's padding (fi)
const COMMON_ROW = 229; // widest one-line video row in English and German
const WIDEST_ROW = 288; // widest one-line video row there can be (lt)

const problems = [];

/** The numeric consts columns.tsx computes its floors from. */
function constants(src) {
  const out = {};
  for (const [, name, value] of src.matchAll(/^(?:export )?const ([A-Z_][A-Z0-9_]*) = ([^;]+);/gm)) {
    const expr = value.trim();
    if (/^[\d\s+*/()-]+$/.test(expr)) {
      out[name] = Number(new Function(`return (${expr})`)());
      continue;
    }
    // An expression over consts already read - the file defines them in order.
    const resolved = expr.replace(/[A-Z_][A-Z0-9_]*/g, (id) => (id in out ? String(out[id]) : id));
    if (/^[\d\s+*/()-]+$/.test(resolved)) out[name] = Number(new Function(`return (${resolved})`)());
  }
  return out;
}

/** Evaluates `340`, `TREE_INDENT + NAME_TEXT_FLOOR` and nothing cleverer. */
function number(expr, consts, where) {
  const resolved = expr.trim().replace(/[A-Za-z_][A-Za-z0-9_]*/g, (id) =>
    id in consts ? String(consts[id]) : id,
  );
  if (!/^[\d\s+*/()-]+$/.test(resolved)) {
    problems.push(`${where}: cannot read the number out of \`${expr.trim()}\``);
    return null;
  }
  return Number(new Function(`return (${resolved})`)());
}

/** The top-level entries of an array literal, by brace depth. */
function entries(src, startMarker) {
  const at = src.indexOf(startMarker);
  if (at < 0) return null;
  // The marker ends on the array's own bracket. Searching for the next '['
  // instead finds the one in `ColumnDef[]` and closes again immediately.
  let i = at + startMarker.length - 1;
  let depth = 0;
  const out = [];
  let from = -1;
  for (; i < src.length; i++) {
    const c = src[i];
    if (c === '[' || c === '{') {
      depth++;
      if (depth === 2 && c === '{') from = i;
    } else if (c === ']' || c === '}') {
      depth--;
      if (depth === 1 && c === '}' && from >= 0) {
        out.push(src.slice(from, i + 1));
        from = -1;
      }
      if (depth === 0) break;
    }
  }
  return out;
}

const consts = constants(text);
const blocks = entries(text, 'export const COLUMNS: ColumnDef[] = [');
if (!blocks || blocks.length < 10) {
  console.error(`check-column-widths: found ${blocks ? blocks.length : 0} column definitions - wrong file?`);
  process.exit(1);
}

const columns = new Map();
for (const block of blocks) {
  const id = block.match(/\bid:\s*'([^']+)'/)?.[1];
  if (!id) continue;
  const width = block.match(/\bwidth:\s*([^,\n]+)/)?.[1];
  const minWidth = block.match(/\bminWidth:\s*([^,\n]+)/)?.[1];
  const perList = {};
  const widthIn = block.match(/\bwidthByProfile:\s*\{([^}]*)\}/)?.[1];
  if (widthIn) {
    for (const [, list, value] of widthIn.matchAll(/(\w+):\s*(\d+)/g)) perList[list] = Number(value);
  }
  const onlyIn = block.match(/\bonlyIn:\s*\[([^\]]*)\]/)?.[1];
  columns.set(id, {
    id,
    width: width ? number(width, consts, `${id}.width`) : null,
    minWidth: minWidth ? number(minWidth, consts, `${id}.minWidth`) : null,
    perList,
    onlyIn: onlyIn ? [...onlyIn.matchAll(/'([^']+)'/g)].map((m) => m[1]) : null,
  });
}

/** What this list draws before anybody has touched the layout. */
function visibleIn(list) {
  const chunk = text.slice(text.indexOf('export const DEFAULT_HIDDEN'));
  const arr = chunk.match(new RegExp(`\\b${list}:\\s*\\[([^\\]]*)\\]`))?.[1] ?? '';
  const hidden = new Set([...arr.matchAll(/'([^']+)'/g)].map((m) => m[1]));
  return [...columns.values()].filter(
    (c) => !hidden.has(c.id) && (!c.onlyIn || c.onlyIn.includes(list)),
  );
}

const defaultWidth = (c, list) => c.perList[list] ?? c.width;

// The variant column against its own measured cell.

const variant = columns.get('variant');
if (!variant) {
  problems.push("there is no 'variant' column any more - re-measure before deleting these rules");
} else {
  if (variant.minWidth < WRAP_FLOOR) {
    problems.push(
      `variant.minWidth is ${variant.minWidth}, below the measured ${WRAP_FLOOR}px a single picker needs. ` +
        'A picker cannot shrink, so under this floor the cell clips it instead of wrapping.',
    );
  }
  for (const list of ['downloads', 'collector']) {
    const w = defaultWidth(variant, list);
    if (list === 'collector' && w < COMMON_ROW) {
      problems.push(
        `variant.width in ${list} is ${w}, below the measured ${COMMON_ROW}px the video row needs on one line. ` +
          'Every yt-dlp package has a video row; sizing under it wraps the common case to save the rare one.',
      );
    }
    if (w < variant.minWidth) {
      problems.push(`variant.width in ${list} is ${w}, below its own minWidth of ${variant.minWidth}.`);
    }
    if (w > WIDEST_ROW) {
      problems.push(
        `variant.width in ${list} is ${w}, above the measured ${WIDEST_ROW}px of the widest cell this column ` +
          'can hold in any of the 42 locales. The surplus is room reserved for content that does not exist.',
      );
    }
  }
}

// The name column is the widest column in each list.

for (const list of ['downloads', 'collector']) {
  const visible = visibleIn(list);
  const name = visible.find((c) => c.id === 'name');
  if (!name) {
    problems.push(`the ${list} list does not draw a name column`);
    continue;
  }
  const mine = defaultWidth(name, list);
  for (const other of visible) {
    if (other.id === 'name') continue;
    const theirs = defaultWidth(other, list);
    if (theirs > mine) {
      problems.push(
        `in ${list} the '${other.id}' column defaults to ${theirs}px and 'name' to ${mine}px. ` +
          'The file name is what the row is for and nothing else on the row carries it.',
      );
    }
  }
}

if (problems.length) {
  console.error(`check-column-widths: ${problems.length} column width(s) out of step with the measurement.`);
  for (const p of problems) console.error(`  ${p}`);
  console.error('The measurements are in the header of this file; re-measure before moving a bound.');
  process.exit(1);
}

const shown = ['downloads', 'collector']
  .map((l) => `${l}: ${visibleIn(l).length} columns, name ${defaultWidth(columns.get('name'), l)}px`)
  .join('; ');
console.log(
  `ok: ${columns.size} column definitions; variant ${defaultWidth(variant, 'collector')}px within the measured ` +
    `${COMMON_ROW}-${WIDEST_ROW} band, floor ${variant.minWidth} >= ${WRAP_FLOOR}; ${shown}`,
);
