// The claims README.md, the manual in docs/, the Help page and the census make
// about this build, checked against the build.
//
// Four documents describe this program to a reader who has not run it: the
// README, the manual, the Help page in Settings, and docs/jd-feature-census.md. They are
// written by hand at different times, and nothing else compares them to the
// source or to each other. Such drift breaks no build, test or type check; it
// is found by a person reading two files at once.
//
// So this checks the claims that can be settled mechanically and only those. A
// sentence about what the collector feels like is out of scope; a number, a
// service list and a default value are in it. The source of truth for a check
// added here has to be code, or the census's own tables, never a second
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
const FEATURES = read('docs', 'features.md');
const INSTALLING = read('docs', 'installing.md');
const CONFIGURATION = read('docs', 'configuration.md');
// The pages mkdocs.yml puts in the manual's navigation, joined: the manual as
// a reader meets it, without the working notes that share its folder.
const MANUAL = [...read('mkdocs.yml').matchAll(/^\s+- .*: (\S+\.md)\r?$/gm)]
  .map((m) => read('docs', m[1]))
  .join('\n');
const MAIN_GO = read('cmd', 'knightloader', 'main.go');
const CATALOGUE = read('internal', 'accounts', 'catalogue.go');
const DOCKERFILE = read('Dockerfile');
const HELP_TSX = read('web', 'src', 'pages', 'settings', 'Help.tsx');
const EN_TS = read('web', 'src', 'lib', 'locales', 'en.ts');
const LOCALES_INDEX = read('web', 'src', 'lib', 'locales', 'index.ts');

const problems = [];
const fail = (msg) => problems.push(msg);

// How many languages there are. Three places say a number and a fourth is the
// number: the files on disk. A locale file with no entry in index.ts ships to
// nobody, so both have to agree before either is worth quoting.

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
const langRow = FEATURES.match(/\|\s*\*\*Languages\*\*\s*\|\s*(\d+)/);
if (!langRow) {
  fail('docs/features.md: no "**Languages** | <n>" row in the feature table');
} else if (Number(langRow[1]) !== languages) {
  fail(`docs/features.md says ${langRow[1]} languages, web/src/lib/locales/ holds ${languages}`);
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

// The other two badges that state a fact about the build. A shields.io badge is
// a hand-typed string in an <img> URL that no toolchain reads, which makes it
// the likeliest thing in the file to be a year out of date while looking
// authoritative.

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

// The desktop bundles desktop.yml builds, against the manual's list of them
// and the README's download buttons. A bundle without a button is one nobody
// finds, and a button for one the matrix does not build leads nowhere. Only the
// buttons count, the ARM64 halves beside Windows and Linux too: a text link
// elsewhere in the README is not where a reader looks for a download.
const DESKTOP_YML = read('.github', 'workflows', 'desktop.yml');
const bundles = [...DESKTOP_YML.matchAll(/^\s+platform: (\S+)\n\s+slug: (\S+)$/gm)].map((m) => ({
  platform: m[1],
  slug: m[2],
}));
const listedPlatforms = INSTALLING.match(/Every release tag builds ([^.]+?),?\s+and attaches/)?.[1];
const buttonBlock = README.match(/<!-- download-buttons\b[^>]*-->([\s\S]*?)<!-- \/download-buttons -->/)?.[1];
if (bundles.length === 0) {
  fail('.github/workflows/desktop.yml: no platform line followed by a slug line, so the desktop builds cannot be checked');
} else if (!listedPlatforms) {
  fail('docs/installing.md: the "Every release tag builds ... and attaches" sentence is gone, so nothing here can check it');
} else if (!buttonBlock) {
  fail('README.md: the download-buttons block is gone, so nothing here can check the desktop downloads');
} else {
  const listed = [...listedPlatforms.matchAll(/`([^`]+)`/g)].map((m) => m[1]).sort();
  const built = bundles.map((b) => b.platform).sort();
  if (listed.join(',') !== built.join(',')) {
    fail(`docs/installing.md says every release builds ${listed.join(', ')}, desktop.yml builds ${built.join(', ')}`);
  }
  // <a href=".../releases/latest/download/knightloader-windows-arm64.zip"><img ...
  const buttons = new Set(
    [...buttonBlock.matchAll(/<a href="[^"]*\/releases\/latest\/download\/knightloader-([a-z0-9-]+)\.zip"><img /g)].map(
      (m) => m[1],
    ),
  );
  for (const { slug } of bundles) {
    if (!buttons.has(slug)) fail(`README.md has no download button for knightloader-${slug}.zip, which desktop.yml builds`);
  }
  for (const slug of buttons) {
    if (!bundles.some((b) => b.slug === slug)) {
      fail(`README.md has a download button for knightloader-${slug}.zip, which desktop.yml does not build`);
    }
  }
}

// The Wails CLI the manual tells a reader to install, against the version
// desktop/go.mod actually requires. Somebody following the manual with a
// different CLI builds the desktop app on a different toolchain than the one
// that produces the release, which is a class of bug that only ever shows up as
// "works here, not in CI".
//
// Compared against go.mod rather than against the workflow, which reads the
// require line itself because a hand-pinned copy drifted twice. Comparing the
// manual to that copy lets both be wrong together with nothing to notice, so
// go.mod is the one place the number lives and the one thing worth comparing
// against.
const DESKTOP_GOMOD = read('desktop', 'go.mod');
const manualWails = INSTALLING.match(/wails\/v2\/cmd\/wails@(v[\d.]+)/)?.[1];
const modWails = DESKTOP_GOMOD.match(/wailsapp\/wails\/v2 (v[\d.]+)/)?.[1];
if (!manualWails || !modWails) {
  fail('the wails install line is missing from docs/installing.md, or the require line from desktop/go.mod');
} else if (manualWails !== modWails) {
  fail(`docs/installing.md installs wails ${manualWails}, desktop/go.mod requires ${modWails}`);
}

// Which services the manual's environment table documents.
// internal/accounts/catalogue.go is the single list both the settings API and
// the Accounts page read. A service that carries an Env override and is not in
// the table is a key somebody cannot find.

const catalogueEnvs = [...CATALOGUE.matchAll(/\{ID: "([a-z0-9]+)".*?Env: "(KL_[A-Z_]+)"/g)].map((m) => ({
  id: m[1],
  env: m[2],
}));
if (catalogueEnvs.length === 0) {
  fail('internal/accounts/catalogue.go: no service with an Env override found - has the Catalogue literal changed shape?');
}

// The environment table's rows: | `KL_ADDR` | `:8749` | listen address |
const envRows = new Map();
for (const line of CONFIGURATION.split(/\r?\n/)) {
  const cells = line.split('|').map((c) => c.trim());
  const name = cells[1]?.match(/^`(KL_[A-Z_]+)`$/);
  if (!name) continue;
  envRows.set(name[1], (cells[2] ?? '').replace(/`/g, ''));
}

