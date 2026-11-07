// Every ink the app computes reaches 4.5:1, whatever colour feeds it
// (GlimStone's rule 45), and it is the ink the web interface computes for the
// same colour. A picked colour or Material You can be anything, so the check
// runs a sweep of colours through both copies of contrast.ts and measures the
// result on every ground accent text sits on.
//
// Both files import nothing at run time, so node strips their types and these
// checks call the real functions.
//
// Run by hand and by CI, from mobile/: `node check-contrast.mjs`
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const load = (...p) => import(pathToFileURL(join(here, ...p)).href);
const phone = await load('src', 'theme', 'contrast.ts');
const web = await load('..', 'web', 'src', 'lib', 'contrast.ts');

const problems = [];
const check = (what, ok) => {
  if (!ok) problems.push(what);
};

// The page and the card of each theme in src/theme/tokens.ts.
const LIGHT_GROUNDS = ['#ffffff'];
const DARK_GROUNDS = ['#161616', '#262626'];
const OLED_GROUNDS = ['#000000', '#161616'];

// tokens.inkFor's light mix, without react-native.
const lightMix = (hex) => {
  const n = parseInt(hex.slice(1), 16);
  return '#' + [16, 8, 0].map((s) => Math.round(((n >> s) & 255) * 0.55).toString(16).padStart(2, '0')).join('');
};

const PRESETS = ['#FCC419', '#1D99F3', '#6FDC8C', '#FF8389', '#BE95FF', '#FF832B', '#3DDBD9', '#FF7EB6'];
const sweep = [...PRESETS, '#FAFAFA', '#050505', '#777777', '#7B7B7B', '#0000FF', '#FFFF00'];
const steps = [0x00, 0x24, 0x49, 0x6d, 0x92, 0xb6, 0xdb, 0xff];
for (const r of steps) for (const g of steps) for (const b of steps) sweep.push('#' + [r, g, b].map((c) => c.toString(16).padStart(2, '0')).join(''));

for (const hex of sweep) {
  const fill = phone.contrastOn(hex);
  check(`the ink on ${hex} reads at 4.5:1`, phone.contrastRatio(fill, hex) >= 4.5);
  check(`the ink on ${hex} is the web's`, fill === web.contrastOn(hex));

  const inks = phone.accentInks(hex);
  check(`${hex} as text gets the web's inks`, JSON.stringify(inks) === JSON.stringify(web.accentInks(hex)));
  const light = inks.light ?? lightMix(hex);
  const dark = inks.dark ?? hex;
  for (const g of LIGHT_GROUNDS) check(`${hex} as text reads at 4.5:1 on ${g}`, phone.contrastRatio(light, g) >= 4.5);
  for (const g of [...DARK_GROUNDS, ...OLED_GROUNDS]) check(`${hex} as text reads at 4.5:1 on ${g}`, phone.contrastRatio(dark, g) >= 4.5);
}

for (const hex of PRESETS) {
  check(`preset ${hex} keeps its own ink as text`, phone.accentInks(hex).light === undefined && phone.accentInks(hex).dark === undefined);
  check(`preset ${hex} carries near-black`, phone.contrastOn(hex) === '#161616');
}

if (problems.length) {
  console.error(`${problems.length} problem(s):`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}
console.log(`ok: ${sweep.length} colours read at 4.5:1 as fill and as text in every theme, the same as on the web`);
