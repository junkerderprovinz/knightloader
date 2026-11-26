// The Downloads and Collector lists narrow on the phone by the web's rules:
// the same quick filters with the same counts, the same search language, the
// same collector facets, and the same words for a link that did not make the
// list. The quick settings' two fields read and write their numbers as the
// web's fields do.
//
// The search table is the web's own: every case of web/check-search-query.mjs
// that asks matchesSearch is run here against the phone's parser, so the two
// cannot drift apart without this failing.
//
// src/api/narrowing.ts, src/api/notAdded.ts and src/components/limitForm.ts
// import nothing at run time, so node strips their types and these checks call
// the real functions.
//
// Run by hand and by CI, from mobile/: `node check-narrowing.mjs`
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const load = (...p) => import(pathToFileURL(join(here, 'src', ...p)).href);
const {
  COLLECTOR_FILTERS,
  DOWNLOAD_FILTERS,
  NOTHING_NARROWED,
  facetOptions,
  fileTypeOf,
  hostOf,
  isNarrowed,
  matchesFacets,
  matchesFilters,
  matchesSearch,
  narrow,
  offeredFilters,
  parseSearch,
} = await load('api', 'narrowing.ts');
const { heldOf, heldReason, originWord, skippedReason } = await load('api', 'notAdded.ts');
const { amountIn, bytesOf, countOf, stepCount, unitFor } = await load('components', 'limitForm.ts');

const problems = [];
const expect = (what, got, want) => {
  const g = JSON.stringify(got);
  const w = JSON.stringify(want);
  if (g !== w) problems.push(`${what}: got ${g}, want ${w}`);
};

const task = (over = {}) => ({
  id: over.id ?? 'x',
  url: 'https://host.example/file.bin',
  name: 'file.bin',
  package: 'Pack',
  resolver: 'direct',
  size: 0,
  loaded: 0,
  speed: 0,
  status: 'queued',
  createdAt: new Date().toISOString(),
  priority: 0,
  position: 0,
  enabled: true,
  ...over,
});

// The filters offered are the web's, in its order.
const web = readFileSync(join(here, '..', 'web', 'src', 'components', 'ListToolbar.tsx'), 'utf8');
const listOf = (name) =>
  [...(web.match(new RegExp(`export const ${name}: QuickFilterId\\[\\] = \\[([^\\]]*)\\]`))?.[1] ?? '').matchAll(/'(\w+)'/g)].map(
    (m) => m[1],
  );
expect('the download filters are the web list', DOWNLOAD_FILTERS, listOf('DOWNLOAD_FILTERS'));
expect('the collector filters are the web list', COLLECTOR_FILTERS, listOf('COLLECTOR_FILTERS'));

{
  const rows = [
    task({ id: 'run', status: 'running' }),
    task({ id: 'leech', status: 'running', resolver: 'torrent' }),
    task({ id: 'unpack', status: 'extracting' }),
    task({ id: 'repair', status: 'done', repair: { stage: 'repairing' } }),
    task({ id: 'done', status: 'done' }),
    task({ id: 'seed', status: 'done', seeding: true }),
    task({ id: 'fail', status: 'error' }),
    task({ id: 'off', enabled: false }),
    task({ id: 'stall', status: 'running', stalledSince: '2026-11-26T03:00:00Z' }),
    task({ id: 'zero', status: 'running', stalledSince: '0001-01-01T00:00:00Z' }),
  ];
  const ids = (f) => rows.filter((r) => matchesFilters(r, new Set(f))).map((r) => r.id);
  expect('downloading takes in unpacking and a par2 repair, not a torrent', ids(['running']), ['run', 'unpack', 'repair', 'stall', 'zero']);
  expect('a running torrent is leeching', ids(['leeching']), ['leech']);
  expect('finished leaves a seeding torrent out', ids(['finished']), ['repair', 'done']);
  expect('Go zero time is not a stall', ids(['stalled']), ['stall']);
  expect('two filters show both kinds', ids(['failed', 'disabled']), ['fail', 'off']);
  expect('no filter keeps every row', ids([]).length, rows.length);
  const offered = offeredFilters(DOWNLOAD_FILTERS, rows, new Set(['paused']));
  expect(
    'a filter is offered with its count, an empty one only while it is on',
    offered.map(({ id, n }) => `${id}:${n}`),
    ['running:5', 'leeching:1', 'queued:1', 'paused:0', 'finished:2', 'seeding:1', 'failed:1', 'disabled:1', 'stalled:1'],
  );
}

