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
// IT DOES NOT FLAG EVERY VALUE THAT HAPPENS TO EQUAL ENGLISH, and de.ts is the
// proof that it must not. de.ts is written by hand by a native speaker, and 114
// of its values are byte-identical to the English: Downloads is Downloads, so
// are Status, Import, Online, Captcha. A check counting those would fire on a
// correct file, and a check that fires on correct files gets switched off.
//
// A WHOLE SENTENCE, though, is never a coincidence. So there is one hard rule at
// the bottom of this file: a value of some length carrying several words may not
// be byte-identical to English unless the ledger says so, in either list. de.ts
// passes it with nothing listed, which is exactly the calibration that makes it
// trustworthy - the yardstick file needs no exemption. It caught eleven on the
// day it was written, all of them right to be English (the <jd:...> variable
// names, and "{peers} peers, {seeds} seeds" in the languages that took the
// BitTorrent words over unchanged), and those eleven are now written down rather
// than tolerated by a threshold.
//
// THE LEDGER HAS TWO LISTS, AND THEY MEAN OPPOSITE THINGS.
//
//   locales     still owed. The value is English because nobody has translated
//               it yet. This shrinks to nothing and is meant to.
//   identical   reviewed and left. The value is English because English is the
//               right answer in that language: yt-dlp is yt-dlp, '{n}/{max}'
//               holds no words, and a keyboard whose Del key says Del wants the
//               tooltip to say Del. This one is permanent.
//
// Both are checked the same three ways, because both are the same kind of claim
// about the catalogues. What differs is what a failure means: a key that stops
// being English in `locales` is progress and its entry is simply stale, while
// one in `identical` is somebody overruling a judgement, which is allowed but
// has to be written down by dropping the entry. Splitting them is what stops the
// next seeding wave from re-opening 627 settled questions and sending forty
// translators to answer them again.
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

// A key cannot be both owed and settled. Nothing writes both today, but the two
// lists are edited by different scripts and the contradiction would be invisible
// otherwise: the debt count would keep it, the review count would keep it, and
// neither would ever come out.
for (const [loc, keys] of Object.entries(ledger.identical ?? {})) {
  const owed = new Set(ledger.locales?.[loc] ?? []);
  for (const k of keys) {
    if (owed.has(k)) problems.push(`${loc} ${k}: listed as owed AND as deliberately identical - pick one`);
  }
}

// THE HARD RULE: no sentence left in English that nobody has written down.
//
// Thresholds, and why these two together. LENGTH alone would catch the <jd:...>
// variable list; WORD COUNT alone would catch "Home End Del" style labels. A
// value that is both long AND several words is prose, and prose that is
// byte-identical across two languages was not translated. Both lists count as
// having written it down: `locales` means a wave still owes it, `identical` means
// somebody looked and left it.
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
    `${overruled.length} key(s) are listed as deliberately identical to English but no longer are. ` +
      `That is allowed - somebody decided the language does want its own word - but the entry has to go ` +
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

// Both numbers, always. The settled count is the one worth seeing when the debt
// is zero: it says 627 values are English because someone looked, not because
// nobody has yet.
console.log(
  `ok: ${files.length} catalogues in step; ` +
    (owed === 0
      ? 'nothing owed'
      : `${owed} value(s) across ${locales} catalogue(s) still carry the English text and are listed as owed`) +
    `; ${settled} reviewed and deliberately identical to English`,
);
