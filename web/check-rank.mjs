// What "matches" means, checked against the ranker that ships.
//
// Two surfaces read lib/rank.ts now - the command palette and the settings
// search - so a change to it changes both at once, and neither has a test
// runner behind it. The rule most worth defending is the quiet one: FOLDING.
// Lowercasing alone, which is all this scorer did while it lived inside
// CommandPalette.tsx, makes every accented language a set of dead ends, and the
// failure looks exactly like "there is nothing called that": in a German UI
// "grosse" never finds "Größe" and "uber" never finds "Überwachung". Nobody
// reports an empty result list as a bug in string normalisation.
//
// It drives the REAL module rather than a copy: Node strips the types out of
// src/lib/rank.ts, which is why that file is kept free of any import at all -
// the same arrangement check-search-query.mjs already uses for
// src/lib/searchQuery.ts. Run by hand or from CI: `node web/check-rank.mjs`.

import { fold, score, scoreFolded, scoreProse } from './src/lib/rank.ts';

const problems = [];

function check(what, got, want) {
  const a = JSON.stringify(got);
  const b = JSON.stringify(want);
  if (a !== b) problems.push(`${what}\n      got  ${a}\n      want ${b}`);
}

/** Does this label match this query at all - the only question most call sites ask. */
const hits = (label, query) => score(label, query) >= 0;

// --- Folding, in the languages this catalogue actually ships -----------------

check('an umlaut folds to its base letter', fold('Größe'), 'grosse');
check('ß and ẞ both become ss', fold('STRAßE ẞ'), 'strasse ss');
check('a French accent folds', fold('Qualité'), 'qualite');
check('a Czech caron folds', fold('Přenos'), 'prenos');
check('plain ASCII is only lowercased', fold('Download Folder'), 'download folder');
check('a script with no marks to strip is left alone', fold('Загрузки'), 'загрузки');
// Greek is not a special case, it is the rule working outside Latin: the tonos
// is a combining mark like any other, so somebody typing without it still finds
// the word - which is exactly how Greek is typed in a hurry.
check('a Greek tonos folds like any other mark', fold('Λήψεις'), 'ληψεις');
check('ληψεις finds Λήψεις', hits('Λήψεις', 'ληψεις'), true);

// The letters NFD has nothing to say about. Every one of these was wrong before
// check-rank.mjs existed - the fold had ß and stopped there, so six of the
// shipped languages folded to themselves while German and French looked fine.
check('a Turkish dotless i becomes i', fold('Bağlantı'), 'baglanti');
check('a Polish stroked l becomes l', fold('Połączenie'), 'polaczenie');
check('a Danish slashed o becomes o', fold('Størrelse'), 'storrelse');
check('an Icelandic thorn and eth fold', fold('Þjöðvegur'), 'thjodvegur');
check('a Croatian stroked d becomes d', fold('Đaci'), 'daci');
check('a ligature becomes both its letters', fold('Æblegrød œuf'), 'aeblegrod oeuf');

// --- The point of folding: typing it the easy way still finds it -------------

check('grosse finds Größe', hits('Größe', 'grosse'), true);
check('größe finds Größe', hits('Größe', 'größe'), true);
check('uber finds Überwachung', hits('Überwachung', 'uber'), true);
check('qualite finds Qualité', hits('Qualité', 'qualite'), true);
check('baglanti finds Bağlantı', hits('Bağlantı', 'baglanti'), true);
check('polaczenie finds Połączenie', hits('Połączenie', 'polaczenie'), true);
// And the fold does NOT invent matches across languages: German "Qualität"
// folds to "qualitat", which the French spelling is not a subsequence of.
check('qualite does not find Qualität', hits('Qualität', 'qualite'), false);
// Folded on BOTH sides, always. Folding only the query would make the accented
// spelling of a word unable to find itself, which is worse than not folding.
check('the accented spelling still finds itself', hits('Größe', 'Größe'), true);

// --- Ranking: lower is better, -1 is no match -------------------------------

check('no query matches everything at rank 0', score('anything', ''), 0);
check('a query with no relation does not match', score('Downloads', 'zzq'), -1);
check('a prefix is the best possible hit', score('Downloads', 'down'), 0);
check(
  'a hit on a word boundary beats one mid-word',
  score('Slow down', 'down') < score('Shutdown', 'down'),
  true,
);
check(
  'a prefix beats a hit on a word boundary',
  score('Downloads', 'down') < score('Slow down', 'down'),
  true,
);
check(
  'every substring hit beats every subsequence hit',
  score('Slow down', 'down') < score('Command palette', 'cmdp'),
  true,
);
check('a subsequence still counts', score('Command palette', 'cmdp'), 1000);
check('an out-of-order subsequence does not', score('Command palette', 'pdmc'), -1);

// --- scoreFolded is the same function minus the normalising ------------------
//
// The settings search folds its ~700 strings once and keeps them, rather than
// re-normalising all of them on every keystroke. That only holds if the two
// agree on everything else.

check(
  'scoreFolded on folded input equals score on raw input',
  scoreFolded(fold('Größe'), fold('GRÖSSE')),
  score('Größe', 'GRÖSSE'),
);
check('scoreFolded does NOT fold for you', scoreFolded('Größe', 'grosse'), -1);

// --- Prose is matched by substring only, and this is the reason --------------
//
// A real hint out of the settings catalogue. Before scoreProse existed, the
// settings search ran scoreFolded over sentences like this one, and the top five
// results for "speed" were five cards whose explanations happened to contain the
// word - ranked above the row actually called "Speed limit" - while "zzzz"
// matched a paragraph about archive extraction. Every letter of any short query
// appears somewhere in a long enough sentence, in order. Measured, not feared.

const HINT = fold(
  'Archives are extracted automatically: zip (including encrypted), rar with multi-volume sets, ' +
    '7z, tar, and gzip/bzip2/xz/zstd whether or not they wrap a tar.',
);

check('a real word in a sentence is found', scoreProse(HINT, fold('volume')) >= 0, true);
check('a word the sentence does not have is not', scoreProse(HINT, fold('captcha')), -1);
check('nonsense does not match prose', scoreProse(HINT, fold('zzzz')), -1);
check(
  'and that is exactly what subsequence matching would have done instead',
  scoreFolded(HINT, fold('zzzz')) >= 0,
  true,
);
check('an empty query matches prose at rank 0', scoreProse(HINT, ''), 0);
check(
  'a hit early in a sentence beats a hit late in it',
  scoreProse(HINT, fold('archives')) < scoreProse(HINT, fold('zstd')),
  true,
);

if (problems.length > 0) {
  console.error(`${problems.length} ranking problem(s):`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}
console.log('ok: folding and ranking behave');