// Every parseSearch case of the web's own check, against this parser. The
// phone has no field picker, so the one case that sets one is left out.
{
  const src = readFileSync(join(here, '..', 'web', 'check-search-query.mjs'), 'utf8');
  const cases = [...src.matchAll(/parseSearch\('((?:[^'\\]|\\.)*)'\),\s*(\[[\s\S]*?\]),?\s*\);/g)];
  if (cases.length < 10) problems.push(`only ${cases.length} search cases could be read from web/check-search-query.mjs`);
  const DAY = 86_400_000;
  for (const [, query, want] of cases) {
    const text = query.replace(/\\(.)/g, '$1');
    const terms = new Function('DAY', `return ${want}`)(DAY);
    expect(`the web's case ${JSON.stringify(text)}`, parseSearch(text), terms);
  }
}

{
  const now = Date.parse('2026-11-26T12:00:00Z');
  const film = task({ name: 'Film.2026.mkv', size: 2e9, host: 'rapid.example', createdAt: '2026-11-20T12:00:00Z' });
  const sample = task({ name: 'film-sample.mkv', size: 3e7, url: 'https://www.files.example/s', createdAt: '2026-11-26T11:00:00Z' });
  const found = (q) => [film, sample].filter((t) => matchesSearch(t, parseSearch(q), now)).map((t) => t.name);
  expect('a word matches anywhere, without case', found('FILM'), ['Film.2026.mkv', 'film-sample.mkv']);
  expect('a minus leaves a word out', found('film -sample'), ['Film.2026.mkv']);
  expect('a size compares in decimal units', found('>1gb'), ['Film.2026.mkv']);
  expect('an age reads days', found('older:3d'), ['Film.2026.mkv']);
  expect('host: reads the server host, then the address', found('host:files'), ['film-sample.mkv']);
  expect('a half-typed prefix is text', found('host:'), []);
  expect('the www of an address is dropped', hostOf(sample), 'files.example');
}

{
  const rows = [
    task({ id: 'a', name: 'a.part1.rar', url: 'https://one.example/a', package: 'P1' }),
    task({ id: 'b', name: 'b.r01', url: 'https://two.example/b', package: 'P1' }),
    task({ id: 'c', name: 'c.mkv', url: 'https://one.example/c', package: 'P2' }),
    task({ id: 'd', name: 'd', url: 'https://one.example/d?x=.mp3', package: '' }),
  ];
  expect('file types by extension, a split archive tail included', rows.map(fileTypeOf), ['archive', 'archive', 'video', 'other']);
  const facets = (over) => ({ ...NOTHING_NARROWED.facets, ...over });
  const pick = (f) => rows.filter((r) => matchesFacets(r, facets(f))).map((r) => r.id);
  expect('two hosts mean either', pick({ host: new Set(['one.example', 'two.example']) }), ['a', 'b', 'c', 'd']);
  expect('a host and a type mean both', pick({ host: new Set(['one.example']), fileType: new Set(['archive']) }), ['a']);
  expect('the loose links are a package too', pick({ package: new Set(['']) }), ['d']);
  expect(
    'hosts are offered most common first, a chosen one kept at zero',
    facetOptions(rows, 'host', new Set(['gone.example'])),
    [
      { value: 'one.example', n: 3 },
      { value: 'two.example', n: 1 },
      { value: 'gone.example', n: 0 },
    ],
  );
  const n = { search: 'one', filters: new Set(), facets: facets({ fileType: new Set(['video']) }) };
  expect('narrow applies the search and the facets together', narrow(rows, n).map((r) => r.id), ['c']);
  expect('nothing set narrows nothing', narrow(rows, NOTHING_NARROWED) === rows, true);
  expect('a filter that hides nothing still counts as narrowing', isNarrowed({ ...NOTHING_NARROWED, filters: new Set(['online']) }), true);
  expect('blank search is no narrowing', isNarrowed({ ...NOTHING_NARROWED, search: '   ' }), false);
}

