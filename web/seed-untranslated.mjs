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

// GELESEN UND NICHT UEBERSCHRIEBEN, und das ist hier einmal schiefgegangen.
//
// Dieses Skript schrieb die Schuldenliste frisch aus dem, was in DIESEM Lauf
// gefehlt hat. Beim ersten Mal stimmte das, weil da alles fehlte. Beim zweiten
// Mal, fuer einen einzigen nachgereichten Schluessel, ersetzte es eine Liste
// von 13766 offenen Werten durch eine mit 40 - und beides zusammen, Baum
// kompiliert und Wache gruen, sah aus wie Ordnung. Eine Schuldenliste, die beim
// Nachtragen vergisst, ist schlimmer als keine: sie behauptet Vollstaendigkeit.
//
// DIE GANZE VORIGE DATEI wird gelesen, nicht nur ihre Schuldenliste, und das ist
// der zweite Anlauf auf dieselbe Sorte Fehler. Geschrieben wurde `{ note,
// locales }`, also flog jedes Feld, das dieses Skript nicht kennt, beim naechsten
// Lauf lautlos raus - und inzwischen gibt es so ein Feld: "identical", die 627
// Werte, die vierzig Laeufe in ihrer jeweiligen Sprache angesehen und bewusst
// englisch gelassen haben. Ein Saelauf fuer einen einzigen nachgereichten
// Schluessel haette diese Arbeit weggeworfen, und weil danach alles kompiliert
// und jede Wache gruen ist, waere es erst beim naechsten Uebersetzungsdurchgang
// aufgefallen: 627 wieder offene Werte, vierzig Uebersetzer, dasselbe Urteil noch
// einmal.
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
  'Keys carrying the ENGLISH text in a non-English catalogue, because the translation wave for them has ' +
  'not run yet. Identical on screen to the fallback lib/i18n.tsx already makes for a missing key; listed ' +
  'here so a later sweep can find them, since a seeded string is otherwise indistinguishable from a ' +
  'translated one. check-untranslated.mjs holds this file and the catalogues together: translate a key ' +
  'and its entry must go, or the check fails.';

// The note is KEPT when the file already has one. It carries decisions somebody
// wrote down by hand (why de.ts is never listed, why over-reporting is the safe
// direction), and rewriting it from a constant on every run would delete them
// silently - the same shape of forgetting the merge above was added to stop.
const note = before.note || DEFAULT_NOTE;

const carriedTotal = Object.values(previous).reduce((n, ks) => n + ks.length, 0);
const seededNow = Object.entries(ledger).reduce(
  (n, [loc, ks]) => n + ks.filter((k) => !(previous[loc] ?? []).includes(k)).length,
  0,
);
const owedTotal = Object.values(ledger).reduce((n, ks) => n + ks.length, 0);

if (!dry) writeFileSync(LEDGER, JSON.stringify({ ...before, note, locales: ledger }, null, 2) + '\n', 'utf8');

// Reported as three separate numbers, because they answer three different
// questions and one of them reading for the others is how a run that seeded
// NOTHING once looked like a run that had seeded everything.
console.log(
  `${dry ? 'would seed' : 'seeded'} ${seededNow} new value(s) across ${touched} catalogue(s); ` +
    `${carriedTotal} already owed, ${owedTotal} owed in total`,
);
