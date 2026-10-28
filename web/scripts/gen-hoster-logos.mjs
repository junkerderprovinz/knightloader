// Rebuilds public/hosters and the list of its logos in src/lib/hosterLogos.ts
// from a folder of logos named after their hoster's domain (1fichier.com.png),
// as collected from the sites themselves, and the debrid and captcha services'
// marks in src/assets/marks. The logos sit in public rather than being
// imported: every imported asset adds a URL expression to the main script,
// while the list adds only a name.
//
//   npm i --no-save sharp
//   node scripts/gen-hoster-logos.mjs <folder>
//   node scripts/gen-hoster-logos.mjs --update <folder>
//
// Each logo becomes a 64-pixel palette PNG, lifted off a square plate it was
// drawn on, with its colour, its ink and how it stands at rest in the list
// (src/lib/logoTile.ts), and beside it <domain>.lit.png where the lit tile
// needs a mask of its own. Subdomains that share one image are written once
// under the domain above them. The same image under unrelated domains is a
// hosting script's default rather than a logo, so those are left out and
// named. Look at the folder before running this: a logo of the wrong brand
// passes every check here.
//
// With --update the folder replaces or adds only the logos it holds and keeps
// every other one, for when a sharper logo turns up for a few hosters. A
// 16-pixel favicon blown up to 64 pixels stays blurred at the size the pickers
// show it, so a logo in the folder should be at least 64 pixels.
import { mkdir, readdir, readFile, rm, writeFile } from 'node:fs/promises';
import { dirname, extname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { entry, prepare } from './logo-pipeline.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const out = join(here, '..', 'public', 'hosters');
const list = join(here, '..', 'src', 'lib', 'hosterLogos.ts');
const marks = join(here, '..', 'src', 'assets', 'marks');

// The captcha services, and the debrid services whose sites offer only a
// small favicon or another brand's, are kept by hand: each mark is the
// service's own, taken from its browser extension, its GitHub account or its
// site at 128 pixels where it has one. They win over a collected logo. Keyed
// by the service's own domain, which also covers the dashboard subdomain a
// catalogue entry links to (dash.capmonster.cloud).
const SERVICE_MARKS = {
  '2captcha.com': '2captcha.svg',
  'anti-captcha.com': 'anticaptcha.png',
  'capmonster.cloud': 'capmonster.svg',
  'capsolver.com': 'capsolver.png',
  '9kw.eu': '9kw.png',
  'deathbycaptcha.com': 'deathbycaptcha.png',
  'solvecha.com': 'solvecha.svg',
  'alldebrid.com': 'alldebrid.png',
  'bestdebrid.com': 'bestdebrid.png',
  'cocoleech.com': 'cocoleech.png',
  'debriditalia.com': 'debriditalia.png',
  'deepbrid.com': 'deepbrid.png',
  'fakirdebrid.net': 'fakirdebrid.png',
  'linksnappy.com': 'linksnappy.png',
  // The site's touch icon is the default of the framework it runs on.
  'multiup.io': 'multiup.png',
  'neodebrid.com': 'neodebrid.png',
  'rpnet.biz': 'rpnet.png',
  // The site's icon draws the cube on a white plate, and lifted off it the
  // paler faces come out translucent.
  'torbox.app': 'torbox.png',
  'zevera.com': 'zevera.png',
};

const update = process.argv[2] === '--update';
const source = process.argv[update ? 3 : 2];
if (!source) {
  console.error('usage: node scripts/gen-hoster-logos.mjs [--update] <folder of <domain>.png files>');
  process.exit(2);
}

let sharp;
try {
  sharp = (await import('sharp')).default;
} catch {
  console.error('sharp is missing: npm i --no-save sharp');
  process.exit(2);
}

// Second-level names under a country code that anyone registers below, as in
// co.uk or com.br. Two sites under one of them are not related.
const SHARED_SECOND_LEVEL = new Set(['ac', 'co', 'com', 'edu', 'gov', 'net', 'org']);

/** commonParent is the domain every name in the group is or lies under, or null. */
function commonParent(domains) {
  const split = domains.map((d) => d.split('.').reverse());
  const shared = [];
  for (let i = 0; split.every((labels) => i < labels.length && labels[i] === split[0][i]); i++) {
    shared.push(split[0][i]);
  }
  if (shared.length < 2 || (shared.length === 2 && SHARED_SECOND_LEVEL.has(shared[1]))) return null;
  return shared.reverse().join('.');
}

const files = (await readdir(source))
  .filter((f) => /\.(png|jpe?g|webp|gif|svg)$/i.test(f) && !f.endsWith('.lit.png'))
  .map((f) => ({ domain: f.slice(0, -extname(f).length).toLowerCase(), path: join(source, f) }))
  .filter((f) => !(f.domain in SERVICE_MARKS));
files.push(...Object.entries(SERVICE_MARKS).map(([domain, file]) => ({ domain, path: join(marks, file) })));
files.sort((a, b) => a.domain.localeCompare(b.domain));

const byImage = new Map();
let lifted = 0;
for (const f of files) {
  const logo = await prepare(sharp, await readFile(f.path));
  if (logo.lifted) lifted++;
  byImage.set(logo.image, { ...logo, domains: [...(byImage.get(logo.image)?.domains ?? []), f.domain] });
}

// What an update keeps of the list, read from the list itself.
const kept = new Map();
if (update) {
  const { HOSTER_LOGOS } = await import(pathToFileURL(list).href);
  for (const [name, info] of HOSTER_LOGOS) kept.set(name, info);
} else {
  await rm(out, { recursive: true, force: true });
}
await mkdir(out, { recursive: true });
let written = 0;
let bytes = 0;
const generic = [];
for (const { png, lit, info, domains } of byImage.values()) {
  const name = domains.length === 1 ? domains[0] : commonParent(domains);
  if (!name) {
    generic.push(domains.join(', '));
    continue;
  }
  await writeFile(join(out, `${name}.png`), png);
  await rm(join(out, `${name}.lit.png`), { force: true });
  if (lit) await writeFile(join(out, `${name}.lit.png`), lit);
  kept.set(name, info);
  written++;
  bytes += png.length + (lit?.length ?? 0);
}
const names = [...kept.keys()].sort();
const entries = names.map((n) => `  ['${n}', ${entry(kept.get(n))}],\n`).join('');
await writeFile(
  list,
  `// Generated by scripts/gen-hoster-logos.mjs, do not edit by hand: the hosters
// and services with a logo in public/hosters, and its colours on a brand tile.
import type { LogoTile } from './logoTile';

export const HOSTER_LOGOS: ReadonlyMap<string, LogoTile> = new Map<string, LogoTile>([
${entries}]);
`,
);

console.log(
  `${written} logos from ${files.length} files, ${(bytes / 1024).toFixed(0)} KiB, ${lifted} lifted off a plate; ${names.length} in the list`,
);
for (const group of generic) console.log(`left out, one image under unrelated domains: ${group}`);
