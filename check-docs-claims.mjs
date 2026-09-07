// The claims README.md, the Help page and docs/ make about this build, checked
// against the build.
//
// Three documents describe this program to a reader who has not run it: the
// README, the Help page in Settings, and docs/jd-feature-census.md. They are
// written by hand, at different times, and nothing was comparing them to the
// source or to each other - so they drifted, quietly, in the direction that
// always flatters: README.md said KL_PROVISION_JD defaulted to 0 while main.go
// defaulted it to 1, listed three debrid services against the catalogue's
// seven, said "the repository is private" four screens under a notice saying it
// is public, and the census summed 205 partial rows over a table holding 206.
// None of that breaks a build, a test or a type check. It is only ever found by
// a person reading two files at once.
//
// So this checks the claims that can be settled mechanically, and only those.
// A sentence about what the collector feels like is not in scope; a number, a
// service list and a default value are. The rule for adding a check here: the
// source of truth must be CODE (or the census's own tables), never a second
// document, or this becomes one more thing to keep in step.
//
// Run by CI and by hand: `node check-docs-claims.mjs`.

import { readdirSync, readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const read = (...p) => readFileSync(join(here, ...p), 'utf8');

const README = read('README.md');
const CENSUS = read('docs', 'jd-feature-census.md');
const MAIN_GO = read('cmd', 'knightloader', 'main.go');
const CATALOGUE = read('internal', 'accounts', 'catalogue.go');
const DOCKERFILE = read('Dockerfile');
const HELP_TSX = read('web', 'src', 'pages', 'settings', 'Help.tsx');
const EN_TS = read('web', 'src', 'lib', 'locales', 'en.ts');
const LOCALES_INDEX = read('web', 'src', 'lib', 'locales', 'index.ts');

const problems = [];
const fail = (msg) => problems.push(msg);

// ---------------------------------------------------------------------------
// 1. How many languages there are.
//
// Three places say a number and a fourth is the number: the files on disk. A
// locale file with no entry in index.ts ships to nobody, so both have to agree
// before either is worth quoting.
// ---------------------------------------------------------------------------

const localeFiles = readdirSync(join(here, 'web', 'src', 'lib', 'locales'))
  .filter((f) => f.endsWith('.ts') && f !== 'index.ts')
  .map((f) => f.slice(0, -3))
  .sort();

// The LOADERS map, keyed by language code: `  de: async () => ...`.
const loaders = [...LOCALES_INDEX.matchAll(/^ {2}([a-z]{2}): async \(\) =>/gm)].map((m) => m[1]).sort();

for (const code of localeFiles) {
  if (!loaders.includes(code)) {
    fail(`locales/${code}.ts exists but index.ts has no loader for it - that language ships to nobody`);
  }
}
for (const code of loaders) {
  if (!localeFiles.includes(code)) {
    fail(`index.ts loads './${code}' but web/src/lib/locales/${code}.ts is not there`);
  }
}

const languages = localeFiles.length;

// The shields.io badge at the top of the README: .../Languages-42-393939?...
const badge = README.match(/badge\/Languages-(\d+)-/);
if (!badge) {
  fail('README.md: the Languages badge is gone, so nothing here can check its number');
} else if (Number(badge[1]) !== languages) {
  fail(`README.md Languages badge says ${badge[1]}, web/src/lib/locales/ holds ${languages}`);
}

// The feature table's own row: | **Languages** | 42, each fetched ... |
const langRow = README.match(/\|\s*\*\*Languages\*\*\s*\|\s*(\d+)/);
if (!langRow) {
  fail('README.md: no "**Languages** | <n>" row in the feature table');
} else if (Number(langRow[1]) !== languages) {
  fail(`README.md feature table says ${langRow[1]} languages, web/src/lib/locales/ holds ${languages}`);
}

// The census row comparing our breadth to JD's.
const censusLang = CENSUS.match(/(\d+) languages against JD's/);
if (!censusLang) {
  fail("docs/jd-feature-census.md: the \"<n> languages against JD's\" claim is gone");
} else if (Number(censusLang[1]) !== languages) {
  fail(
    `docs/jd-feature-census.md says ${censusLang[1]} languages against JD's, web/src/lib/locales/ holds ${languages}`,
  );
}

// ---------------------------------------------------------------------------
// 1b. The other two badges that state a fact about the build.
//
// A shields.io badge is a hand-typed string in an <img> URL that no toolchain
// reads, which makes it the single most likely thing in the file to be a year
// out of date while looking authoritative.
// ---------------------------------------------------------------------------

const goDirective = read('go.mod').match(/^go (\d+)\.(\d+)/m);
const goBadge = README.match(/badge\/Go-([\d.]+)-/);
if (!goDirective) {
  fail('go.mod has no `go <version>` directive');
} else if (!goBadge) {
  fail('README.md: the Go badge is gone, so nothing here can check its version');
} else {
  const want = `${goDirective[1]}.${goDirective[2]}`;
  if (goBadge[1] !== want) {
    fail(`README.md's Go badge says ${goBadge[1]}, go.mod asks for ${want}`);
  }
}

// The image the release workflow actually builds, against the badge that
// promises it: `platforms: linux/amd64,linux/arm64`.
const RELEASE_YML = read('.github', 'workflows', 'release.yml');
const built = RELEASE_YML.match(/^\s*platforms: (\S+)$/m);
const archBadge = README.match(/badge\/Arch-([^-]+)-/);
if (!built) {
  fail('.github/workflows/release.yml has no `platforms:` line, so the Arch badge cannot be checked');
} else if (!archBadge) {
  fail('README.md: the Arch badge is gone, so nothing here can check it');
} else {
  // amd64%20%7C%20arm64 -> ['amd64', 'arm64']
  const promised = decodeURIComponent(archBadge[1])
    .split('|')
    .map((s) => s.trim())
    .sort();
  const actual = built[1]
    .split(',')
    .map((p) => p.split('/').pop())
    .sort();
  if (promised.join(',') !== actual.join(',')) {
    fail(
      `README.md's Arch badge promises ${promised.join(', ')}, release.yml builds ${actual.join(', ')}`,
    );
  }
}

// The Wails CLI the README tells a reader to install, against the one the
// release build uses. These had drifted to v2.10.2 and v2.13.0, so anyone
// following the README built the desktop app with a different toolchain than
// the one that produces the release, which is a class of bug that only ever
// shows up as "works here, not in CI".
const DESKTOP_YML = read('.github', 'workflows', 'desktop.yml');
const wailsIn = (src) => src.match(/wails\/v2\/cmd\/wails@(v[\d.]+)/)?.[1];
const readmeWails = wailsIn(README);
const ciWails = wailsIn(DESKTOP_YML);
if (!readmeWails || !ciWails) {
  fail('the `wails@<version>` line is missing from README.md or .github/workflows/desktop.yml');
} else if (readmeWails !== ciWails) {
  fail(`README.md installs wails ${readmeWails}, .github/workflows/desktop.yml builds releases with ${ciWails}`);
}

// ---------------------------------------------------------------------------
// 2. Which services the README's environment table documents.
//
// internal/accounts/catalogue.go is the single list both the settings API and
// the Accounts page read. A service that carries an Env override and is not in
// the README is a key somebody cannot find, and the README listed three of the
// six for weeks after four more landed.
// ---------------------------------------------------------------------------

const catalogueEnvs = [...CATALOGUE.matchAll(/\{ID: "([a-z0-9]+)".*?Env: "(KL_[A-Z_]+)"/g)].map((m) => ({
  id: m[1],
  env: m[2],
}));
if (catalogueEnvs.length === 0) {
  fail('internal/accounts/catalogue.go: no service with an Env override found - has the Catalogue literal changed shape?');
}

// The environment table's rows: | `KL_ADDR` | `:8749` | listen address |
const readmeEnvRows = new Map();
for (const line of README.split(/\r?\n/)) {
  const cells = line.split('|').map((c) => c.trim());
  const name = cells[1]?.match(/^`(KL_[A-Z_]+)`$/);
  if (!name) continue;
  readmeEnvRows.set(name[1], (cells[2] ?? '').replace(/`/g, ''));
}

for (const { id, env } of catalogueEnvs) {
  if (!readmeEnvRows.has(env)) {
    fail(`README.md's Configuration table has no ${env} row, but ${id} in internal/accounts/catalogue.go offers it`);
  }
}

// The other direction: a documented variable nothing reads is a promise the
// program does not keep. Checked against the whole Go tree rather than one
// file, because these are read where they are used.
const GO_SOURCES = ['cmd', 'internal', 'desktop']
  .flatMap((dir) => goFiles(join(here, dir)))
  .map((f) => readFileSync(f, 'utf8'))
  .join('\n');

for (const env of readmeEnvRows.keys()) {
  if (!GO_SOURCES.includes(env)) {
    fail(`README.md documents ${env}, and no Go file mentions it`);
  }
}

function goFiles(dir) {
  const out = [];
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    const p = join(dir, e.name);
    if (e.isDirectory()) out.push(...goFiles(p));
    else if (e.name.endsWith('.go') && !e.name.endsWith('_test.go')) out.push(p);
  }
  return out;
}

// ---------------------------------------------------------------------------
// 3. The two defaults a reader acts on.
//
// KL_CNL decides whether the Click'n'Load switch reads on or off on a fresh
// install, and KL_PROVISION_JD decides whether a .dlc opens at all. Both were
// documented as the opposite of what main.go does at some point.
// ---------------------------------------------------------------------------

for (const env of ['KL_CNL', 'KL_PROVISION_JD']) {
  const found = [...MAIN_GO.matchAll(new RegExp(`envInt\\("${env}", (\\d+)\\)`, 'g'))].map((m) => m[1]);
  if (found.length === 0) {
    fail(`cmd/knightloader/main.go no longer reads ${env} with envInt, so its default cannot be read from here`);
    continue;
  }
  if (new Set(found).size > 1) {
    fail(`cmd/knightloader/main.go gives ${env} more than one default (${[...new Set(found)].join(', ')})`);
    continue;
  }
  const documented = readmeEnvRows.get(env);
  if (documented === undefined) {
    fail(`README.md's Configuration table has no ${env} row`);
  } else if (documented !== found[0]) {
    fail(
      `README.md says ${env} defaults to "${documented}", cmd/knightloader/main.go defaults it to ${found[0]}`,
    );
  }
}

// The container must not pin KL_CNL, or the binary's default above is not the
// one a container install actually gets - which is exactly the drift the image
// carried while it set KL_CNL=0 and every doc said 9666.
if (/^ *(ENV +)?KL_CNL=/m.test(DOCKERFILE)) {
  fail('Dockerfile sets KL_CNL, so the container does not get the default README.md and docs/clicknload.md quote');
}

// ---------------------------------------------------------------------------
// 4. Every resolver is named in the README.
//
// The resolvers ARE the architecture (README.md's Overview says so), so a whole
// backend the reader is never told about is the worst kind of omission: the
// torrent resolver shipped registered unconditionally and appeared in neither
// the feature table nor the diagram.
// ---------------------------------------------------------------------------

// Package directory -> what the README is expected to call it. A directory
// with no entry here fails on purpose: a new resolver should force a decision
// about how the README names it, not slip past a lookup that returns nothing.
const RESOLVER_NAMES = {
  debrid: 'debrid',
  jd: 'JDownloader',
  // The package is named after the shape it needs (list a tree, ask a size,
  // read from an offset), not after today's protocol list, so there is no one
  // obvious word. SFTP is the token to check for: it is one of the four this
  // resolver actually claims, and unlike "FTP" it cannot match by accident
  // inside "FTPS" or a sentence about something else.
  // "header profile" rather than "hostheaders": the README describes the thing
  // by what a person calls it, and a check that demands the package name would
  // force the documentation to speak Go.
  hostheaders: 'header profile',
  remotefs: 'SFTP',
  torbox: 'TorBox',
  torrent: 'torrent',
  ytdlp: 'yt-dlp',
};

const resolverDirs = readdirSync(join(here, 'internal', 'resolver'), { withFileTypes: true })
  .filter((e) => e.isDirectory())
  .map((e) => e.name)
  .sort();

for (const dir of resolverDirs) {
  const name = RESOLVER_NAMES[dir];
  if (!name) {
    fail(`internal/resolver/${dir} is a resolver this script has never heard of - add it to RESOLVER_NAMES and to README.md`);
    continue;
  }
  if (!README.toLowerCase().includes(name.toLowerCase())) {
    fail(`internal/resolver/${dir} ships, and README.md never says "${name}"`);
  }
}

// ---------------------------------------------------------------------------
// 5. Help page key parity.
//
// Not a count. The file's own doc comment used to assert "all 47 keys" over 46,
// which is what a hand-kept number does; the relationship worth asserting is
// that the two sets are equal, and that one is checkable.
// ---------------------------------------------------------------------------

const helpKeysUsed = new Set([...HELP_TSX.matchAll(/'(settings\.help\.[A-Za-z0-9.]+)'/g)].map((m) => m[1]));
const helpKeysDefined = new Set([...EN_TS.matchAll(/^ {2}'(settings\.help\.[A-Za-z0-9.]+)':/gm)].map((m) => m[1]));

for (const k of helpKeysUsed) {
  if (!helpKeysDefined.has(k)) fail(`Help.tsx reads ${k}, and en.ts does not define it`);
}
for (const k of helpKeysDefined) {
  if (!helpKeysUsed.has(k)) fail(`en.ts defines ${k}, and Help.tsx renders nothing with it - dead help text`);
}
if (helpKeysUsed.size === 0) {
  fail('Help.tsx reads no settings.help.* key at all - has the page changed shape?');
}

// ---------------------------------------------------------------------------
// 6. The census counts its own tables.
//
// The summary table, the Contents list and each area's own heading all state
// totals somebody typed. Flipping one row's Status silently falsifies three
// numbers, which is how the summary came to sum 205 partial over 206 rows.
// ---------------------------------------------------------------------------

const STATUSES = ['have', 'partial', 'missing'];
const areas = [];
let area = null;
for (const line of CENSUS.split(/\r?\n/)) {
  const heading = line.match(/^## (\d+)\. (.+)$/);
  if (heading) {
    area = { n: Number(heading[1]), title: heading[2], have: 0, partial: 0, missing: 0, rows: 0 };
    areas.push(area);
    continue;
  }
  if (!area || !line.startsWith('|')) continue;
  // A cell may hold an escaped pipe (a regex alternation, say), which a plain
  // split would read as a column break and shift every later column by one.
  const cells = line.split(/(?<!\\)\|/).map((c) => c.trim());
  const status = cells[5];
  if (!STATUSES.includes(status)) continue;
  area[status]++;
  area.rows++;
}

if (areas.length === 0) {
  fail('docs/jd-feature-census.md: no "## <n>. <title>" areas found - has the document changed shape?');
}

const totals = { have: 0, partial: 0, missing: 0, rows: 0 };
for (const a of areas) for (const k of Object.keys(totals)) totals[k] += a[k];

// **928 features** found across 9 areas.
const declaredTotal = CENSUS.match(/\*\*(\d+) features\*\* found across (\d+) areas/);
if (!declaredTotal) {
  fail('docs/jd-feature-census.md: no "**<n> features** found across <n> areas" line');
} else {
  if (Number(declaredTotal[1]) !== totals.rows) {
    fail(`census header says ${declaredTotal[1]} features, its tables hold ${totals.rows}`);
  }
  if (Number(declaredTotal[2]) !== areas.length) {
    fail(`census header says ${declaredTotal[2]} areas, the document has ${areas.length}`);
  }
}

// | have | 79 | present and working here |
for (const status of STATUSES) {
  const row = CENSUS.match(new RegExp(`^\\| ${status} \\| (\\d+) \\|`, 'm'));
  if (!row) {
    fail(`census summary table has no "${status}" row`);
  } else if (Number(row[1]) !== totals[status]) {
    fail(`census summary says ${row[1]} ${status}, its tables hold ${totals[status]}`);
  }
}

// Contents:  1. [Extensions...](#...) — 69 features: 6 have, 22 partial, 41 missing
// Heading:   69 features — 6 have, 22 partial, 41 missing.
for (const a of areas) {
  const contents = CENSUS.match(
    new RegExp(`^${a.n}\\. \\[[^\\]]+\\]\\([^)]+\\) [-—] (\\d+) features: (\\d+) have, (\\d+) partial, (\\d+) missing`, 'm'),
  );
  if (!contents) {
    fail(`census Contents has no line for area ${a.n} (${a.title}) in the expected shape`);
  } else {
    const [, n, have, partial, missing] = contents.map(Number);
    if (n !== a.rows || have !== a.have || partial !== a.partial || missing !== a.missing) {
      fail(
        `census Contents line ${a.n} says ${n} features: ${have} have, ${partial} partial, ${missing} missing; ` +
          `area ${a.n} (${a.title}) holds ${a.rows}: ${a.have} have, ${a.partial} partial, ${a.missing} missing`,
      );
    }
  }
}

// The per-area headline lines, matched in document order against the areas.
const headlines = [...CENSUS.matchAll(/^(\d+) features [-—] (\d+) have, (\d+) partial, (\d+) missing\.$/gm)];
if (headlines.length !== areas.length) {
  fail(`census has ${headlines.length} "<n> features - ... " headline lines for ${areas.length} areas`);
} else {
  headlines.forEach((m, i) => {
    const a = areas[i];
    const [n, have, partial, missing] = m.slice(1).map(Number);
    if (n !== a.rows || have !== a.have || partial !== a.partial || missing !== a.missing) {
      fail(
        `census headline under "## ${a.n}. ${a.title}" says ${n} features: ${have} have, ${partial} partial, ` +
          `${missing} missing; the table holds ${a.rows}: ${a.have} have, ${a.partial} partial, ${a.missing} missing`,
      );
    }
  });
}

// ---------------------------------------------------------------------------
// 7. No line numbers into a file that moves.
//
// docs/ used to cite "README.md:73 states the no-relay stance" and three more
// like it. Every one of them pointed somewhere else by the time anybody
// followed it, and one pointed at the Buy-me-a-coffee button. A section name
// survives an edit; a line number is a footnote with an expiry date.
// ---------------------------------------------------------------------------

for (const [i, line] of CENSUS.split(/\r?\n/).entries()) {
  const ref = line.match(/README\.md:\d+/);
  if (ref) {
    fail(
      `docs/jd-feature-census.md:${i + 1} cites ${ref[0]} - name the section instead, README.md's line numbers move`,
    );
  }
}

// ---------------------------------------------------------------------------

if (problems.length > 0) {
  console.error(`${problems.length} claim(s) the source does not support:`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}
console.log(
  `ok: ${languages} languages, ${catalogueEnvs.length} service env vars, ${resolverDirs.length} resolvers, ` +
    `${helpKeysUsed.size} help keys, ${totals.rows} census rows across ${areas.length} areas`,
);
