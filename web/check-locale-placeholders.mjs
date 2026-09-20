// Placeholder parity across the 42 web catalogues, and the dash rule with it.
//
// A dropped placeholder fails quietly and one language deep: that catalogue
// renders "Removed download(s)." with no number in it for ever, and tsc is
// happy either way because the type is `string`. extension/check-locales.mjs
// guards the extension's catalogues the same way.
//
// Placeholders: every {name} in the English value appears in the translation,
// none dropped, none invented, none misspelt. Order is free, and several
// languages have to move them.
//
// Dashes: none standing between clauses, except in bg, ru, sr and uk, where it
// is ordinary punctuation and its absence reads as a mistake. Both lengths
// count, because the German Gedankenstrich is the shorter one. A dash between
// digits is left alone, so the test is whitespace on both sides rather than
// presence.
//
// Not checked: whether a value equals its English source. Plenty legitimately
// do, and what stays English by intent is listed in untranslated.json and
// checked by its own script.
//
// The scanner is line-based because the catalogues carry comment lines between
// entries: a lazy [\s\S]*? up to the next key runs past a comment and swallows
// the following value.
//
// Run: `node web/check-locale-placeholders.mjs`
import { readFileSync, readdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const dir = join(here, 'src/lib/locales');

/** Locales where a dash between clauses is normal punctuation. */
const DASH_OK = new Set(['bg', 'ru', 'sr', 'uk']);

/**
 * A dash standing between clauses: em or en, with whitespace on both sides.
 *
 * The whitespace separates it from the correct uses. A year range or a size
 * range is right in every language here, and a check that flagged those is one
 * people learn to skip.
 */
const clauseDash = /\s[–—]\s/;

/** key -> the raw source text of its value, values wrapped over lines included. */
function entries(file) {
  const out = new Map();
  let key = null;
  let buf = [];
  const close = () => {
    if (key !== null) out.set(key, buf.join(' '));
    key = null;
    buf = [];
  };
  for (const line of readFileSync(join(dir, file), 'utf8').split('\n')) {
    const start = line.match(/^ {2}'([^']+)':\s*(.*)$/);
    if (start) {
      close();
      key = start[1];
      buf = [start[2]];
      if (/,\s*$/.test(start[2])) close();
      continue;
    }
    if (key === null) continue;
    // A comment, a blank line or the closing brace ends the entry.
    if (/^\s*(\/\/|\/\*|\}|$)/.test(line)) {
      close();
      continue;
    }
    buf.push(line.trim());
    if (/,\s*$/.test(line)) close();
  }
  close();
  return out;
}

const placeholders = (v) => [...new Set(v.match(/\{[a-zA-Z]+\}/g) || [])].sort().join(',');

const en = entries('en.ts');
const files = readdirSync(dir).filter((f) => f.endsWith('.ts') && f !== 'en.ts' && f !== 'index.ts');

const problems = [];
for (const file of files) {
  const loc = file.replace('.ts', '');
  const d = entries(file);
  for (const [k, ev] of en) {
    const v = d.get(k);
    if (v === undefined) continue; // key parity is tsc's job, and check-untranslated names it
    const want = placeholders(ev);
    const got = placeholders(v);
    if (want !== got) {
      problems.push(`${loc} ${k}: placeholders want [${want || 'none'}] got [${got || 'none'}]`);
    }
    if (!DASH_OK.has(loc) && clauseDash.test(v)) problems.push(`${loc} ${k}: dash between clauses`);
  }
}
for (const [k, v] of en) if (clauseDash.test(v)) problems.push(`en ${k}: dash between clauses`);

if (problems.length) {
  console.log(`${problems.length} problem(s):`);
  for (const p of problems.slice(0, 40)) console.log('  ' + p);
  if (problems.length > 40) console.log(`  ... and ${problems.length - 40} more`);
  process.exit(1);
}
console.log(`ok: ${files.length + 1} catalogues, placeholders and dashes hold across ${en.size} keys`);