for (const { id, env } of catalogueEnvs) {
  if (!envRows.has(env)) {
    fail(`docs/configuration.md has no ${env} row, but ${id} in internal/accounts/catalogue.go offers it`);
  }
}

// The other direction: a documented variable nothing reads is a promise the
// program does not keep. Checked against the whole Go tree rather than one
// file, because these are read where they are used.
const GO_SOURCES = ['cmd', 'internal', 'desktop']
  .flatMap((dir) => goFiles(join(here, dir)))
  .map((f) => readFileSync(f, 'utf8'))
  .join('\n');

for (const env of envRows.keys()) {
  if (!GO_SOURCES.includes(env)) {
    fail(`docs/configuration.md documents ${env}, and no Go file mentions it`);
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

// The two defaults a reader acts on. KL_CNL decides whether the Click'n'Load
// switch reads on or off on a fresh install, and KL_PROVISION_JD decides
// whether a .dlc opens at all. Both have been documented as the opposite of
// what main.go does.

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
  const documented = envRows.get(env);
  if (documented === undefined) {
    fail(`docs/configuration.md has no ${env} row`);
  } else if (documented !== found[0]) {
    fail(
      `docs/configuration.md says ${env} defaults to "${documented}", cmd/knightloader/main.go defaults it to ${found[0]}`,
    );
  }
}

// The container must not pin KL_CNL, or the binary's default above is not the
// one a container install actually gets - which is exactly the drift the image
// carried while it set KL_CNL=0 and every doc said 9666.
if (/^ *(ENV +)?KL_CNL=/m.test(DOCKERFILE)) {
  fail('Dockerfile sets KL_CNL, so the container does not get the default docs/configuration.md and docs/clicknload.md quote');
}

// Every documented `docker build` of this image passes the revision in.
//
// Whoever builds the image follows docs/installing.md or docs/preview-deploy.md,
// so their build commands are the builds that get made. The binary cannot work
// the revision out for itself inside that build: .dockerignore excludes .git,
// so the Go toolchain in the build stage has no repository and stamps no
// vcs.revision (see buildinfo.Revision), and the preview deploy ships the tree
// with `git archive`, which carries no .git to exclude.
//
// An empty commit is a correct answer and the About card says so, drawing a
// plain crest with the revision unknown. A documented command that produces it
// by omission, on the build shape nearly every user has, is not. So a
// `docker build` naming this Dockerfile passes VERSION and COMMIT, or it is not
// documented here.

// A list, because saying `docker build` and telling a reader to run it are
// different sentences and only the second can be wrong. The two documents here
// hand somebody a command to paste; every other mention in the tree is prose
// about a command, and docs/easter-eggs.md quotes a build line with no COMMIT
// because that broken line is the evidence in its own account of the defect.
//
// A third document that does give a reader a build command belongs here, and
// the backstop under the loop is what makes that happen rather than hoping the
// next author reads this paragraph.
const BUILD_DOCS = [
  ['docs/installing.md', INSTALLING],
  ['docs/preview-deploy.md', read('docs', 'preview-deploy.md')],
];
if (!/ARG COMMIT=/.test(DOCKERFILE)) {
  fail('Dockerfile no longer takes a COMMIT build arg, so no documented build can stamp the revision');
}

/**
 * The `docker build` lines of THIS image that a reader is meant to RUN.
 *
 * Inside a fenced block only, which is the one mechanical difference between an
 * instruction and a mention: every runnable command in these documents is
 * fenced and all three prose mentions in the tree are not. Dockerfile.relay is
 * skipped either way - it builds a different binary, with no UI and no crest.
 */
function buildCommands(text) {
  const out = [];
  let fenced = false;
  text.split(/\r?\n/).forEach((line, i) => {
    if (/^\s*```/.test(line)) {
      fenced = !fenced;
      return;
    }
    if (!fenced || !/\bdocker build\b/.test(line) || /Dockerfile\.relay/.test(line)) return;
    out.push({ line: i + 1, text: line });
  });
  return out;
}

let commandsSeen = 0;
for (const [name, text] of BUILD_DOCS) {
  for (const cmd of buildCommands(text)) {
    commandsSeen++;
    for (const arg of ['VERSION', 'COMMIT']) {
      if (!new RegExp(String.raw`--build-arg\s+${arg}=`).test(cmd.text)) {
        fail(
          `${name}:${cmd.line} documents a \`docker build\` with no --build-arg ${arg}=. ` +
            (arg === 'COMMIT'
              ? 'The build context has no .git (.dockerignore excludes it, and git archive never ships it), ' +
                'so the binary it makes answers {"commit":""} and the About card\'s crest never turns.'
              : 'The version under the wordmark would read "dev" on a deployed image.'),
        );
      }
    }
  }
}
if (commandsSeen === 0) {
  fail('no `docker build` command found in docs/installing.md or docs/preview-deploy.md, so this check is looking in the wrong place');
}

