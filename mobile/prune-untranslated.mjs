// Takes out of mobile/untranslated.json what has meanwhile been translated, and
// out of its "identical" list what somebody has since given a word of its own.
// The same rule as web/prune-untranslated.mjs, which explains why this is a
// separate step that says what it removed: a ledger that corrected itself
// quietly could never report a translation nobody wrote down.
//
// Run: node mobile/prune-untranslated.mjs          (writes)
//      node mobile/prune-untranslated.mjs --dry    (says what would go)
import { readFileSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const dir = join(here, 'src/i18n');
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
if (same.dropped) {
  console.log(
    `${dry ? 'would also drop' : 'also dropped'} ${same.dropped} reviewed entr${same.dropped === 1 ? 'y' : 'ies'} ` +
      `that stopped being identical to English; ${same.kept} still are`,
  );
}
