// A strip of glyphs follows the Beschriftung setting like every badge does.
//
// Beschriftung decides whether a control shows its glyph, its words or both,
// and the badges, the sidebar and the settings rail all follow it. A Tabs strip
// of glyphs that keeps its words in the glyph-only mode, such as the
// end-of-queue action in quick settings, is the one row on the screen that
// does not change. Tabs follows the setting once the strip says `labelled`, or
// is handed a `display` of its own, as the settings rail is.
//
// Every <Tabs> in web/src whose items name an `icon` is read, up to the end of
// its tag. A strip without glyphs has nothing to hide and is left out.
//
// Run: `node web/check-strip-labels.mjs`.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const web = dirname(fileURLToPath(import.meta.url));
const src = join(web, 'src');

function sources(dir) {
  const found = [];
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) found.push(...sources(path));
    else if (/\.tsx$/.test(entry)) found.push(path);
  }
  return found;
}

/** The tag from `<Tabs` to its `/>` or `>`, skipping what sits inside braces. */
function tagAt(text, start) {
  let depth = 0;
  for (let i = start; i < text.length; i++) {
    const c = text[i];
    if (c === '{') depth++;
    else if (c === '}') depth--;
    else if (depth === 0 && c === '>') return text.slice(start, i + 1);
  }
  return null;
}

const problems = [];
let strips = 0;
for (const file of sources(src)) {
  const text = readFileSync(file, 'utf8');
  const where = relative(web, file).replaceAll('\\', '/');
  for (const m of text.matchAll(/<Tabs\b/g)) {
    const tag = tagAt(text, m.index);
    if (!tag || !/\bicon\s*[:=]/.test(tag)) continue;
    strips++;
    if (!/\blabelled\b|\bdisplay=/.test(tag)) {
      const line = text.slice(0, m.index).split('\n').length;
      problems.push(`${where}:${line}: a strip of glyphs that ignores Beschriftung; give it \`labelled\``);
    }
  }
}

if (strips < 3) problems.push(`found ${strips} strips of glyphs, expected at least 3`);

if (problems.length) {
  for (const p of problems) console.error(`✗ ${p}`);
  process.exit(1);
}
console.log(`ok: all ${strips} strips of glyphs follow Beschriftung`);
