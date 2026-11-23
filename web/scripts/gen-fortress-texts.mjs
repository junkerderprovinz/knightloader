// Rebuilds internal/fortress/texts.json from the locale files. Run it after
// changing a text the fortress status uses:
//
//   node scripts/gen-fortress-texts.mjs
//
// The vitest suite fails while the file is behind the locales.
import { readdir, writeFile } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { fortressTexts } from '../src/lib/fortressTexts.ts';

const here = dirname(fileURLToPath(import.meta.url));
const locales = join(here, '..', 'src', 'lib', 'locales');
const out = join(here, '..', '..', 'internal', 'fortress', 'texts.json');

const dicts = {};
for (const file of await readdir(locales)) {
  if (!/^[a-z]{2}\.ts$/.test(file)) continue;
  const lang = file.slice(0, 2);
  dicts[lang] = (await import(pathToFileURL(join(locales, file)).href))[lang];
}
await writeFile(out, JSON.stringify(fortressTexts(dicts), null, 2) + '\n');
