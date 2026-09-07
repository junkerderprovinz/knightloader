// The settings rail and the command palette have to name the same pages.
//
// They are two hand-kept lists of the same set: registry.tsx's PAGES map says
// which sub-pages have a component, and lib/commands/settings.ts says which
// ones the palette can jump to. Nothing enforced that they agree, and by
// 2026-09-07 three pages had drifted out of the palette (accounts, instances,
// appearance) while the comment there still explained why one of them had no
// component yet.
//
// The drift is invisible from the outside: a missing command looks exactly
// like a command nobody typed, and a stale one only shows up when somebody
// picks it and lands on the "not built yet" placeholder. So it gets a check
// rather than a promise in a comment.
//
// Run by CI and by hand: `node web/check-settings-pages.mjs`.

import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));

/** Everything registry.tsx maps to a component: `  <id>: () => <Something />,`. */
function registryPages() {
  const src = readFileSync(join(here, 'src', 'pages', 'settings', 'registry.tsx'), 'utf8');
  const start = src.indexOf('PAGES');
  if (start === -1) throw new Error('PAGES not found in registry.tsx');
  // Only the map's own entries: two spaces of indent, an id, then an arrow
  // function. Deeper indentation belongs to something nested inside a page.
  return new Set([...src.slice(start).matchAll(/^ {2}([a-z][a-zA-Z]*): \(\) =>/gm)].map((m) => m[1]));
}

/** Everything the palette offers: `{ id: '<id>', labelKey: ... }`. */
function palettePages() {
  const src = readFileSync(join(here, 'src', 'lib', 'commands', 'settings.ts'), 'utf8');
  const start = src.indexOf('SETTINGS_PAGES');
  if (start === -1) throw new Error('SETTINGS_PAGES not found in commands/settings.ts');
  return new Set([...src.slice(start).matchAll(/\{ id: '([a-z][a-zA-Z]*)'/g)].map((m) => m[1]));
}

const registry = registryPages();
const palette = palettePages();

// A fixture that reads nothing is a check that passes for the wrong reason.
if (registry.size < 5) throw new Error(`only ${registry.size} pages parsed out of registry.tsx - the parser is wrong, not the code`);
if (palette.size < 5) throw new Error(`only ${palette.size} pages parsed out of commands/settings.ts - the parser is wrong, not the code`);

const problems = [];
for (const id of registry) {
  if (!palette.has(id)) problems.push(`${id} has a component but no palette command - it cannot be reached by typing its name`);
}
for (const id of palette) {
  if (!registry.has(id)) problems.push(`${id} has a palette command but no component - picking it lands on the placeholder`);
}

if (problems.length > 0) {
  console.error(`${problems.length} settings page(s) out of step:`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}
console.log(`ok: ${registry.size} settings pages, rail and palette agree`);
