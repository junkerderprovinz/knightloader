// Nimmt aus untranslated.json heraus, was inzwischen wirklich uebersetzt ist.
//
// Die Schuldenliste ist eine Behauptung ("diese Werte tragen noch den englischen
// Text"), und check-untranslated.mjs prueft sie gegen die Kataloge. Nach einer
// Uebersetzungswelle stimmt sie nicht mehr, und die Wache sagt das laut: sie
// nennt jeden Schluessel, der uebersetzt wurde und noch drinsteht. Das ist
// Absicht und der Grund, warum diese Datei nicht von selbst mitwaechst - eine
// Liste, die sich stillschweigend selbst korrigiert, koennte nie melden, dass
// jemand etwas uebersetzt und vergessen hat.
//
// Also raeumt dieses Skript sie auf, absichtlich und sichtbar, und sagt was es
// entfernt hat. Ein Eintrag bleibt genau dann stehen, wenn sein Wert Zeichen
// fuer Zeichen der englische ist.
//
// ACHTUNG: ein Wert, der in dieser Sprache legitim gleich lautet (DRM, Matrix,
// ein blankes "Import"), bleibt damit als Schuld stehen, obwohl er fertig ist.
// Das ist die richtige Richtung zu irren: eine Schuld zu viel faellt beim
// naechsten Durchgang auf, eine Schuld zu wenig nie.
//
// Dieser Durchgang ist inzwischen gelaufen, und deshalb hat die Datei eine
// zweite Liste: "identical" fuer die 627 Werte, die je ein Lauf in der Sprache
// selbst angesehen und bewusst stehengelassen hat. Der Pruner raeumt beide auf,
// nach derselben Regel, denn beide behaupten dasselbe ueber den Katalog. Nur die
// Bedeutung des Fundes ist verschieden: aus "locales" faellt etwas, WEIL es
// uebersetzt wurde, aus "identical", weil jemand ein Urteil umgestossen hat.
//
// Lauf: node web/prune-untranslated.mjs          (schreibt)
//       node web/prune-untranslated.mjs --dry    (sagt nur, was wegfiele)
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
      buf = [start[2]];
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

/** Behaelt je Sprache nur die Schluessel, deren Wert noch der englische ist. */
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
  // Nur die zwei Listen ersetzen. Das uebrige Objekt (beide Notizen) bleibt wie
  // es ist, damit von Hand geschriebene Begruendungen einen Lauf ueberleben.
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
// Getrennt gemeldet, weil ein Fund hier etwas anderes heisst: jemand hat einem
// geprueften "bleibt englisch" widersprochen, und das gehoert gesehen, nicht in
// derselben Zahl versteckt.
if (same.dropped) {
  console.log(
    `${dry ? 'would also drop' : 'also dropped'} ${same.dropped} reviewed entr${same.dropped === 1 ? 'y' : 'ies'} ` +
      `that stopped being identical to English; ${same.kept} still are`,
  );
}
