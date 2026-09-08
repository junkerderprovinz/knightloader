// The settings search index and the settings pages have to name the same
// strings.
//
// web/src/pages/settings/searchIndex.ts is a DECLARED index: 24 pages, their
// cards, and the caption plus (i) text of every row on them, written down as
// translation keys. Declared rather than derived, and the reasons are the four
// this script's own FILE_PAGES table exists to survive:
//
//   - a settings page reaches the catalogue through four helper names, not one
//     (t, tx, cx, rx). A scanner that only knew `t(` would miss six of the
//     twenty-four pages outright and they would still LOOK indexed, because
//     their page name goes on matching.
//   - a page's keys are not necessarily in that page's file. settings.rules.*
//     mostly lives in components/RuleEditor.tsx, and the Downloads page is
//     eight files.
//   - one file draws two rail entries. Look.tsx renders both /settings/look and
//     /settings/appearance, split by `{appearance && (` / `{general && (`
//     guards.
//   - a key prefix does not name its page. The Downloads page carries
//     settings.downloads.*, settings.stall.*, settings.crawl.*,
//     settings.feeds.* and 64 bare settings.<one segment> keys, and the same
//     prefix also holds toasts and refusals that are not rows at all.
//
// So the index is written by hand and this is what keeps it honest. Without it
// the drift is invisible in the worst way: a row added to a page is simply
// never found by the search, and a missing search result looks exactly like a
// word nobody typed.
//
// WHAT IT CHECKS, and the forward/reverse pair deliberately at PAGE scope
// rather than card scope:
//
//   coverage  every .tsx under src/pages/settings is either mapped to a page in
//             FILE_PAGES or written off in NOT_PAGE_SOURCES with a reason. A new
//             settings file nobody mapped would draw cards and rows this script
//             never looks at, which is the same silence one folder further out.
//   forward   every label/hint/SectionTitle key the settings sources hand to the
//             catalogue appears somewhere under one of that file's pages in the
//             index, or on EXCLUDED with a reason.
//   reverse   every key in the index exists in en.ts (see `in en` below - two
//             keys already crash the page without it) AND still appears
//             literally in one of that page's own sources, so a renamed or
//             deleted key fails here instead of becoming a result that jumps to
//             a row that is not there any more.
//   expiry    an EXCLUDED entry that names a key it is WAITING ON stops being
//             allowed the moment that key reaches en.ts. An exclusion that
//             outlives its own reason is a gap again, just a documented one.
//
// WHICH card a row belongs to stays an editorial decision, made by reading the
// page. Pinning that mechanically would mean a brace-matching JSX parser, and
// it would be wrong anyway for the pages whose cards come from another file.
// Page scope is the part that can drift silently; card placement is the part
// somebody notices the first time they use the search.
//
// Run by CI and by hand: `node web/check-settings-search.mjs`.
// `node web/check-settings-search.mjs --dump` prints what it found, per page,
// per card, which is how the index was filled in the first place.

