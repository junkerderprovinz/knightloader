// GlimStone rule 21: whatever the pointer is over lights up in the accent.
//
// `--hover` is one colour for a button, a row, a menu entry and a tile: the
// lightness of surface3 in the accent's hue, which lifts a row off the card and
// a filled control off surface2 while the text on it keeps 4.5:1
// (components/hoverTint.test.ts measures it). A grey step under the pointer is
// the old ramp, and beside a lit button it reads as a second, duller hover. The
// grey steps stay for what is not a hover, a field's focus above all.
//
// So a component names one hover ground, `hover:bg-hover`. What the pointer
// lights in a colour of its own may keep it: the accent and state fills step
// along their own colour (`hover:bg-accentHover`, `hover:bg-statusFailHover`),
// a mix of the accent or a state hue, and `hover:bg-transparent`. A disabled
// control keeps its resting fill. A row that publishes its ground as a custom
// property, `hover:[--row-ground:…]`, names `var(--hover)` there.
//
// It looks inside each string literal, so a class list split across two
// literals joined with `+` is read as two, which changes nothing here since
// every hover class is judged on its own.
//
// Run: `node web/check-hover-ramp.mjs`.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const src = join(dirname(fileURLToPath(import.meta.url)), 'src');

function sources(dir) {
  const found = [];
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) found.push(...sources(path));
    else if (/\.tsx?$/.test(entry) && !/\.test\.tsx?$/.test(entry)) found.push(path);
  }
  return found;
}

const files = sources(src);
if (files.length < 20) {
  console.error(`check-hover-ramp: only ${files.length} source files found, wrong directory?`);
  process.exit(1);
}

// A background under the pointer or the keyboard's hover, behind any variants.
const HOVER_BG = /(?<![\w-])((?:[\w-]+(?:\[[^\]\s]*\])?:)*(?:group-)?(?:hover|has-\[:focus-visible\]):bg-(?:\[[^\]\s]+\]|[\w/-]+))/g;
const LIT = /:bg-(hover|transparent|accent[\w]*|status\w*Hover|\[color-mix\(in_srgb,var\(--(status|accent|item-hue)-[^\]]+\])$/;
const ROW_GROUND = /(?<![\w-])((?:[\w-]+:)*hover:\[--[\w-]+:([^\]\s]+)\])/g;

const problems = [];
let seen = 0;
for (const path of files) {
  const text = readFileSync(path, 'utf8');
  const at = (index) => `${path.slice(src.length + 1).split('\\').join('/')}:${text.slice(0, index).split('\n').length}`;
  for (const m of text.matchAll(HOVER_BG)) {
    seen++;
    if (/(^|:)disabled:/.test(m[1]) || LIT.test(m[1])) continue;
    problems.push(`${at(m.index)} ${m[1]} -> ${m[1].replace(/bg-(?:\[[^\]]+\]|[\w/-]+)$/, 'bg-hover')}`);
  }
  for (const m of text.matchAll(ROW_GROUND)) {
    if (m[2].replace(/_/g, ' ') !== 'var(--hover)') problems.push(`${at(m.index)} ${m[1]} -> var(--hover)`);
  }
}
problems.sort();

if (seen < 20) {
  console.error(`check-hover-ramp: only ${seen} hover grounds found, has the class syntax changed?`);
  process.exit(1);
}
if (problems.length) {
  console.error(`check-hover-ramp: ${problems.length} hover(s) to a grey step instead of the accent's --hover:`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}
console.log(`check-hover-ramp: ${files.length} files, every one of ${seen} hover grounds lights up in the accent.`);
