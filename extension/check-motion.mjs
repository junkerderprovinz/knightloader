// The motion engine's promises, read from the stylesheet itself.
//
//   dials     Every level declares every dial: subtle, off and storm, and the
//             bare :root, which is the lively level. A level that misses one
//             quietly runs the lively number, and nothing on screen says so
//             until somebody compares two levels side by side.
//   wild      No rule names the lively level. `[data-motion='wild']` leaves
//             out storm, which wants more motion rather than none, so a rule
//             names the quiet levels instead.
//   level     Every page names its level on <html>. The extension has no motion
//             setting, and without the attribute the bare :root, the lively
//             level, would apply rather than GlimStone's default.
//
// A dial is any --motion-* or --drag-* property one of the levels sets, so a
// dial added to one level and forgotten in another is caught the day it lands.
//
// Run by CI and by hand: `node extension/check-motion.mjs`.
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const read = (...p) => readFileSync(join(here, ...p), 'utf8');
const problems = [];
const fail = (why) => problems.push(why);

const css = read('src', 'glimstone.css').replace(/\/\*[\s\S]*?\*\//g, '');
const pages = ['options.html', 'popup.html'];

const LEVELS = ['subtle', 'off', 'storm'];
const DIAL = /(--(?:motion|drag)-[a-z-]+)\s*:/g;

/** The dials declared in every innermost rule whose selector is exactly `selector`. */
function declared(selector) {
  const names = new Set();
  for (const rule of css.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    if (rule[1].trim().replace(/"/g, "'") !== selector) continue;
    for (const dial of rule[2].matchAll(DIAL)) names.add(dial[1]);
  }
  return names;
}

// dials
const byLevel = Object.fromEntries(LEVELS.map((l) => [l, declared(`:root[data-motion='${l}']`)]));
const dials = new Set(LEVELS.flatMap((l) => [...byLevel[l]]));
if (dials.size < 15) {
  fail(`src/glimstone.css: only ${dials.size} motion dials found across the levels - the level blocks are missing or were renamed`);
}
for (const level of LEVELS) {
  const missing = [...dials].filter((d) => !byLevel[level].has(d));
  if (missing.length) fail(`src/glimstone.css: ${level} does not declare ${missing.join(', ')}, so it borrows the bare :root's number`);
}
{
  const root = declared(':root');
  const missing = [...dials].filter((d) => !root.has(d));
  if (missing.length) fail(`src/glimstone.css: the bare :root, the lively level, does not declare ${missing.join(', ')}`);
}

// wild
for (const [name, text] of [['src/glimstone.css', css], ...pages.map((p) => [`src/${p}`, read('src', p)])]) {
  if (/data-motion\s*=\s*['"]?wild\b/.test(text)) {
    fail(`${name}: a rule names the lively level, which leaves storm out - name the quiet levels instead`);
  }
}

// level
for (const page of pages) {
  const html = read('src', page).match(/<html\b[^>]*>/)?.[0] ?? '';
  if (!/\sdata-motion="subtle"/.test(html)) {
    fail(`src/${page}: <html> carries no data-motion="subtle", so the page runs at the lively level`);
  }
}

if (problems.length) {
  for (const p of problems) console.error(`✗ ${p}`);
  process.exit(1);
}
console.log(
  `ok: subtle, off, storm and the bare :root each declare all ${dials.size} motion dials, ` +
    'no rule names the lively level, and every page runs at subtle',
);
