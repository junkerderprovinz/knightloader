// Fills a catalogue's missing keys with the English text and records each one as
// a debt, so the tree compiles while a translation wave is still owed.
//
// Nothing changes on screen: lib/i18n.tsx resolves `dict[key] ?? en[key]`, so a
// key missing from a catalogue already renders as English. What the seeding buys
// is the type gate, since `Dict = Record<TranslationKey, string>` makes a
// missing key a compile error in 41 files at once.
//
// The ledger is not optional, because a seeded English string looks translated:
// the next sweep counts keys, finds parity and moves on, and forty languages
// keep a paragraph of English for ever, where a missing key at least fails
// loudly. So the debt is written down per locale and per key, and
// check-untranslated.mjs holds the two together.
//
// Run: node web/seed-untranslated.mjs          (writes)
//      node web/seed-untranslated.mjs --dry    (says what it would write)
import { readFileSync, writeFileSync, readdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const dir = join(here, 'src/lib/locales');
const LEDGER = join(here, 'untranslated.json');
const dry = process.argv.includes('--dry');

const keysOf = (file) =>
  (readFileSync(join(dir, file), 'utf8').match(/^ {2}'[^']+':/gm) || []).map((x) => x.slice(3, -2));

/** key -> the raw source text of its value, wrapped values included. */
function entries(file) {
  const out = new Map();
  let key = null;
  let buf = [];
  for (const line of readFileSync(join(dir, file), 'utf8').split('\n')) {
    const start = line.match(/^ {2}'([^']+)':\s*(.*)$/);
    if (start) {
      if (key) out.set(key, buf.join(' '));
      key = start[1];
      buf = [start[2]];
      if (/,\s*$/.test(start[2])) {
        out.set(key, buf.join(' '));
        key = null;
        buf = [];
      }
      continue;
    }
    if (key === null) continue;
    if (/^\s*(\/\/|\/\*|\}|$)/.test(line)) {
      out.set(key, buf.join(' '));
      key = null;
      buf = [];
      continue;
    }
    buf.push(line.trim());
    if (/,\s*$/.test(line)) {
      out.set(key, buf.join(' '));
      key = null;
      buf = [];
    }
  }
  if (key) out.set(key, buf.join(' '));
  return out;
}

const en = entries('en.ts');
const enKeys = keysOf('en.ts');
const files = readdirSync(dir).filter((f) => f.endsWith('.ts') && f !== 'en.ts' && f !== 'index.ts');

// The whole previous ledger is read and merged, not just its debt list and not
// overwritten. A run that wrote the list from what was missing this time would
// replace thousands of open values with the handful a single late key produces,
// and a debt list that forgets while being added to is worse than none, because
// it claims to be complete. Reading the whole file keeps the fields this script
// does not know, `identical` above all: the values somebody looked at in their
// own language and left English. Losing them compiles and leaves every check
// green, so it would surface only at the next translation pass.
const before = (() => {
  try {
    return JSON.parse(readFileSync(LEDGER, 'utf8'));
  } catch {
    return {};
  }
})();
const previous = before.locales ?? {};
const settled = before.identical ?? {};

const ledger = {};
let touched = 0;
for (const file of files) {
  const loc = file.replace('.ts', '');
  const have = new Set(keysOf(file));
  const missing = enKeys.filter((k) => !have.has(k));

  // What this locale already owed stays owed. A key seeded three waves ago is
  // still carrying English whether or not this run happened to touch it.
  //
  // Except what has already been ruled on: a key in `identical` is English
  // because someone working in this language decided it should be, and carrying
  // it back into the debt would put it in both lists at once. check-untranslated
  // refuses that state; this is the side that must not create it.
  const decided = new Set(settled[loc] ?? []);
  const carried = (previous[loc] ?? []).filter((k) => en.has(k) && !decided.has(k));
  const union = [...new Set([...carried, ...missing])].filter((k) => !decided.has(k));
  if (union.length) ledger[loc] = union;
  if (missing.length === 0) continue;

  const bad = missing.filter((k) => en.get(k) === undefined);
  if (bad.length) {
    console.error(`${loc}: cannot read the English value of ${bad.length} key(s), first ${bad[0]}`);
    process.exit(1);
  }

  touched++;
  if (dry) continue;

  const p = join(dir, file);
  let s = readFileSync(p, 'utf8');
  // Every catalogue but en.ts closes with `};` - en.ts closes `} as const;` and
  // is never seeded, being the source the seed comes from.
  const at = s.lastIndexOf('\n};');
  if (at < 0) {
    console.error(`${p}: no closing brace found - refusing to guess`);
    process.exit(1);
  }
  const add = missing.map((k) => `  '${k}': ${en.get(k)}`).join('\n');
  s = s.slice(0, at) + '\n' + add + s.slice(at);
  writeFileSync(p, s, 'utf8');
}

const DEFAULT_NOTE =
  'Keys carrying the English text in a non-English catalogue, because the translation wave for them has ' +
  'not run yet. Identical on screen to the fallback lib/i18n.tsx already makes for a missing key; listed ' +
  'here so a later sweep can find them, since a seeded string is otherwise indistinguishable from a ' +
  'translated one. check-untranslated.mjs holds this file and the catalogues together: translate a key ' +
  'and its entry must go, or the check fails.';

// An existing note is kept: it carries decisions somebody wrote down by hand,
// such as why de.ts is never listed, and rewriting it from a constant on every
// run would drop them without a word.
const note = before.note || DEFAULT_NOTE;

const carriedTotal = Object.values(previous).reduce((n, ks) => n + ks.length, 0);
const seededNow = Object.entries(ledger).reduce(
  (n, [loc, ks]) => n + ks.filter((k) => !(previous[loc] ?? []).includes(k)).length,
  0,
);
const owedTotal = Object.values(ledger).reduce((n, ks) => n + ks.length, 0);

if (!dry) writeFileSync(LEDGER, JSON.stringify({ ...before, note, locales: ledger }, null, 2) + '\n', 'utf8');

// Three separate numbers, because they answer three questions: one of them read
// for the others makes a run that seeded nothing look like a run that seeded
// everything.
console.log(
  `${dry ? 'would seed' : 'seeded'} ${seededNow} new value(s) across ${touched} catalogue(s); ` +
    `${carriedTotal} already owed, ${owedTotal} owed in total`,
);
