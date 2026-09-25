// The numbers a list column carries, checked against what its cell and its
// card were measured to hold.
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
// The name column has no width of its own: it takes what the other columns
// leave (gridTemplate in columns.tsx). In a narrow window it gives way to its
// floor first, then every other column to its minWidth, and only then does the
// table scroll. So the defaults decide how wide the name is, and the minimums
// decide the narrowest card the table fits.
//
// The card and its furniture, read off the live download list and collector
// (Chromium, the page tall enough for <main> to show its 10px scrollbar):
//
//   card            934 at 1280px, 1094 at 1440px, 1574 at 1920px
//   furniture       128: the row's 12px padding on each side and the 104px
//                   track for the row's three action badges
//
// The widest content of the other default columns, in the app's font with the
// cell's 16px of padding. These are what the widths in columns.tsx round up:
//
//   size            68  "1023 MiB", the longest a size gets
//   speed           78  "1023 MiB/s"
//   time left       82  "123h 45min"; 75 for "19h 58min", the widest value
//                       under a hundred hours
//   status         133  the widest transfer state in the 42 locales (fr)
//   host           116  "rapidgator.net" behind its logo; longer hosts
//                       truncate into their bubble
//   progress        60  the percentage, its gap and the padding; the bar
//                       takes the rest, 68px at the default of 128
//
// The variant column, read off the live collector (seeded yt-dlp variant rows,
// Chromium at 1600x950) in all 42 shipped locales, measuring the cell's content
// on one line plus the cell's own 16px of padding:
//
//   video row       max 287.1  (lt: "Vaizdo įrašas" and two pickers reading
//                               "Automatinis"); 230.7 in English and German,
//                               with "MP4 (H.265)" and "2160p60"
//   audio row       max 261.3  (fa: label, "AAC (M4A)" and the bitrate picker)
//   the other kinds max  86.8  (he)
//   widest single   max 143.7  (fa: the bitrate picker at 320 kbit/s and
//   control                     padding). A picker cannot shrink (shrink-0),
//                               so below this the cell's own overflow clips it.
//
// The rules that follow from them, and nothing beyond them:
//
//   1. In the collector, the variant column's minWidth >= 144. The floor is the
//      widest single control rather than the whole row: wrapping saves a
//      narrow column, clipping does not, and a picker is clipped rather than
//      shrunk. The download list shows a line of text there that truncates
//      into its tooltip, so its own floor can be lower.
//   2. In the collector, 231 <= variant.width <= 288. The lower bound is the
//      video row, which every yt-dlp package has exactly one of, on one line
//      in English and German. In a language with a longer word for Auto its
//      quality picker wraps under the format picker, rather than the column
//      growing by 60px in every language. The upper bound is the widest cell
//      this column can ever hold in any language; above it the column is
//      reserving room for content that does not exist. The download list
//      keeps the upper bound.
//   3. In each list, the default columns fit the card at 1280px with the name
//      at its floor and every other column at its minWidth. Past that the
//      table scrolls before anybody has added a column or dragged one wider.
//   4. In each list, at 1440px with every other column at its default width,
//      what is left for the name is at least the widest of those defaults. It
//      carries the file name, which is what the row is for, and it is the one
//      column that cannot be read anywhere else.
//   5. The time-left column's minWidth >= 75, so a column dragged to its floor
//      still shows hours and minutes below a hundred hours instead of cutting
//      the minutes off.
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