import { readdirSync, readFileSync, statSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const src = (rel) => readFileSync(join(here, rel), 'utf8');

/**
 * Which file draws which page, written down rather than guessed.
 *
 * `pages` is a list because two of these genuinely serve two rail entries
 * (Look.tsx) or are rendered from a second page (Help.tsx exports the About
 * card, which the General tab draws at its foot). A key found in such a file
 * satisfies the forward check under EITHER of its pages - the guard that
 * decides which one is a JSX conditional, and a regex that claimed to read it
 * would be a guess wearing a check's clothes.
 *
 * `titleTags` names the wrapper components in that file whose `title` prop
 * becomes a SectionTitle further down (Help.tsx's Topic, Modules.tsx's Group).
 * Without it those two pages would parse to zero card titles and this script
 * would never notice a topic being added or renamed.
 */
const FILE_PAGES = [
  { file: 'src/pages/settings/Modules.tsx', pages: ['modules'], titleTags: ['Group'] },
  { file: 'src/pages/settings/DownloadsSettings.tsx', pages: ['downloads'] },
  // A DIRECTORY, not eight filenames. Listing the files by hand would itself be
  // a drift vector of exactly the kind this script exists to close: the ninth
  // card someone adds beside them would simply not be scanned, and its rows
  // would go unindexed with nothing to say so.
  { file: 'src/pages/settings/downloads/', pages: ['downloads'] },
  { file: 'src/pages/settings/Archives.tsx', pages: ['archives'] },
  // One component, two rail entries, split by `{appearance && (` / `{general && (`.
  { file: 'src/pages/settings/Look.tsx', pages: ['look', 'appearance'] },
  // The cards that have moved out of that file into their own, all of them on
  // the General tab so far. A directory for the same reason downloads/ is one.
  { file: 'src/pages/settings/look/', pages: ['look'] },
  // The Help page's topics, plus the About card the General tab draws at its foot.
  { file: 'src/pages/settings/Help.tsx', pages: ['help', 'look'], titleTags: ['Topic'] },
  { file: 'src/pages/settings/Accounts.tsx', pages: ['accounts'] },
  { file: 'src/pages/settings/Instances.tsx', pages: ['instances'] },
  { file: 'src/pages/settings/Access.tsx', pages: ['access'] },
  { file: 'src/pages/settings/Advanced.tsx', pages: ['advanced'] },
  { file: 'src/pages/settings/Rules.tsx', pages: ['rules'] },
  // 141 settings.rules.* keys, most of them here rather than in Rules.tsx.
  { file: 'src/components/RuleEditor.tsx', pages: ['rules'] },
  { file: 'src/pages/settings/Categories.tsx', pages: ['categories'] },
  { file: 'src/pages/settings/Connections.tsx', pages: ['connections'] },
  { file: 'src/pages/settings/Reconnect.tsx', pages: ['reconnect'] },
  { file: 'src/pages/settings/Resolvers.tsx', pages: ['resolvers'] },
  // A directory for the same reason downloads/ and look/ are: the cookie jars
  // card moved out of Resolvers.tsx into one of its own, and the next card to
  // follow it must be scanned without anybody remembering to come back here.
  { file: 'src/pages/settings/resolvers/', pages: ['resolvers'] },
  { file: 'src/pages/settings/Torrents.tsx', pages: ['torrents'] },
  { file: 'src/pages/settings/Captcha.tsx', pages: ['captcha'] },
  { file: 'src/pages/settings/Schedule.tsx', pages: ['schedule'] },
  // The health page is one card per file by construction (which is what makes
  // "at most one SectionTitle per Card" structural there rather than
  // remembered), so the shell and the folder are both mapped - the folder as a
  // DIRECTORY, for the same reason downloads/ and diagnostics/ are one.
  { file: 'src/pages/settings/Health.tsx', pages: ['health'] },
  { file: 'src/pages/settings/health/', pages: ['health'] },
  { file: 'src/pages/settings/Diagnostics.tsx', pages: ['diagnostics'] },
  // A directory for the same reason downloads/, look/, resolvers/ and
  // shortcuts/ are: the database-maintenance card moved out into one of its
  // own, and the next card to follow it must be scanned without anybody
  // remembering to come back here.
  { file: 'src/pages/settings/diagnostics/', pages: ['diagnostics'] },
  { file: 'src/pages/settings/BrowserTools.tsx', pages: ['browsertools'] },
  { file: 'src/pages/settings/Scripts.tsx', pages: ['scripts'] },
  { file: 'src/pages/settings/EventTargets.tsx', pages: ['eventtargets'] },
  // A DIRECTORY, for the same reason downloads/, look/, resolvers/,
  // diagnostics/ and shortcuts/ are: the row editor, the events picker, the
  // status block and the test panel are four files today, and the fifth card
  // somebody adds beside them must be scanned without anybody remembering to
  // come back here.
  { file: 'src/pages/settings/eventtargets/', pages: ['eventtargets'] },
  { file: 'src/pages/settings/Shortcuts.tsx', pages: ['shortcuts'] },
  { file: 'src/pages/settings/shortcuts/', pages: ['shortcuts'] },
];

/**
 * Keys the sources hand to the catalogue that are deliberately NOT in the
 * index, each with the reason written down - so "not indexed" is a decision
 * somebody made rather than a gap nobody noticed.
 */
const EXCLUDED = new Map([
  // ---- absent from en.ts, which is worse than not being indexed ----
  // lib/i18n.tsx resolves `dict[key] ?? en[key]` with no final fallback, so
  // this one returns undefined despite its `string` type, and it is drawn as a
  // real card title today. Add it to en.ts (and therefore to all 42 locales,
  // which the Type check step enforces) and move it into the index - the
  // Schedule status banner is unfindable until somebody does.
  //
  // An entry may name a key it is WAITING ON. The moment that key reaches en.ts
  // the exclusion fails, saying so - because at that point the reason for it is
  // gone, and an exclusion that outlives its reason is the silence this whole
  // script exists to prevent.
  //
  // The Diagnostics system card used to sit here beside it, together with the
  // six readings inside it. settings.diagnostics.systemTitle reached en.ts with
  // this wave, so both exclusions expired exactly as designed and came out: the
  // card is indexed by its own title now. Its two remaining strays,
  // settings.diagnostics.speedSamples and .speedSamplesHint, went nowhere -
  // they exist in all 42 locales and NO source draws them, so putting them in
  // the index would fail the reverse check instead. Dropping them from here was
  // the whole fix.
  [
    'settings.schedule.statusTitle',
    { waitingOn: 'settings.schedule.statusTitle', reason: 'NOT IN en.ts, so it resolves to undefined. Drawn as the Schedule status banner title' },
  ],
  // The notifications card was excluded here while it was being built, with its
  // own keys named as the condition. They landed in the same wave, this check
  // said so on the next run, and the exclusion came out: the card is indexed
  // like every other one. That is the whole point of a `waitingOn` rather than
  // a comment - a promise nobody has to remember to keep.

  // ---- not a row, and not a card either ----
  [
    'common.loading',
    'the whole-page LoadingCard four settings pages show while their own fetch is in flight. Page furniture, on no card at all',
  ],
  ['settings.rules.testRunning', "the same, for the Rules page's dry run"],
  [
    'settings.modules.off',
    'the badge on the feeds card that reads, in full, "Off". As a search result it would be the word Off pointing at a card - findable is not the same as useful',
  ],
  [
    'settings.system.shuttingDownTitle',
    'the card that REPLACES the lifecycle card while the server is restarting. A result for it would lead somewhere that only exists during a shutdown',
  ],

  // ---- a second search box, over a different question ----
  [
    'settings.advanced.search',
    'the raw-key search box on the Advanced page. It searches dotted paths and JSON values over the settings document, which is a different question from this one, and offering it as a row would suggest the two boxes do the same thing',
  ],
  ['settings.advanced.filterAll', "part of the raw-key table's own machinery, above"],
  ['settings.advanced.filterChanged', "part of the raw-key table's own machinery, above"],
]);

// ---------------------------------------------------------------------------
// Reading a JSX attribute without pretending to be a parser.
//
// `hint={`${t('a')} ${t('b')}`}` is a real call site (DownloadsSettings.tsx),
// and so is a label whose value spans four lines. A regex that stopped at the
// first `}` would read half of either. So: find the attribute, walk forward
// counting braces, and pull every catalogue call out of the balanced span.
// ---------------------------------------------------------------------------

/** Every `t('x')` / `tx('x')` / `cx('x')` / `rx('x')` key inside a span of source. */
function keysIn(span) {
  return [...span.matchAll(/\b(?:t|tx|cx|rx)\('([^']+)'/g)].map((m) => m[1]);
}

/** The index just past the `{` that opens an attribute value, to its match. */
function balanced(text, openBrace) {
  let depth = 0;
  for (let i = openBrace; i < text.length; i++) {
    if (text[i] === '{') depth++;
    else if (text[i] === '}') {
      depth--;
      if (depth === 0) return i;
    }
  }
  return text.length;
}

/**
 * Every `<attr>={...}` value in a file, as {at, keys}.
 *
 * The leading `(?<![\w-])` is load-bearing: without it `aria-label={t('x')}`
 * matches `label=` and a button's accessible name lands in the index as a row
 * caption that no row has.
 */
function attrSpans(text, attr) {
  const out = [];
  const re = new RegExp(`(?<![\\w-])${attr}=\\{`, 'g');
  for (const m of text.matchAll(re)) {
    const open = m.index + m[0].length - 1;
    const close = balanced(text, open);
    out.push({ at: m.index, span: text.slice(open, close + 1), tag: owningTag(text, m.index) });
  }
  return out;
}

/**
 * The component this attribute is written on - the nearest `<Capitalised` above
 * it. Reported by --dump only, and only as a hint to whoever fills the index:
 * `label` on a `Field` is a row you can jump to, `label` on a `LabelBadge` or a
 * `LoadingCard` is not, and nothing but reading the call site tells them apart.
 * The check itself never uses this, because a heuristic that walks backwards
 * through JSX is exactly the kind of almost-parser this file refuses to be.
 */
function owningTag(text, at) {
  const before = text.slice(Math.max(0, at - 2000), at);
  const m = [...before.matchAll(/<([A-Z]\w*)/g)].pop();
  return m ? m[1] : '?';
}

/** Every `<SectionTitle …>children</SectionTitle>` as {at, key} for the child key. */
function sectionTitles(text) {
  const out = [];
  for (const m of text.matchAll(/<SectionTitle\b/g)) {
    // Past the opening tag, respecting attribute braces, to the `>` that ends it.
    let i = m.index + '<SectionTitle'.length;
    while (i < text.length && text[i] !== '>') {
      if (text[i] === '{') i = balanced(text, i);
      i++;
    }
    const end = text.indexOf('</SectionTitle>', i);
    const children = text.slice(i + 1, end === -1 ? i + 1 : end);
    const keys = keysIn(children);
    // A dynamic title (Help.tsx's `{title}`, Shortcuts.tsx's `{groupLabel(…)}`)
    // has no key here. Its file names the wrapper in `titleTags` instead.
    if (keys.length > 0) out.push({ at: m.index, key: keys[0] });
  }
  return out;
}

/** `<Topic … title={t('k')}` and friends - a card title one component removed. */
function wrapperTitles(text, tags) {
  const out = [];
  for (const tag of tags ?? []) {
    const re = new RegExp(`<${tag}\\b`, 'g');
    for (const m of text.matchAll(re)) {
      const head = text.slice(m.index, m.index + 400);
      const title = /(?<![\w-])title=\{\s*(?:t|tx|cx|rx)\('([^']+)'/.exec(head);
      if (title) out.push({ at: m.index, key: title[1] });
    }
  }
  return out;
}

/** Everything one source file says, in source order, bucketed by card. */
function scanFile(entry) {
  const text = src(entry.file);
  const titles = [...sectionTitles(text), ...wrapperTitles(text, entry.titleTags)].sort((a, b) => a.at - b.at);
  const rows = [];
  for (const attr of ['label', 'hint']) {
    for (const { at, span, tag } of attrSpans(text, attr)) {
      for (const key of keysIn(span)) rows.push({ at, key, kind: attr, tag });
    }
  }
  rows.sort((a, b) => a.at - b.at);

  // A row belongs to the last card title above it. Rows above the first title
  // in the file belong to no card yet - a modal, a shared sub-component - and
  // are reported under an empty card so they are placed by hand rather than
  // filed under whichever card happened to be first.
  const cards = [{ title: null, rows: [] }];
  let ti = 0;
  for (const row of rows) {
    while (ti < titles.length && titles[ti].at < row.at) {
      cards.push({ title: titles[ti].key, rows: [] });
      ti++;
    }
    cards[cards.length - 1].rows.push(row);
  }
  while (ti < titles.length) {
    cards.push({ title: titles[ti].key, rows: [] });
    ti++;
  }
  return { file: entry.file, pages: entry.pages, titles, cards };
}

/**
 * Files under src/pages/settings that are not a page's markup, each with the
 * reason it is not in FILE_PAGES. Anything else there has to be mapped, or the
 * check below fails: a new settings file nobody added to the table would draw
 * cards and rows that this script never looks at, which is precisely the silence
 * it exists to break.
 */
const NOT_PAGE_SOURCES = new Map([
  ['registry.tsx', 'the id-to-component map. check-settings-pages.mjs is what reads it'],
  ['context.tsx', 'the draft/feature provider - no catalogue text of its own'],
  ['controls.tsx', 'NeutralSwitch, a control. Its labels come from its callers'],
  ['Empty.tsx', 'the registered-but-not-built placeholder, which is not a card on any page'],
  ['Appearance.tsx', 'three lines: it renders <Look section="appearance" />, and Look.tsx is mapped'],
  ['SettingsSearch.tsx', 'the search box itself. It sits above the pages rather than on one, and indexing it would make it a result in its own list'],
]);

/** A table entry ending in `/` is a directory: every .tsx in it, in name order. */
function expand(entry) {
  if (!entry.file.endsWith('/')) return [entry];
  return readdirSync(join(here, entry.file))
    .filter((f) => f.endsWith('.tsx'))
    .sort()
    .map((f) => ({ ...entry, file: entry.file + f }));
}

const SOURCES = FILE_PAGES.flatMap(expand);

/** Every .tsx anywhere under src/pages/settings, so nothing can hide in a new folder. */
function everySettingsSource(rel = 'src/pages/settings') {
  const out = [];
  for (const name of readdirSync(join(here, rel))) {
    const child = `${rel}/${name}`;
    if (statSync(join(here, child)).isDirectory()) out.push(...everySettingsSource(child));
    else if (name.endsWith('.tsx')) out.push(child);
  }
  return out;
}

const mapped = new Set(SOURCES.map((e) => e.file));
const unmapped = everySettingsSource().filter(
  (f) => !mapped.has(f) && !NOT_PAGE_SOURCES.has(f.slice(f.lastIndexOf('/') + 1)),
);
if (unmapped.length > 0) {
  console.error(`${unmapped.length} settings source(s) no page is mapped to:`);
  for (const f of unmapped) {
    console.error(`  ${f} - add it to FILE_PAGES, or to NOT_PAGE_SOURCES with a reason`);
  }
  process.exit(1);
}

const scans = SOURCES.map(scanFile);

if (process.argv.includes('--dump')) {
  for (const s of scans) {
    console.log(`\n=== ${s.file}  ->  ${s.pages.join(' | ')}`);
    for (const card of s.cards) {
      if (card.title === null && card.rows.length === 0) continue;
      console.log(`  # ${card.title ?? '(no card yet)'}`);
      const seen = new Set();
      for (const r of card.rows) {
        if (seen.has(r.key)) continue;
        seen.add(r.key);
        console.log(`      ${r.kind.padEnd(5)} ${`<${r.tag}`.padEnd(16)} ${r.key}`);
      }
    }
  }
  process.exit(0);
}

// ---------------------------------------------------------------------------
// The index, read as text. Importing it would mean compiling TypeScript in a
// check that has to stay a plain `node` invocation - the same reason
// check-settings-pages.mjs reads its two lists with a regex. searchIndex.ts is
// written to be readable this way: type-only imports, one flat object literal,
// every value a quoted key.
// ---------------------------------------------------------------------------

/** page id -> Set of every key declared anywhere under it. */
function indexedKeys() {
  const text = src('src/pages/settings/searchIndex.ts');
  const start = text.indexOf('SETTINGS_INDEX');
  if (start === -1) throw new Error('SETTINGS_INDEX not found in searchIndex.ts');
  const body = text.slice(text.indexOf('{', start));
  const byPage = new Map();
  // Each page is `  <id>: [` at two spaces of indent; everything until the next
  // one belongs to it. Deeper indentation is a card or a row inside that page.
  const heads = [...body.matchAll(/^ {2}([a-z][a-zA-Z]*): \[/gm)];
  for (let i = 0; i < heads.length; i++) {
    const from = heads[i].index;
    const to = i + 1 < heads.length ? heads[i + 1].index : body.length;
    byPage.set(heads[i][1], new Set(keysInQuotes(body.slice(from, to))));
  }
  return byPage;
}

/** Every `'settings.…'`-shaped string literal in a span. */
function keysInQuotes(span) {
  return [...span.matchAll(/'([a-z][a-zA-Z0-9]*(?:\.[a-zA-Z0-9]+)+)'/g)].map((m) => m[1]);
}

/** Every key en.ts actually has. */
function englishKeys() {
  const text = src('src/lib/locales/en.ts');
  return new Set([...text.matchAll(/^ {2}'([^']+)':/gm)].map((m) => m[1]));
}

const index = indexedKeys();
const en = englishKeys();

// A fixture that reads nothing is a check that passes for the wrong reason -
// the same guard check-settings-pages.mjs makes, for the same failure: a
// parser that has quietly stopped matching reports a clean run.
const titleCount = scans.reduce((n, s) => n + s.titles.length, 0);
const rowCount = new Set(scans.flatMap((s) => s.cards.flatMap((c) => c.rows.map((r) => r.key)))).size;
if (titleCount < 40) throw new Error(`only ${titleCount} card titles parsed out of the settings sources - the parser is wrong, not the code`);
if (rowCount < 150) throw new Error(`only ${rowCount} row keys parsed out of the settings sources - the parser is wrong, not the code`);
if (en.size < 500) throw new Error(`only ${en.size} keys parsed out of en.ts - the parser is wrong, not the code`);
if (index.size < 15) throw new Error(`only ${index.size} pages parsed out of searchIndex.ts - the parser is wrong, not the code`);

const problems = [];

// An exclusion that names a key it is waiting on is a note to whoever lands that
// key, and it has to stop being silent the moment they do.
for (const [key, note] of EXCLUDED) {
  if (typeof note === 'object' && note.waitingOn && en.has(note.waitingOn)) {
    problems.push(
      `${key} is excluded from the search index only until ${note.waitingOn} reaches en.ts - it is there now, so index it and drop the exclusion`,
    );
  }
}

// Forward: everything the pages say is findable, or written off on purpose.
for (const s of scans) {
  const seen = new Set();
  const all = [...s.titles.map((t) => t.key), ...s.cards.flatMap((c) => c.rows.map((r) => r.key))];
  for (const key of all) {
    if (seen.has(key)) continue;
    seen.add(key);
    if (EXCLUDED.has(key)) continue;
    if (s.pages.some((p) => index.get(p)?.has(key))) continue;
    problems.push(
      `${key} is drawn by ${s.file} but is not in the search index under ${s.pages.join(' or ')} - the settings search cannot find it`,
    );
  }
}

// Reverse: nothing in the index is a key the catalogue lost, or a key the page
// stopped drawing.
const sourceOf = new Map();
for (const s of scans) {
  for (const page of s.pages) {
    if (!sourceOf.has(page)) sourceOf.set(page, '');
    sourceOf.set(page, sourceOf.get(page) + src(s.file));
  }
}
for (const [page, keys] of index) {
  for (const key of keys) {
    // T2, and it is not hypothetical: lib/i18n.tsx resolves `dict[key] ?? en[key]`
    // with no final fallback, so a key absent from en returns undefined despite
    // the `string` type and the first .toLowerCase() in the matcher blanks the
    // whole settings page.
    if (!en.has(key)) {
      problems.push(`${key} is in the search index under ${page} but not in en.ts - it resolves to undefined and blanks the page`);
      continue;
    }
    const text = sourceOf.get(page);
    if (text === undefined) {
      problems.push(`${page} is in the search index but no source file is mapped to it - add it to FILE_PAGES`);
      break;
    }
    if (!text.includes(`'${key}'`)) {
      problems.push(`${key} is in the search index under ${page} but no source of that page mentions it any more - a result that jumps to a row that is not there`);
    }
  }
}

if (problems.length > 0) {
  console.error(`${problems.length} settings search index problem(s):`);
  for (const p of problems) console.error(`  ${p}`);
  console.error('\nRun `node web/check-settings-search.mjs --dump` to see what the pages actually draw.');
  process.exit(1);
}
console.log(`ok: ${index.size} pages, ${titleCount} card titles, ${rowCount} row keys, index and settings pages agree`);