// The document nobody added to the list. README.md and docs/ are the pages that
// tell a reader what to run now, so a build command appearing in one of them
// fails until it is checked like the other two.
//
// .github/release-notes/ is out of scope: those are an account of what was true
// on a past day, and a check about today's Dockerfile would ask somebody to
// edit history to make CI green.
const listed = new Set(BUILD_DOCS.map(([name]) => name));
const livingDocs = [
  'README.md',
  ...readdirSync(join(here, 'docs'))
    .filter((f) => f.endsWith('.md'))
    .map((f) => `docs/${f}`),
];
for (const name of livingDocs) {
  if (listed.has(name)) continue;
  for (const cmd of buildCommands(read(...name.split('/')))) {
    fail(
      `${name}:${cmd.line} hands a reader a \`docker build\` of this image, and ${name} is not in BUILD_DOCS in ` +
        'check-docs-claims.mjs - so nothing checks that it passes VERSION and COMMIT. Add it to that list.',
    );
  }
}

// Every resolver is named in the manual. The resolvers decide how anything is
// fetched, as its start page explains, so a whole backend the reader is never
// told about is the worst kind of omission.

// Package directory -> what the manual is expected to call it. A directory
// with no entry here fails on purpose: a new resolver should force a decision
// about how the manual names it, not slip past a lookup that returns nothing.
const RESOLVER_NAMES = {
  debrid: 'debrid',
  jd: 'JDownloader',
  // The package is named after the shape it needs (list a tree, ask a size,
  // read from an offset), not after today's protocol list, so there is no one
  // obvious word. SFTP is the token to check for: it is one of the four this
  // resolver actually claims, and unlike "FTP" it cannot match by accident
  // inside "FTPS" or a sentence about something else.
  // "header profile" rather than "hostheaders": the manual describes the thing
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
    fail(`internal/resolver/${dir} is a resolver this script has never heard of; add it to RESOLVER_NAMES and to the manual`);
    continue;
  }
  if (!MANUAL.toLowerCase().includes(name.toLowerCase())) {
    fail(`internal/resolver/${dir} ships, and no page in the manual says "${name}"`);
  }
}

// Help page key parity, and not a count: a hand-kept number goes stale, while
// the two sets being equal is both the relationship worth asserting and the one
// that is checkable.

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

// The census counts its own tables. The summary table, the Contents list and
// each area's heading all state totals somebody typed, so flipping one row's
// Status falsifies three numbers at once.

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

// Contents:  1. [Extensions...](#...) - 69 features: 6 have, 22 partial, 41 missing
// Heading:   69 features - 6 have, 22 partial, 41 missing.
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

// No line numbers into a file that moves. A citation like "README.md:73 states
// the no-relay stance" points somewhere else by the time anybody follows it. A
// section name survives an edit; a line number is a footnote with an expiry
// date.

for (const [i, line] of CENSUS.split(/\r?\n/).entries()) {
  const ref = line.match(/README\.md:\d+/);
  if (ref) {
    fail(
      `docs/jd-feature-census.md:${i + 1} cites ${ref[0]} - name the section instead, README.md's line numbers move`,
    );
  }
}

if (problems.length > 0) {

  console.error(`${problems.length} claim(s) the source does not support:`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}
console.log(
  `ok: ${languages} languages, ${catalogueEnvs.length} service env vars, ${resolverDirs.length} resolvers, ` +
    `${helpKeysUsed.size} help keys, ${totals.rows} census rows across ${areas.length} areas`,
);
