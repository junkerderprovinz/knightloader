// Holds untranslated.json and the catalogues together.
//
// A key missing from a catalogue renders as English, because lib/i18n.tsx
// resolves `dict[key] ?? en[key]`. When a wave of new keys lands in en.ts before
// the translation pass, the choices are a red build in 41 files or a seeded
// English value. This repo seeds (see seed-untranslated.mjs), and a seeded
// string is indistinguishable from a translated one, so the next sweep counts
// keys, finds parity, and forty languages keep a paragraph of English for ever.
//
// The seeding therefore writes a ledger, and this holds the two together:
//
//   still English   every key the ledger claims is a placeholder still carries
//                   en.ts's own text. Translate one and its entry goes, or this
//                   fails and says which, which is what keeps the ledger from
//                   rotting into a list of things that were fixed long ago.
//   still there     a ledger entry naming a key en.ts does not have is a
//                   leftover and fails, the way the settings-search index fails
//                   on a key renamed out from under it.
//   nothing hidden  no catalogue is missing a key outright. That is tsc's job
//                   too, but tsc reports one enormous type error per file while
//                   this names the key.
//
// It does not flag every value that happens to equal English, and de.ts is why:
// it is written by hand by a native speaker, and 114 of its values are
// byte-identical to the English, Downloads being Downloads, and so Status,
// Import, Online and Captcha. A check that fires on a correct file gets
// switched off.
//
// A whole sentence is never a coincidence, so the rule at the bottom of this
// file is that a value of some length carrying several words may not be
// byte-identical to English unless the ledger says so, in either list. de.ts
// passes it with nothing listed, which is the calibration that makes it
// trustworthy. The eleven it does catch are right to be English: the <jd:...>
// variable names, and "{peers} peers, {seeds} seeds" in the languages that took
// the BitTorrent words over unchanged.
//
// The ledger has two lists, and they mean opposite things:
//
//   locales     still owed. The value is English because nobody has translated
//               it yet, and the list is meant to shrink to nothing.
//   identical   reviewed and left. English is the right answer in that
//               language: yt-dlp is yt-dlp, '{n}/{max}' holds no words, and a
//               keyboard whose Del key says Del wants the tooltip to say Del.
//               This list is permanent.
//
// Both are checked the same three ways, being the same kind of claim about the
// catalogues. What differs is the meaning of a failure: a key that stops being
// English in `locales` is progress and its entry merely stale, while one in
// `identical` is somebody overruling a judgement, which is allowed once the
// entry is dropped. The split is what stops the next seeding wave reopening 627
// settled questions.
//
// Run: `node web/check-untranslated.mjs`
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
      buf = start[2] ? [start[2]] : [];
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
const overruled = [];

/**
 * Checks one of the two lists. Both claim the same thing about a catalogue - the
 * value is byte-for-byte the English one - so both are verified identically;
 * `landing` only decides where a key that has stopped being English is reported.
 */
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
        problems.push(`${loc} ${k}: listed under ${label}, but en.ts no longer has that key - drop the entry`);
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

// A key cannot be both owed and settled. The two lists are edited by different
// scripts, so the contradiction would otherwise sit in both counts for ever.
for (const [loc, keys] of Object.entries(ledger.identical ?? {})) {
  const owed = new Set(ledger.locales?.[loc] ?? []);
  for (const k of keys) {
    if (owed.has(k)) problems.push(`${loc} ${k}: listed as owed and as reviewed-identical, pick one`);
  }
}

// No sentence left in English that nobody has written down.
//
// Both thresholds together: length alone would catch the <jd:...> variable
// list, word count alone the "Home End Del" style labels. A value that is long
// and several words is prose, and prose byte-identical across two languages was
// not translated. Either list counts as having written it down.
const LONG = 30;
const WORDS = 4;
for (const file of files) {
  const loc = file.replace('.ts', '');
  const d = entries(file);
  const written = new Set([...(ledger.locales?.[loc] ?? []), ...(ledger.identical?.[loc] ?? [])]);
  for (const [k, ev] of en) {
    if (written.has(k) || d.get(k) !== ev) continue;
    const text = ev.replace(/^'|',?$/g, '');
    if (text.length < LONG || (text.match(/ /g) || []).length < WORDS) continue;
    problems.push(
      `${loc} ${k}: a full sentence is still the English one, and nothing in untranslated.json says why`,
    );
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
const settled = count(ledger.identical);

// Both numbers: with nothing owed, the settled count is what says those values
// are English because somebody looked.
console.log(
  `ok: ${files.length} catalogues in step; ` +
    (owed === 0
      ? 'nothing owed'
      : `${owed} value(s) across ${locales} catalogue(s) still carry the English text and are listed as owed`) +
    `; ${settled} reviewed and left identical to English`,
);
