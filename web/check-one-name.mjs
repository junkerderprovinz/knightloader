// One thing, one name. A second name for the same thing makes a reader look for
// a second thing, and a name that already belongs to something else makes two
// things look like one. The names held here:
//
//   the built-in torrent client   the module switch, its priority row and the
//                                 debrid texts; the switch does not stop a
//                                 torrent going to a debrid service, so
//                                 "Torrents" would claim more than it switches
//   rejected links                "held" belongs to the Hold action and its row
//                                 mark; the link filter's and the tracker ban's
//                                 area takes the filter's own verdict
//   backends                      a row of the priority order is a backend, and a
//                                 service is a debrid service
//   das Token                     German takes the neuter, as "Leg eins an" does
//   Ihr at a sentence start       reads as the formal "your" in a du interface
//
// A hint that sends the reader to a switch names it as the switch reads.
//
// Run: `node web/check-one-name.mjs`.
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const web = dirname(fileURLToPath(import.meta.url));

/** key -> value of one catalogue, wrapped values included. */
function catalogue(path) {
  const text = readFileSync(join(web, path), 'utf8');
  const out = new Map();
  for (const m of text.matchAll(/^ {2}'([^']+)':\s*\n?\s*(?:'((?:[^'\\]|\\.)*)'|"((?:[^"\\]|\\.)*)")/gm)) {
    out.set(m[1], m[2] ?? m[3]);
  }
  return out;
}

const books = {
  'web en': catalogue('src/lib/locales/en.ts'),
  'web de': catalogue('src/lib/locales/de.ts'),
  'mobile en': catalogue('../mobile/src/i18n/en.ts'),
  'mobile de': catalogue('../mobile/src/i18n/de.ts'),
};

const REJECTED = [
  /^collector\.filtered\./,
  /^collector\.toastStartHeld$/,
  /^torrent\.held$/,
  /^detail\.heldBecause$/,
  /^settings\.rules\.(resultRejected|filterHint|emptyFilter|nameHint|thenHintFilter|action\.reasonHint)$/,
  /^settings\.torrents\.bannedTrackersHint$/,
  /^skipped\.info$/,
  /^downloads\.startHeld$/,
];
const PRIORITY_ROW = [/^accounts\.routing\./, /^props\.backend/];

const rules = [
  { book: /en$/, words: /Torrent and magnet|\bbuilt-in client\b|\btorrent engine\b/i, why: 'the built-in torrent client has one name' },
  { book: /de$/, words: /Torrent & Magnet|\beingebaute[nmrs]? Client|Torrent-Engine/, why: 'der eingebaute Torrent-Client hat einen Namen' },
  { book: /en$/, keys: REJECTED, words: /\bheld\b|\bhold(s|ing)?\b/i, why: 'held is the Hold action; this area is Rejected' },
  { book: /de$/, keys: REJECTED, words: /zurück(ge)?halt|zurückhält|zurückzuhalten|\bhält\b[^.]*\bzurück\b/, why: 'zurückgehalten ist Zurückhalten; hier heißt es Abgelehnt' },
  { book: /en$/, keys: PRIORITY_ROW, words: /\bresolvers?\b|(?<!debrid )\bservices?\b/i, why: 'a row of the priority order is a backend' },
  { book: /de$/, keys: PRIORITY_ROW, words: /Resolver|(?<!Debrid-)\bDienst(e|en|es)?\b/, why: 'eine Zeile der Prioritätsreihenfolge ist ein Backend' },
  {
    book: /de$/,
    words: /\b(der|den|Der|Den|Neuer|neuer|Dieser|dieser|Diesen|diesen|einen|keinen|jeden)\s+(API-)?Token\b/,
    why: 'das Token, sächlich',
  },
  { book: /de$/, words: /(^|[.!?:]\s)Ihr(e|en|em|er|es)?\b/, why: 'Ihr am Satzanfang liest sich als Sie-Form' },
];

const problems = [];
let checked = 0;
for (const [book, values] of Object.entries(books)) {
  for (const rule of rules) {
    if (!rule.book.test(book)) continue;
    for (const [key, value] of values) {
      if (rule.keys && !rule.keys.some((k) => k.test(key))) continue;
      checked++;
      // A placeholder such as {held} is a name for code, not for the reader.
      const hit = rule.words.exec(value.replace(/\{\w+\}/g, ''));
      if (hit) problems.push(`${book} ${key}: "${hit[0]}" (${rule.why})`);
    }
  }
}

// The module switch and its priority row are the same client.
const SWITCH_NAMED_IN = [['accounts.importHint', 'settings.torrents.keepOnService']];
for (const book of ['web en', 'web de']) {
  const module = books[book].get('settings.module.torrents');
  const row = books[book].get('resolver.torrent');
  if (module !== row) problems.push(`${book}: the module is "${module}" but its priority row is "${row}"`);
  for (const [hint, toggle] of SWITCH_NAMED_IN) {
    const name = books[book].get(toggle);
    if (!books[book].get(hint)?.includes(name)) problems.push(`${book} ${hint}: does not name the switch "${name}"`);
  }
}

if (problems.length > 0) {
  console.error(`one name: ${problems.length} problem(s)\n`);
  for (const p of problems) console.error(`  - ${p}`);
  process.exit(1);
}
console.log(`ok: ${checked} values hold their names`);
