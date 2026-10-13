// Rebuilds public/hosters and the list of its logos in src/lib/hosterLogos.ts
// from a folder of logos named after their hoster's domain (1fichier.com.png),
// as collected from the sites themselves. The logos sit in public rather than
// being imported: every imported asset adds a URL expression to the main
// script, while the list adds only a name.
//
//   npm i --no-save sharp
//   node scripts/gen-hoster-logos.mjs <folder>
//
// Each logo becomes a 64-pixel palette PNG. One drawn in ink that would vanish
// on one of the two themes gets a plate of the other theme's ground, and
// subdomains that share one image are written once under the domain above
// them. The same image under unrelated domains is a hosting script's default
// rather than a logo, so those are left out and named. Look at the folder
// before running this: a logo of the wrong brand passes every check here.
import { mkdir, readdir, rm, writeFile } from 'node:fs/promises';
import { createHash } from 'node:crypto';
import { dirname, extname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const out = join(here, '..', 'public', 'hosters');
const list = join(here, '..', 'src', 'lib', 'hosterLogos.ts');

const SIZE = 64;
// A plated logo keeps this much ground around it, a pixel at the 16-pixel size.
const PLATE_INSET = 4;
// The theme grounds, --carbon-bg in src/index.css.
const DARK = { r: 0x16, g: 0x16, b: 0x16 };
const LIGHT = { r: 0xf4, g: 0xf4, b: 0xf4 };
const PLATE_LIGHT = { r: 255, g: 255, b: 255, alpha: 1 };
const PLATE_DARK = { ...DARK, alpha: 1 };

const source = process.argv[2];
if (!source) {
  console.error('usage: node scripts/gen-hoster-logos.mjs <folder of <domain>.png files>');
  process.exit(2);
}

let sharp;
try {
  sharp = (await import('sharp')).default;
} catch {
  console.error('sharp is missing: npm i --no-save sharp');
  process.exit(2);
}

const near = (px, i, ground) =>
  Math.hypot(px[i] - ground.r, px[i + 1] - ground.g, px[i + 2] - ground.b) < 70;

/**
 * plateFor is the plate a logo needs, if any: a logo with a see-through
 * ground whose drawn pixels nearly all match one theme's ground disappears
 * there. One with a ground of its own already has a tile.
 */
function plateFor(px) {
  let drawn = 0;
  let onDark = 0;
  let onLight = 0;
  for (let i = 0; i < px.length; i += 4) {
    if (px[i + 3] < 128) continue;
    drawn++;
    if (near(px, i, DARK)) onDark++;
    if (near(px, i, LIGHT)) onLight++;
  }
  if (drawn === 0 || drawn / (px.length / 4) > 0.9) return null;
  if (onDark / drawn >= 0.9) return PLATE_LIGHT;
  if (onLight / drawn >= 0.9) return PLATE_DARK;
  return null;
}

async function render(file) {
  const fit = { fit: 'contain', background: { r: 0, g: 0, b: 0, alpha: 0 } };
  const { data } = await sharp(file).resize(SIZE, SIZE, fit).ensureAlpha().raw().toBuffer({ resolveWithObject: true });
  const plate = plateFor(data);
  let image = sharp(file).resize(SIZE, SIZE, fit);
  if (plate) {
    const inner = await sharp(file)
      .resize(SIZE - 2 * PLATE_INSET, SIZE - 2 * PLATE_INSET, fit)
      .png()
      .toBuffer();
    image = sharp({ create: { width: SIZE, height: SIZE, channels: 4, background: plate } }).composite([
      { input: inner, left: PLATE_INSET, top: PLATE_INSET },
    ]);
  }
  const png = await image.png({ palette: true, quality: 90, effort: 10, compressionLevel: 9 }).toBuffer();
  return { png, plated: Boolean(plate) };
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

const files = (await readdir(source)).filter((f) => /\.(png|jpe?g|webp|gif|svg)$/i.test(f)).sort();
const byImage = new Map();
let plated = 0;
for (const f of files) {
  const domain = f.slice(0, -extname(f).length).toLowerCase();
  const { png, plated: p } = await render(join(source, f));
  if (p) plated++;
  const key = createHash('sha256').update(png).digest('hex');
  byImage.set(key, { png, domains: [...(byImage.get(key)?.domains ?? []), domain] });
}

await rm(out, { recursive: true, force: true });
await mkdir(out, { recursive: true });
const names = [];
let bytes = 0;
const generic = [];
for (const { png, domains } of byImage.values()) {
  const name = domains.length === 1 ? domains[0] : commonParent(domains);
  if (!name) {
    generic.push(domains.join(', '));
    continue;
  }
  await writeFile(join(out, `${name}.png`), png);
  names.push(name);
  bytes += png.length;
}
names.sort();
const entries = names.map((n) => `  '${n}',\n`).join('');
await writeFile(
  list,
  `// Generated by scripts/gen-hoster-logos.mjs, do not edit by hand: the hosters
// with a logo in public/hosters.
export const HOSTER_LOGOS: ReadonlySet<string> = new Set([
${entries}]);
`,
);

console.log(`${names.length} logos from ${files.length} files, ${(bytes / 1024).toFixed(0)} KiB, ${plated} on a plate`);
for (const group of generic) console.log(`left out, one image under unrelated domains: ${group}`);
