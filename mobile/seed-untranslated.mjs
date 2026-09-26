// Fills the app catalogues' missing keys with the English text and lists every
// value still in English as owed, so the translation pass after it finds them.
// The ledger has the format web/seed-untranslated.mjs writes, so one pass can
// fill both; that file explains why the debt is written down instead of left
// to the English fallback.
//
// One thing differs from the web seed: this one also lists a value that is
// already in a catalogue and still equals the English one. App keys are often
// added by hand, in English in every catalogue at once, and check-untranslated
// fails on any English value the ledger does not name. Running this after such
// a change is what names them.
//
// de.ts is never seeded and never listed. It is written by hand, and English
// put there would reach German users, so its missing keys are named and the run
// fails until somebody writes them.
//
// Run: node mobile/seed-untranslated.mjs          (writes)
//      node mobile/seed-untranslated.mjs --dry    (says what it would write)
import { readFileSync, writeFileSync, readdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const dir = join(here, 'src/i18n');
const LEDGER = join(here, 'untranslated.json');
const dry = process.argv.includes('--dry');

/** key -> the raw source text of its value, wrapped values included. */
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
      buf = start[2] ? [start[2]] : [];
      if (/,\s*$/.test(start[2])) close();
      continue;
    }
    if (key === null) continue;
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

const en = entries('en.ts');
// The folder also holds the provider and its helpers; a catalogue is the file
// named after its language code. Sorted, so the ledger comes out in the same
// order on every file system.
const files = readdirSync(dir)
  .filter((f) => /^[a-z]{2}\.ts$/.test(f) && f !== 'en.ts' && f !== 'de.ts')
  .sort();

// The previous file is merged, never rebuilt: "identical" and the notes are
// decisions made by hand that no run could recreate.
const before = JSON.parse(readFileSync(LEDGER, 'utf8'));
const previous = before.locales ?? {};
const settled = before.identical ?? {};

const ledger = {};
let seeded = 0;
let touched = 0;
for (const file of files) {
  const loc = file.slice(0, -3);
  const have = entries(file);
  const decided = new Set(settled[loc] ?? []);

  // What was owed stays owed until prune-untranslated.mjs sees it translated,
  // and a key somebody ruled identical is never owed as well.
  const owed = (previous[loc] ?? []).filter((k) => en.has(k) && !decided.has(k));
  for (const [k, v] of en) {
    if (decided.has(k) || owed.includes(k)) continue;
    if (!have.has(k) || have.get(k) === v) owed.push(k);
  }
  if (owed.length) ledger[loc] = owed;

  const missing = [...en.keys()].filter((k) => !have.has(k));
  if (missing.length === 0) continue;
  touched++;
  seeded += missing.length;
  if (dry) continue;

  const p = join(dir, file);
  let s = readFileSync(p, 'utf8');
  const at = s.lastIndexOf('\n};');
  if (at < 0) {
    console.error(`${p}: no closing brace found, refusing to guess`);
    process.exit(1);
  }
  const add = missing.map((k) => `  '${k}': ${en.get(k)}`).join('\n');
  s = s.slice(0, at) + '\n' + add + s.slice(at);
  writeFileSync(p, s, 'utf8');
}

const count = (list) => Object.values(list).reduce((n, ks) => n + ks.length, 0);
const already = count(previous);
const listed = Object.entries(ledger).reduce(
  (n, [loc, ks]) => n + ks.filter((k) => !(previous[loc] ?? []).includes(k)).length,
  0,
);

if (!dry) writeFileSync(LEDGER, JSON.stringify({ ...before, locales: ledger }, null, 2) + '\n', 'utf8');

console.log(
  `${dry ? 'would seed' : 'seeded'} ${seeded} missing value(s) into ${touched} catalogue(s); ` +
    `${listed} newly listed as owed, ${already} already owed, ${count(ledger)} owed in total`,
);

const inGerman = entries('de.ts');
const german = [...en.keys()].filter((k) => !inGerman.has(k));
if (german.length) {
  console.error(`de.ts is written by hand and was not seeded. Write the German for: ${german.join(', ')}`);
  process.exit(1);
}
