// What the search box understands, checked against the parser that ships.
//
// The rule this exists to defend is the one nobody notices breaking: an
// unrecognised prefix, a stray colon, an operator with no number behind it and a
// quote somebody is halfway through typing all have to stay ORDINARY TEXT. That
// failure is silent in the worst way - the list simply comes back empty, the
// person retypes the same query, and there is nothing to report but "search is
// broken". No compiler has an opinion about it, and the UI has no test runner,
// so it gets a check of its own in the shape this repository already uses for
// exactly this problem (extension/check-relay-seal.mjs, check-settings-pages.mjs).
//
// It drives the REAL module rather than a copy: Node strips the types out of
// src/lib/searchQuery.ts, which is why that file is kept free of any import that
// survives compilation. Run by hand or from CI: `node web/check-search-query.mjs`.

import { parseSearch, matchesSearch } from './src/lib/searchQuery.ts';

const problems = [];

function check(what, got, want) {
  const a = JSON.stringify(got);
  const b = JSON.stringify(want);
  if (a !== b) problems.push(`${what}\n      got  ${a}\n      want ${b}`);
}

/** A task with only the fields the search reads, so a case says what it is about. */
function task(fields) {
  return {
    id: 'x',
    url: 'https://host.example/file.bin',
    name: '',
    package: '',
    resolver: '',
    size: 0,
    loaded: 0,
    speed: 0,
    status: 'queued',
    createdAt: new Date().toISOString(),
    priority: 0,
    position: 0,
    enabled: true,
    ...fields,
  };
}

const ago = (ms) => new Date(Date.now() - ms).toISOString();
const DAY = 86_400_000;

// --- Several terms are an AND, and a leading minus excludes -----------------

check(
  'two words are two terms',
  parseSearch('big buck'),
  [
    { kind: 'text', needle: 'big', field: 'any', negate: false },
    { kind: 'text', needle: 'buck', field: 'any', negate: false },
  ],
);
check('a leading minus negates', parseSearch('-sample'), [
  { kind: 'text', needle: 'sample', field: 'any', negate: true },
]);
check(
  'both words have to match',
  [
    matchesSearch(task({ name: 'big buck bunny.mkv' }), { text: 'big bunny', category: 'any' }),
    matchesSearch(task({ name: 'big trouble.mkv' }), { text: 'big bunny', category: 'any' }),
  ],
  [true, false],
);
check(
  'the excluded word wins',
  [
    matchesSearch(task({ name: 'movie.sample.mkv' }), { text: 'movie -sample', category: 'any' }),
    matchesSearch(task({ name: 'movie.mkv' }), { text: 'movie -sample', category: 'any' }),
  ],
  [false, true],
);

// --- Prefixes aim one term at one field -------------------------------------

check('host: aims at the host', parseSearch('host:rapidgator'), [
  { kind: 'text', needle: 'rapidgator', field: 'host', negate: false },
]);
check('paket: is package:', parseSearch('paket:serie'), [
  { kind: 'text', needle: 'serie', field: 'package', negate: false },
]);
check('a prefix can be excluded too', parseSearch('-host:rapidgator'), [
  { kind: 'text', needle: 'rapidgator', field: 'host', negate: true },
]);
check(
  'the prefix really narrows the field',
  [
    // The word is in the package, not in the name, so name: must not find it.
    matchesSearch(task({ name: 'e01.mkv', package: 'Serie' }), { text: 'paket:serie', category: 'any' }),
    matchesSearch(task({ name: 'e01.mkv', package: 'Serie' }), { text: 'name:serie', category: 'any' }),
  ],
  [true, false],
);

// --- Size and age -----------------------------------------------------------

check('a bare comparison is a size', parseSearch('>500mb'), [
  { kind: 'size', op: '>', bytes: 500 * 1024 ** 2, negate: false },
]);
check('size: is the long form', parseSearch('size:<=1gb'), [
  { kind: 'size', op: '<=', bytes: 1024 ** 3, negate: false },
]);
check('aelter: is older:', parseSearch('aelter:7t'), [
  { kind: 'age', olderThan: true, ms: 7 * DAY, negate: false },
]);
check('newer: takes hours', parseSearch('newer:2h'), [
  { kind: 'age', olderThan: false, ms: 2 * 3_600_000, negate: false },
]);
check(
  'the size comparison actually compares',
  [
    matchesSearch(task({ size: 700 * 1024 ** 2 }), { text: '>500mb', category: 'any' }),
    matchesSearch(task({ size: 300 * 1024 ** 2 }), { text: '>500mb', category: 'any' }),
    // Nobody has measured this one yet, so it answers no rather than posing as
    // zero bytes and sweeping into every "<1gb".
    matchesSearch(task({ size: 0 }), { text: '<1gb', category: 'any' }),
  ],
  [true, false, false],
);
check(
  'the age comparison actually compares',
  [
    matchesSearch(task({ createdAt: ago(9 * DAY) }), { text: 'older:7d', category: 'any' }),
    matchesSearch(task({ createdAt: ago(2 * DAY) }), { text: 'older:7d', category: 'any' }),
    matchesSearch(task({ createdAt: ago(2 * DAY) }), { text: 'newer:7d', category: 'any' }),
  ],
  [true, false, true],
);

// --- Everything unrecognised stays plain text -------------------------------
//
// The half of the feature that has to keep working for people who never learn
// that any of the above exists.

check('an unknown prefix is text', parseSearch('c:\\media'), [
  { kind: 'text', needle: 'c:\\media', field: 'any', negate: false },
]);
check('a colon inside a name is text', parseSearch('s02e04:1080p'), [
  { kind: 'text', needle: 's02e04:1080p', field: 'any', negate: false },
]);
check('a known prefix with nothing behind it is text', parseSearch('host:'), [
  { kind: 'text', needle: 'host:', field: 'any', negate: false },
]);
check('an operator with no number is text', parseSearch('>>>'), [
  { kind: 'text', needle: '>>>', field: 'any', negate: false },
]);
check('an unknown unit is text, not a guess at bytes', parseSearch('>3x'), [
  { kind: 'text', needle: '>3x', field: 'any', negate: false },
]);
check('a minus inside a word belongs to the word', parseSearch('s02e04-1080p'), [
  { kind: 'text', needle: 's02e04-1080p', field: 'any', negate: false },
]);
check('a lone minus is not a negation', parseSearch('-'), [
  { kind: 'text', needle: '-', field: 'any', negate: false },
]);
check(
  'a file name with a colon in it still finds its row',
  matchesSearch(task({ name: 'Doctor Who: The Movie.mkv' }), { text: 'who:', category: 'any' }),
  true,
);

// --- Quoting ----------------------------------------------------------------

check('quotes hold a phrase together', parseSearch('"big buck"'), [
  { kind: 'text', needle: 'big buck', field: 'any', negate: false },
]);
check('a quote can follow a prefix', parseSearch('paket:"my series"'), [
  { kind: 'text', needle: 'my series', field: 'package', negate: false },
]);
check(
  'an unclosed quote narrows as it is typed',
  parseSearch('"big bu'),
  [{ kind: 'text', needle: 'big bu', field: 'any', negate: false }],
);

// --- The picker still scopes bare words -------------------------------------

check('the category is where a bare word goes', parseSearch('bunny', 'host'), [
  { kind: 'text', needle: 'bunny', field: 'host', negate: false },
]);
check('an empty query matches everything', matchesSearch(task({ name: 'anything' }), { text: '   ', category: 'any' }), true);

if (problems.length > 0) {
  console.error(`${problems.length} search-query case(s) wrong:`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}
console.log('search query parser: all cases hold');