// Measured on the live instance. See the header.
const WRAP_FLOOR = 144; // widest single control + the cell's padding (fa)
const COMMON_ROW = 231; // widest one-line video row in English and German
const WIDEST_ROW = 288; // widest one-line video row there can be (lt)
const ETA_FLOOR = 75; // "19h 58min" + the cell's padding
const CARD_1280 = 934;
const CARD_1440 = 1094;
const FURNITURE = 128; // row padding + the actions track

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
  /** `{ downloads: 104 }` out of a `widthByProfile` or `minWidthByProfile`. */
  const byList = (key) => {
    const inner = block.match(new RegExp(`\\b${key}:\\s*\\{([^}]*)\\}`))?.[1] ?? '';
    return Object.fromEntries([...inner.matchAll(/(\w+):\s*(\d+)/g)].map(([, list, value]) => [list, Number(value)]));
  };
  const onlyIn = block.match(/\bonlyIn:\s*\[([^\]]*)\]/)?.[1];
  columns.set(id, {
    id,
    width: width ? number(width, consts, `${id}.width`) : null,
    minWidth: minWidth ? number(minWidth, consts, `${id}.minWidth`) : null,
    perList: byList('widthByProfile'),
    minPerList: byList('minWidthByProfile'),
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
const minWidth = (c, list) => c.minPerList[list] ?? c.minWidth;

// The variant column against its own measured cell.

const variant = columns.get('variant');
if (!variant) {
  problems.push("there is no 'variant' column any more - re-measure before deleting these rules");
} else {
  if (minWidth(variant, 'collector') < WRAP_FLOOR) {
    problems.push(
      `variant's minWidth in the collector is ${minWidth(variant, 'collector')}, below the measured ${WRAP_FLOOR}px ` +
        'a single picker needs. A picker cannot shrink, so under this floor the cell clips it instead of wrapping.',
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
    if (w < minWidth(variant, list)) {
      problems.push(`variant.width in ${list} is ${w}, below its own minWidth of ${minWidth(variant, list)}.`);
    }
    if (w > WIDEST_ROW) {
      problems.push(
        `variant.width in ${list} is ${w}, above the measured ${WIDEST_ROW}px of the widest cell this column ` +
          'can hold in any of the 42 locales. The surplus is room reserved for content that does not exist.',
      );
    }
  }
}

// The time-left column against its own measured cell.

const eta = columns.get('eta');
if (!eta) {
  problems.push("there is no 'eta' column any more - re-measure before deleting this rule");
} else if (minWidth(eta, 'downloads') < ETA_FLOOR) {
  problems.push(
    `eta's minWidth is ${minWidth(eta, 'downloads')}, below the measured ${ETA_FLOOR}px of "19h 58min". ` +
      'At its floor the column would cut the minutes off.',
  );
}

// The default columns against the card: they fit it at 1280px, and at 1440px
// the name is still the widest column.

const room = {};
for (const list of ['downloads', 'collector']) {
  const visible = visibleIn(list);
  const name = visible.find((c) => c.id === 'name');
  if (!name) {
    problems.push(`the ${list} list does not draw a name column`);
    continue;
  }
  const others = visible.filter((c) => c.id !== 'name');
  const narrowest = FURNITURE + minWidth(name, list) + others.reduce((n, c) => n + minWidth(c, list), 0);
  if (narrowest > CARD_1280) {
    problems.push(
      `in ${list} the default columns need ${narrowest}px with every one at its minWidth, more than the ` +
        `${CARD_1280}px card at 1280px. The table would scroll before anybody added or widened a column.`,
    );
  }
  room[list] = CARD_1440 - FURNITURE - others.reduce((n, c) => n + defaultWidth(c, list), 0);
  const widest = others.reduce((a, c) => (defaultWidth(c, list) > defaultWidth(a, list) ? c : a));
  if (room[list] < defaultWidth(widest, list)) {
    problems.push(
      `in ${list} the name gets ${room[list]}px at 1440px and the '${widest.id}' column defaults to ` +
        `${defaultWidth(widest, list)}px. The file name is what the row is for and nothing else on the row carries it.`,
    );
  }
}

if (problems.length) {
  console.error(`check-column-widths: ${problems.length} column width(s) out of step with the measurement.`);
  for (const p of problems) console.error(`  ${p}`);
  console.error('The measurements are in the header of this file; re-measure before moving a bound.');
  process.exit(1);
}

const shown = ['downloads', 'collector']
  .map((l) => `${l}: ${visibleIn(l).length} columns, name ${room[l]}px at 1440px`)
  .join('; ');
console.log(
  `ok: ${columns.size} column definitions; variant ${defaultWidth(variant, 'collector')}px within the measured ` +
    `${COMMON_ROW}-${WIDEST_ROW} band, floor ${minWidth(variant, 'collector')} >= ${WRAP_FLOOR}; ${shown}`,
);
