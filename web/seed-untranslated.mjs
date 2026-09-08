// Fills a catalogue's missing keys with the English text and records each one as
// a debt, so the tree compiles while a translation wave is still owed.
//
// WHY THIS CHANGES NOTHING ON SCREEN. lib/i18n.tsx resolves `dict[key] ?? en[key]`,
// so a key MISSING from a catalogue already renders as English. Seeding the
// English text is therefore identical at runtime to the fallback that was going
// to happen anyway; the only thing it buys is the type gate, because
// `Dict = Record<TranslationKey, string>` makes a missing key a compile error in
// 41 files at once.
//
// WHY THE LEDGER IS NOT OPTIONAL. A seeded English string LOOKS translated: the
// next sweep counts keys, finds parity, and moves on, and forty languages keep a
// paragraph of English for ever. A missing key at least fails loudly. So the debt
// is written down, per locale and per key, and check-untranslated.mjs holds the
// two of them together.
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

const ledger = {};
let touched = 0;
for (const file of files) {
  const loc = file.replace('.ts', '');
  const have = new Set(keysOf(file));
  const missing = enKeys.filter((k) => !have.has(k));
  if (missing.length === 0) continue;

  const bad = missing.filter((k) => en.get(k) === undefined);
  if (bad.length) {
    console.error(`${loc}: cannot read the English value of ${bad.length} key(s), first ${bad[0]}`);
    process.exit(1);
  }

  ledger[loc] = missing;
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

const total = Object.values(ledger).reduce((n, ks) => n + ks.length, 0);
if (!dry) {
  writeFileSync(
    LEDGER,
    JSON.stringify(
      {
        note:
          'Keys carrying the ENGLISH text in a non-English catalogue, because the translation wave for ' +
          'them has not run yet. Identical on screen to the fallback lib/i18n.tsx already makes for a ' +
          'missing key; listed here so a later sweep can find them, since a seeded string is otherwise ' +
          'indistinguishable from a translated one. check-untranslated.mjs holds this file and the ' +
          'catalogues together: translate a key and its entry must go, or the check fails.',
        locales: ledger,
      },
      null,
      2,
    ) + '\n',
    'utf8',
  );
}
console.log(`${dry ? 'would seed' : 'seeded'} ${total} value(s) across ${touched} catalogue(s)`);
