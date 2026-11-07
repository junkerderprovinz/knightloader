// No :has() in the extension's stylesheets tests the class or style attribute
// (GlimStone's rule 36). Such a selector makes Chrome restyle the whole page
// whenever a class or an inline style changes anywhere in it, and these pages
// change inline styles on every frame of a drag in the colour picker and of
// disco's walk. A :has() names a class instead, as `:has(.glim-readme-btn-hint)`.
//
// Run by CI and by hand: `node extension/check-has-selector.mjs`.
import { readdirSync, readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const src = join(dirname(fileURLToPath(import.meta.url)), 'src');
const problems = [];

for (const file of readdirSync(src).filter((f) => /\.(css|html)$/.test(f))) {
  const lines = readFileSync(join(src, file), 'utf8').split('\n');
  lines.forEach((line, i) => {
    for (const m of line.matchAll(/:has\(([^)]*)\)/g)) {
      const attr = m[1].match(/\[\s*(class|style)\b/)?.[1];
      if (attr) problems.push(`src/${file}:${i + 1}: ${m[0]} tests the ${attr} attribute`);
    }
  });
}

if (problems.length) {
  for (const p of problems) console.error(p);
  console.error('Test a class by its name instead, such as :has(.is-open).');
  process.exit(1);
}
console.log('ok: no :has() in the stylesheets tests the class or style attribute');