// The words for links that did not make the list, by the keys the web uses.
{
  const t = (key, vars) => (vars ? `${key}${JSON.stringify(vars)}` : key);
  const rule = task({ id: 'r', skipped: true, skipCode: 'filterRule', skipParams: { rule: 'no samples' }, matchedRules: ['no samples'] });
  const ban = task({ id: 'b', skipped: true, skipCode: 'bannedTracker', skipParams: { host: 't.example' } });
  const hist = task({
    id: 'h',
    skipped: true,
    skipCode: 'downloaded',
    skipParams: { name: 'x.mkv', finished: 'not a date' },
    createdAt: '2026-01-01T00:00:00Z',
  });
  const newer = task({ id: 'n', skipped: true, skipCode: 'fromTheFuture', skipReason: 'English words', matchedRules: ['r1', 'r2'] });
  expect('a rule names itself', heldReason(t, rule), 'no samples: collector.filtered.reason.filterRule{"rule":"no samples"}');
  expect('a hold naming no rule is the tracker ban', heldReason(t, ban).split(':')[0], 'settings.torrents.bannedTrackers');
  expect('the history holds as itself', heldReason(t, hist).split(':')[0], 'collector.filtered.byHistory');
  expect('an unknown code keeps the English', heldReason(t, newer), 'r1, r2: English words');
  expect('held links oldest first, staged ones left out', heldOf([newer, task(), hist]).map((x) => x.id), ['h', 'n']);
  expect('a known origin is worded', originWord(t, 'cnl'), 'collector.filtered.origin.cnl');
  expect('an unknown origin stays as sent', originWord(t, 'feed'), 'feed');
  expect(
    'a folded duplicate names what recognised it',
    skippedReason(t, { url: 'u', kind: 'duplicate', reason: 'r', code: 'sameFile', params: { name: 'n', signal: 'name+size', by: 'filter' }, at: '' }),
    'skipped.reason.sameFile{"name":"n","signal":"skipped.signal.nameSize","by":"skipped.by.filter"}',
  );
  expect(
    'a playlist count is a number',
    skippedReason(t, { url: 'u', kind: 'playlist', reason: 'r', code: 'playlistCapped', params: { total: '9', kept: '5', left: '4' }, at: '' }),
    'skipped.reason.playlistCapped{"total":9,"kept":5,"left":4}',
  );
  expect('a kind quotes the tool', skippedReason(t, { url: 'u', kind: 'nzb', reason: 'no server', at: '' }), 'skipped.reason.nzb{"detail":"no server"}');
  expect('an unknown kind keeps the English', skippedReason(t, { url: 'u', kind: 'new', reason: 'English', at: '' }), 'English');

  // Every key the words use exists in the app's catalogue.
  const en = readFileSync(join(here, 'src', 'i18n', 'en.ts'), 'utf8');
  const src = readFileSync(join(here, 'src', 'api', 'notAdded.ts'), 'utf8');
  for (const [, key] of src.matchAll(/'((?:collector|skipped|settings)\.[\w.]+)'/g)) {
    if (!en.includes(`'${key}':`)) problems.push(`notAdded.ts uses ${key}, which en.ts does not have`);
  }
}

// The quick settings' fields, by the web's rules and the settings' bounds.
{
  expect('nothing is shown in kB/s', unitFor(0).label, 'kB/s');
  expect('a limit is shown in the largest unit it fills', unitFor(2_500_000).label, 'MB/s');
  expect('two decimals at most', amountIn(1_234_567, unitFor(1_234_567)), '1.23');
  expect('a decimal comma is read', bytesOf('1,5', unitFor(2e6)), 1_500_000);
  expect('a bare amount counts in the unit on show', bytesOf('750', unitFor(1)), 750_000);
  expect('a word is no amount', bytesOf('fast', unitFor(1)), null);
  expect('a negative is no amount', bytesOf('-1', unitFor(1)), null);
  expect('the count stops at the top', stepCount(64, 1), 64);
  expect('the count stops at one', stepCount(1, -1), 1);
  expect('a typed count is held to the bounds', countOf('100'), 64);
  expect('a count is a whole number', countOf('2.5'), null);

  // The bounds are the settings document's, which the server clamps to.
  const go = readFileSync(join(here, '..', 'internal', 'settings', 'settings_queue.go'), 'utf8');
  const ceiling = Number(go.match(/if n\.MaxConcurrent > (\d+)/)?.[1]);
  expect("the top is the server's", countOf(String(ceiling + 1)), ceiling);
}

if (problems.length) {
  console.error(`${problems.length} problem(s):\n  ${problems.join('\n  ')}`);
  process.exit(1);
}
console.log('ok: the lists narrow and the limits read as on the web');
