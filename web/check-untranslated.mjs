// Holds untranslated.json and the catalogues together.
//
// THE PROBLEM IT EXISTS FOR. A key missing from a catalogue renders as English,
// because lib/i18n.tsx resolves `dict[key] ?? en[key]`. So when a wave of new
// keys lands in en.ts and the translation pass has not run yet, the honest
// choices are a red build in 41 files or a seeded English value. Seeding is what
// this repo does (see seed-untranslated.mjs), and it has one failure mode: a
// seeded string is indistinguishable from a translated one, so the next sweep
// counts keys, finds parity, and forty languages keep a paragraph of English for
// ever.
//
// So the seeding writes a ledger, and this holds the two together:
//
//   still English   every key the ledger claims is a placeholder must still carry
//                   en.ts's own text. Translate one and its entry has to go, or
//                   this fails and says which - that is what keeps the ledger
//                   from rotting into a list of things that were fixed years ago.
//   still there     a ledger entry naming a key en.ts no longer has is a leftover
//                   and fails, the same way the settings-search index fails on a
//                   key that was renamed out from under it.
//   nothing hidden  a catalogue may not be missing a key outright. That is tsc's
//                   job too, but tsc reports it as one enormous type error per
//                   file; this names the key.
//
// It deliberately does NOT flag every value that happens to equal English.
// Plenty are right: DRM is DRM, Matrix is Matrix, and a Dutch "Import" is an
// English one. Only what the ledger claims is checked.
//
// Run by CI and by hand: `node web/check-untranslated.mjs`
import { readFileSync, readdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const dir = join(here, 'src/lib/locales');

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
const files = readdirSync(dir).filter((f) => f.endsWith('.ts') && f !== 'en.ts' && f !== 'index.ts');

let ledger = { locales: {} };
try {
  ledger = JSON.parse(readFileSync(join(here, 'untranslated.json'), 'utf8'));
} catch {
  /* no ledger is a valid state: it means nothing is owed */
}

const problems = [];
const translated = [];

for (const [loc, keys] of Object.entries(ledger.locales ?? {})) {
  const file = `${loc}.ts`;
  if (!files.includes(file)) {
    problems.push(`untranslated.json names ${loc}, which is not a catalogue`);
    continue;
  }
  const d = entries(file);
  for (const k of keys) {
    if (!en.has(k)) {
      problems.push(`${loc} ${k}: listed as untranslated, but en.ts no longer has that key - drop the entry`);
      continue;
    }
    const v = d.get(k);
    if (v === undefined) {
      problems.push(`${loc} ${k}: listed as untranslated and missing from the catalogue entirely`);
      continue;
    }
    if (v !== en.get(k)) translated.push(`${loc} ${k}`);
  }
}

// Key parity, named. tsc catches this too, but as one type error per file.
for (const file of files) {
  const d = entries(file);
  const loc = file.replace('.ts', '');
  const missing = [...en.keys()].filter((k) => !d.has(k));
  for (const k of missing.slice(0, 3)) problems.push(`${loc} ${k}: missing from the catalogue`);
  if (missing.length > 3) problems.push(`${loc}: ${missing.length - 3} further key(s) missing`);
}

if (translated.length) {
  problems.push(
    `${translated.length} key(s) have been translated but are still listed in untranslated.json. ` +
      `Remove their entries: ${translated.slice(0, 6).join(', ')}${translated.length > 6 ? ', ...' : ''}`,
  );
}

if (problems.length) {
  console.log(`${problems.length} problem(s):`);
  for (const p of problems.slice(0, 40)) console.log('  ' + p);
  if (problems.length > 40) console.log(`  ... and ${problems.length - 40} more`);
  process.exit(1);
}

const owed = Object.values(ledger.locales ?? {}).reduce((n, ks) => n + ks.length, 0);
const locales = Object.keys(ledger.locales ?? {}).length;
console.log(
  owed === 0
    ? `ok: ${files.length} catalogues, nothing owed`
    : `ok: ${files.length} catalogues in step; ${owed} value(s) across ${locales} catalogue(s) still carry the English text and are listed as owed`,
);
