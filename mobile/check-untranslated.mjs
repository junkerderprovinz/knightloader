// Holds mobile/untranslated.json and the app's catalogues together, the way
// web/check-untranslated.mjs does for the web UI, over a ledger in the same
// format, so one translation pass can fill both.
//
// It is stricter than the web check in one place. The web UI has many short
// values that equal the English one by right (Status, Online, DRM) and were
// never listed one by one, so there only a whole sentence left in English
// fails. The app has few enough that every one of them is written down, so
// here any value equal to the English one fails unless the ledger names it:
// owed under "locales", or checked and meant to stay under "identical". A key
// copied into every catalogue in English fails until seed-untranslated.mjs
// lists it, and a label nobody translated cannot hide behind being short.
//
// de.ts is written by hand and never listed, so a German word that is also the
// English one (Downloads, Premium) is not a debt. Only the web's sentence rule
// applies to it.
//
// Run: node mobile/check-untranslated.mjs
import { readFileSync, readdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const dir = join(here, 'src/i18n');

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
const files = readdirSync(dir).filter((f) => /^[a-z]{2}\.ts$/.test(f) && f !== 'en.ts');

const ledger = JSON.parse(readFileSync(join(here, 'untranslated.json'), 'utf8'));

const problems = [];
const translated = [];
const overruled = [];

/** Both lists claim the value is byte for byte the English one; `landing` only
 *  decides where a key that has stopped being English is reported. */
function verify(list, label, landing) {
  for (const [loc, keys] of Object.entries(list ?? {})) {
    const file = `${loc}.ts`;
    if (!files.includes(file)) {
      problems.push(`untranslated.json names ${loc} under ${label}, which is not a catalogue`);
      continue;
    }
    const d = entries(file);
    for (const k of keys) {
      if (!en.has(k)) {
        problems.push(`${loc} ${k}: listed under ${label}, but en.ts no longer has that key, drop the entry`);
        continue;
      }
      const v = d.get(k);
      if (v === undefined) {
        problems.push(`${loc} ${k}: listed under ${label} and missing from the catalogue entirely`);
        continue;
      }
      if (v !== en.get(k)) landing.push(`${loc} ${k}`);
    }
  }
}

verify(ledger.locales, 'locales', translated);
verify(ledger.identical, 'identical', overruled);

for (const label of ['locales', 'identical']) {
  if (ledger[label]?.de) {
    problems.push(
      `untranslated.json lists de under ${label}; de.ts is written by hand, so write the German instead`,
    );
  }
}

for (const [loc, keys] of Object.entries(ledger.identical ?? {})) {
  const owed = new Set(ledger.locales?.[loc] ?? []);
  for (const k of keys) {
    if (owed.has(k)) problems.push(`${loc} ${k}: listed as owed and as reviewed-identical, pick one`);
  }
}

// The web check's thresholds, for de.ts alone: long and several words is prose,
// and prose byte-identical to the English was not translated.
const LONG = 30;
const WORDS = 4;
const isSentence = (ev) => {
  const text = ev.replace(/^'|',?$/g, '');
  return text.length >= LONG && (text.match(/ /g) || []).length >= WORDS;
};

for (const file of files) {
  const loc = file.slice(0, -3);
  const d = entries(file);
  const written = new Set([...(ledger.locales?.[loc] ?? []), ...(ledger.identical?.[loc] ?? [])]);
  for (const [k, ev] of en) {
    if (written.has(k) || d.get(k) !== ev) continue;
    if (loc === 'de') {
      if (isSentence(ev)) problems.push(`de ${k}: a full sentence is still the English one`);
      continue;
    }
    problems.push(
      `${loc} ${k}: still the English text, and untranslated.json names it neither as owed nor as identical. ` +
        `Run seed-untranslated.mjs, or list it under identical if English is right there`,
    );
  }
}

// Key parity, named. tsc catches this too, but as one type error per file.
for (const file of files) {
  const d = entries(file);
  const loc = file.slice(0, -3);
  const missing = [...en.keys()].filter((k) => !d.has(k));
  for (const k of missing.slice(0, 3)) problems.push(`${loc} ${k}: missing from the catalogue`);
  if (missing.length > 3) problems.push(`${loc}: ${missing.length - 3} further key(s) missing`);
}

if (translated.length) {
  problems.push(
    `${translated.length} key(s) have been translated but are still listed as owed in untranslated.json. ` +
      `Run prune-untranslated.mjs: ${translated.slice(0, 6).join(', ')}${translated.length > 6 ? ', ...' : ''}`,
  );
}

if (overruled.length) {
  problems.push(
    `${overruled.length} key(s) are listed as reviewed-identical to English and are not. ` +
      `That is allowed, the language wanting its own word after all, but the entry goes ` +
      `with it: ${overruled.slice(0, 6).join(', ')}${overruled.length > 6 ? ', ...' : ''}`,
  );
}

if (problems.length) {
  console.log(`${problems.length} problem(s):`);
  for (const p of problems.slice(0, 40)) console.log('  ' + p);
  if (problems.length > 40) console.log(`  ... and ${problems.length - 40} more`);
  process.exit(1);
}

const count = (l) => Object.values(l ?? {}).reduce((n, ks) => n + ks.length, 0);
const owed = count(ledger.locales);
const locales = Object.keys(ledger.locales ?? {}).length;

console.log(
  `ok: ${files.length} catalogues in step; ` +
    (owed === 0
      ? 'nothing owed'
      : `${owed} value(s) across ${locales} catalogue(s) still carry the English text and are listed as owed`) +
    `; ${count(ledger.identical)} reviewed and left identical to English`,
);
