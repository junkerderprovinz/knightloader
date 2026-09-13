// Placeholder parity across the 42 web catalogues, and the dash rule with it.
//
// WHY THIS EXISTS. extension/check-locales.mjs has guarded the extension's 120
// keys this way since it was written, and the web UI's 2500 had nothing at all.
// The failure is quiet and one-language-deep: a translator drops {n} and that
// language renders "Removed download(s)." with no number in it, for ever, while
// tsc is perfectly happy because the type is `string`. Nobody who reads that
// language reports it as a missing placeholder, they report it as a weird
// sentence, if they report it at all.
//
// WHAT IT CHECKS
//
//   placeholders  every {name} in the English value appears in the translation,
//                 none dropped, none invented, none misspelt. Order is free: a
//                 language may move them, and several must.
//   dashes        no dash standing between clauses, except in bg, ru, sr and uk,
//                 where one is ordinary punctuation and its absence reads as a
//                 mistake. jdp's standing rule, and it covers UI strings rather
//                 than only prose.
//
//                 BOTH LENGTHS, and the short one is the whole reason this note
//                 exists. The rule is usually written "no em dashes", so the
//                 first version of this check looked for U+2014 only - and in
//                 German the Gedankenstrich IS the en dash, U+2013. The source
//                 catalogues carried "reach it – on the same network", which is
//                 exactly the construction the rule is about, and 36 translators
//                 dutifully copied it past a green check.
//
//                 A dash BETWEEN DIGITS is left alone: 2020–2024 and 10–20 MB
//                 are correct typography in every language here, and flagging
//                 them would teach people to ignore this check. The test is
//                 therefore "surrounded by whitespace", not "present".
//
// WHAT IT DELIBERATELY DOES NOT CHECK
//
//   Whether a value equals the English one. Plenty legitimately do: DRM is DRM,
//   Matrix is Matrix, a Dutch "Import" is an English one. What is untranslated
//   ON PURPOSE lives in untranslated.json and is checked by its own script.
//
// THE SCANNER IS LINE-BASED, AND THAT IS LOAD-BEARING. A regex that captured
// [\s\S]*? up to the next entry looks right and is not: these files carry
// comment lines BETWEEN entries, so a lazy match runs past the comment and
// swallows the FOLLOWING key's value. Written that way, this check accused all
// 41 catalogues of dropping {n} and {reason} from a key whose English has
// neither, de.ts included - and de.ts is written by hand. A check that accuses
// every file at once is accusing itself.
//
// Run by CI and by hand: `node web/check-locale-placeholders.mjs`
import { readFileSync, readdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const dir = join(here, 'src/lib/locales');

/** Locales where a dash between clauses is normal punctuation. */
const DASH_OK = new Set(['bg', 'ru', 'sr', 'uk']);

/**
 * A dash STANDING BETWEEN CLAUSES: em or en, with whitespace on both sides.
 *
 * Whitespace on both sides is what separates the construction from the correct
 * uses. 2020–2024 and 10–20 MB are right in every language here, and a check
 * that flagged them would be one people learn to skip.
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
