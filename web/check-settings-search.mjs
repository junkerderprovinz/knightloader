// Checks that the settings search index (src/pages/settings/searchIndex.ts)
// and the settings pages name the same strings, so a new row is never missing
// from the search.
//
// The index is written by hand because it cannot be derived: pages reach the
// catalogue through t, tx, cx and rx; a page's keys may live in other files
// (RuleEditor.tsx, a page's folder of cards); Look.tsx draws two rail
// entries; and a key prefix does not name its page.
//
// Checks, at page scope:
//   coverage  every .tsx under src/pages/settings is in FILE_PAGES or in
//             NOT_PAGE_SOURCES with a reason.
//   forward   every label, hint and SectionTitle key a page draws is in the
//             index under that page, or in EXCLUDED with a reason. A
//             `<ModuleToggle id="x">` draws settings.module.x as its label.
//   reverse   every indexed key exists in en.ts and still appears in one of
//             its page's sources.
//   expiry    an EXCLUDED entry waiting on a key fails once that key is in en.ts.
// Which card a row belongs to is left to whoever edits the index.
//
// Run: `node web/check-settings-search.mjs`; `--dump` prints what each page
// draws, per card.

import { readdirSync, readFileSync, statSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const src = (rel) => readFileSync(join(here, rel), 'utf8');

/**
 * Which file draws which page. A file may serve two pages (Look.tsx, or
 * Help.tsx whose About card the General tab draws), and its keys count for
 * either. `titleTags` names wrapper components whose `title` prop becomes a
 * SectionTitle (Help.tsx's Topic, Modules.tsx's Group).
 *
 * An entry ending in `/` is a directory, so a card added to it is scanned
 * without editing this table.
 */
const FILE_PAGES = [
  { file: 'src/pages/settings/Modules.tsx', pages: ['modules'], titleTags: ['Group'] },
  { file: 'src/pages/settings/CollectorSettings.tsx', pages: ['collector'] },
  { file: 'src/pages/settings/collector/', pages: ['collector'] },
  { file: 'src/pages/settings/DownloadsSettings.tsx', pages: ['downloads'] },
  { file: 'src/pages/settings/downloads/', pages: ['downloads'] },
  { file: 'src/pages/settings/Archives.tsx', pages: ['archives'] },
  // One component, two rail entries, split by `{appearance && (` / `{general && (`.
  { file: 'src/pages/settings/Look.tsx', pages: ['look', 'appearance'] },
  { file: 'src/pages/settings/look/', pages: ['look'] },
  // The Help page's topics, plus the About card the General tab draws at its foot.
  { file: 'src/pages/settings/Help.tsx', pages: ['help', 'look'], titleTags: ['Topic'] },
  { file: 'src/pages/settings/Accounts.tsx', pages: ['accounts'] },
  { file: 'src/pages/settings/Instances.tsx', pages: ['instances'] },
  { file: 'src/pages/settings/Access.tsx', pages: ['access'] },
  { file: 'src/pages/settings/access/', pages: ['access'] },
  { file: 'src/pages/settings/Advanced.tsx', pages: ['advanced'] },
  { file: 'src/pages/settings/Rules.tsx', pages: ['rules'] },
  // Most settings.rules.* keys live here rather than in Rules.tsx.
  { file: 'src/components/RuleEditor.tsx', pages: ['rules'] },
  // The categories card, drawn at the foot of the rules page.
  { file: 'src/pages/settings/Categories.tsx', pages: ['rules'] },
  { file: 'src/pages/settings/Network.tsx', pages: ['network'] },
  { file: 'src/pages/settings/network/', pages: ['network'] },
  { file: 'src/pages/settings/Connections.tsx', pages: ['network'] },
  { file: 'src/pages/settings/Reconnect.tsx', pages: ['network'] },
  { file: 'src/pages/settings/Resolvers.tsx', pages: ['resolvers'] },
  { file: 'src/pages/settings/resolvers/', pages: ['resolvers'] },
  { file: 'src/pages/settings/Torrents.tsx', pages: ['torrents'] },
  { file: 'src/pages/settings/Captcha.tsx', pages: ['captcha'] },
  { file: 'src/pages/settings/Automation.tsx', pages: ['automation'] },
  { file: 'src/pages/settings/automation/', pages: ['automation'] },
  { file: 'src/pages/settings/Schedule.tsx', pages: ['automation'] },
  { file: 'src/pages/settings/EventTargets.tsx', pages: ['automation'] },
  { file: 'src/pages/settings/eventtargets/', pages: ['automation'] },
  { file: 'src/pages/settings/EventPrograms.tsx', pages: ['automation'] },
  { file: 'src/pages/settings/eventprograms/', pages: ['automation'] },
  { file: 'src/pages/settings/Scripts.tsx', pages: ['automation'] },
  { file: 'src/pages/settings/Health.tsx', pages: ['health'] },
  { file: 'src/pages/settings/health/', pages: ['health'] },
  { file: 'src/pages/settings/Diagnostics.tsx', pages: ['diagnostics'] },
  { file: 'src/pages/settings/diagnostics/', pages: ['diagnostics'] },
  { file: 'src/pages/settings/BrowserTools.tsx', pages: ['browsertools'] },
  { file: 'src/pages/settings/Shortcuts.tsx', pages: ['shortcuts'] },
  { file: 'src/pages/settings/shortcuts/', pages: ['shortcuts'] },
];

/**
 * Keys the pages draw that are left out of the index on purpose, each with its
 * reason.
 */
const EXCLUDED = new Map([
  // Not a row, and not a card either.
  [
    'common.loading',
    'the whole-page LoadingCard four settings pages show while their own fetch is in flight. Page furniture, on no card at all',
  ],
  ['settings.rules.testRunning', "the same, for the Rules page's dry run"],
  [
    'settings.system.shuttingDownTitle',
    'the card that replaces the lifecycle card while the server is restarting. A result for it would lead somewhere that only exists during a shutdown',
  ],
  [
    'auth.twoFactor.qrLabel',
    'the accessible name of the QR code image inside the second factor enrolment. It reaches the catalogue through QRCode\'s `label` prop, which is an alt text rather than a caption; the card it belongs to is indexed by its own title, and it only exists while somebody is halfway through an enrolment',
  ],

  // A second search box, over a different question.
  [
    'settings.advanced.search',
    'the raw-key search box on the Advanced page. It searches dotted paths and JSON values over the settings document, which is a different question from this one, and offering it as a row would suggest the two boxes do the same thing',
  ],
  ['settings.advanced.filterAll', "part of the raw-key table's own machinery, above"],
  ['settings.advanced.filterChanged', "part of the raw-key table's own machinery, above"],

  // An easter egg.
  [
    'settings.rainbowDisco',
    'the disco switch, which appears only once somebody has found it (docs/easter-eggs.md). A search result would give it away, and would point at a row that is usually not there',
  ],
]);

// Attribute values can nest braces (hint={`${t('a')} ${t('b')}`}) or span
// lines, so each value is read to its balanced closing brace and every
// catalogue call inside it is collected.

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
 * Every `<attr>={...}` value in a file, as {at, keys}. The `(?<![\w-])` keeps
 * `aria-label=` from matching `label=`.
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
 * The component this attribute is written on, the nearest `<Capitalised`
 * above it. Only --dump shows it, as a hint: `label` on a Field is a row,
 * `label` on a LoadingCard is not. The check itself does not rely on it.
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

/**
 * Every `<ModuleToggle id="x"` as {at, key}. The switch draws the module's
 * name, settings.module.x, as its label without the page ever naming the key.
 */
function moduleToggles(text) {
  return [...text.matchAll(/<ModuleToggle\s[^>]*?(?<![\w-])id="([a-z]+)"/g)].map((m) => ({
    at: m.index,
    key: `settings.module.${m[1]}`,
  }));
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
  for (const { at, key } of moduleToggles(text)) rows.push({ at, key, kind: 'label', tag: 'ModuleToggle' });
  rows.sort((a, b) => a.at - b.at);

  // A row belongs to the last card title above it. Rows before the first title
  // (a modal, a shared sub-component) go under an empty card to be placed by
  // hand.
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

/** Files under src/pages/settings that are not a page's markup, with the
 *  reason. Every other file there must be in FILE_PAGES. */
const NOT_PAGE_SOURCES = new Map([
  ['registry.tsx', 'the id-to-component map. check-settings-pages.mjs is what reads it'],
  ['context.tsx', 'the draft/feature provider - no catalogue text of its own'],
  ['controls.tsx', 'NeutralSwitch, a control. Its labels come from its callers'],
  ['ModuleToggle.tsx', "a module's switch and the badges between it and the Modules page. The switch's label is read where a page draws it, from `<ModuleToggle id=\"x\"`"],
  ['pageIcons.tsx', 'the glyph of each page in the rail, no catalogue text'],
  ['Empty.tsx', 'the registered-but-not-built placeholder, which is not a card on any page'],
  ['Appearance.tsx', 'three lines: it renders <Look section="appearance" />, and Look.tsx is mapped'],
  ['SettingsSearch.tsx', 'the search box itself. It sits above the pages rather than on one, and indexing it would make it a result in its own list'],
]);

// A test beside a page renders it but is not one, and its strings are fixtures.
const isSource = (name) => name.endsWith('.tsx') && !name.endsWith('.test.tsx');

/** A table entry ending in `/` is a directory: every .tsx in it, in name order. */
function expand(entry) {
  if (!entry.file.endsWith('/')) return [entry];
  return readdirSync(join(here, entry.file))
    .filter(isSource)
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
    else if (isSource(name)) out.push(child);
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

// The index is read as text so the check runs with plain `node`. searchIndex.ts
// keeps to that shape: type-only imports, one object literal, quoted keys.

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

// A parser that has stopped matching would report a clean run.
const titleCount = scans.reduce((n, s) => n + s.titles.length, 0);
const rowCount = new Set(scans.flatMap((s) => s.cards.flatMap((c) => c.rows.map((r) => r.key)))).size;
if (titleCount < 40) throw new Error(`only ${titleCount} card titles parsed out of the settings sources - the parser is wrong, not the code`);
if (rowCount < 150) throw new Error(`only ${rowCount} row keys parsed out of the settings sources - the parser is wrong, not the code`);
if (en.size < 500) throw new Error(`only ${en.size} keys parsed out of en.ts - the parser is wrong, not the code`);
if (index.size < 15) throw new Error(`only ${index.size} pages parsed out of searchIndex.ts - the parser is wrong, not the code`);

const problems = [];

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
const switchesOf = new Map();
for (const s of scans) {
  const text = src(s.file);
  for (const page of s.pages) {
    if (!sourceOf.has(page)) sourceOf.set(page, '');
    sourceOf.set(page, sourceOf.get(page) + text);
    if (!switchesOf.has(page)) switchesOf.set(page, new Set());
    for (const { key } of moduleToggles(text)) switchesOf.get(page).add(key);
  }
}
for (const [page, keys] of index) {
  for (const key of keys) {
    // A key missing from en.ts resolves to undefined, and the matcher's
    // .toLowerCase() then blanks the settings page.
    if (!en.has(key)) {
      problems.push(`${key} is in the search index under ${page} but not in en.ts - it resolves to undefined and blanks the page`);
      continue;
    }
    const text = sourceOf.get(page);
    if (text === undefined) {
      problems.push(`${page} is in the search index but no source file is mapped to it - add it to FILE_PAGES`);
      break;
    }
    if (!text.includes(`'${key}'`) && !switchesOf.get(page).has(key)) {
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
