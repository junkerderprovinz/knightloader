// Takes out of untranslated.json what has meanwhile been translated.
//
// The debt list is a claim, that these values still carry the English text, and
// check-untranslated.mjs holds it against the catalogues. After a translation
// wave it is out of date and the check names every key that was translated and
// is still listed. That is the point of not growing this file by itself: a list
// that corrected itself quietly could never report that somebody translated
// something and forgot to say so.
//
// So the clearing out happens here, visibly, and the script says what it
// removed. An entry stays exactly when its value is character for character the
// English one.
//
// A value that is legitimately the same in that language (DRM, Matrix, a bare
// "Import") therefore stays listed as owed although it is finished. That is the
// right direction to be wrong in: one debt too many turns up on the next pass,
// one too few never does.
//
// The file carries a second list, "identical", for the values somebody looked
// at in the language itself and left English. Both are cleared by the same
// rule, since both claim the same thing about the catalogue. Only the meaning
// of a find differs: out of "locales" something falls because it was
// translated, out of "identical" because somebody overruled a judgement.
//
// Run: node web/prune-untranslated.mjs          (writes)
//      node web/prune-untranslated.mjs --dry    (says what would go)
import { readFileSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const dir = join(here, 'src/lib/locales');
const LEDGER = join(here, 'untranslated.json');
const dry = process.argv.includes('--dry');

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
const ledger = JSON.parse(readFileSync(LEDGER, 'utf8'));

/** Keeps, per language, only the keys whose value is still the English one. */
function sweep(list) {
  const next = {};
  let kept = 0;
  let dropped = 0;
  for (const [loc, keys] of Object.entries(list ?? {})) {
    const d = entries(`${loc}.ts`);
    const still = keys.filter((k) => d.get(k) === en.get(k));
    dropped += keys.length - still.length;
    kept += still.length;
    if (still.length) next[loc] = still;
  }
  return { next, kept, dropped };
}

const owed = sweep(ledger.locales);
const same = sweep(ledger.identical);

if (!dry) {
  // Only the two lists are replaced. The rest of the object, both notes
  // included, stays as it is, so reasons written by hand survive a run.
  ledger.locales = owed.next;
  if (ledger.identical) ledger.identical = same.next;
  writeFileSync(LEDGER, JSON.stringify(ledger, null, 2) + '\n', 'utf8');
}

const locales = Object.keys(owed.next).length;
console.log(
  `${dry ? 'would drop' : 'dropped'} ${owed.dropped} translated entr${owed.dropped === 1 ? 'y' : 'ies'}; ` +
    (owed.kept === 0
      ? 'nothing is owed any more'
      : `${owed.kept} still owed across ${locales} catalogue${locales === 1 ? '' : 's'}`),
);
// Reported separately, because a find here means something else: somebody
// contradicted a reviewed "stays English", and that belongs in view rather than
// hidden inside the same number.
if (same.dropped) {
  console.log(
    `${dry ? 'would also drop' : 'also dropped'} ${same.dropped} reviewed entr${same.dropped === 1 ? 'y' : 'ies'} ` +
      `that stopped being identical to English; ${same.kept} still are`,
  );
}
