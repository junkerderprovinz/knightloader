// Whatever the pointer is over lights up in the accent (GlimStone rule 21), as
// in the web UI: one --hover for a button, a row, a menu entry and a tile.
//
//   token    glimstone.css declares --hover as the web UI does, surface3's
//            lightness in the accent's hue, again inside a rainbow position
//            and one step down under OLED.
//   grounds  Every hover in glimstone.css, in the pages' own <style> and in
//            the video button's shadow stylesheet paints the accent's hover,
//            the accent or a state hue, or nothing. A grey step under the
//            pointer is the old ramp, and beside a lit button it reads as a
//            second, duller hover.
//
// Run by CI and by hand: `node extension/check-hover.mjs`.
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const read = (...p) => readFileSync(join(here, ...p), 'utf8');
const problems = [];

const strip = (css) => css.replace(/\/\*[\s\S]*?\*\//g, '');
const rules = (css) => [...strip(css).matchAll(/([^{}]+)\{([^{}]*)\}/g)].map(([, s, body]) => ({ selector: s.split(';').pop().trim(), body }));

const sheet = read('src', 'glimstone.css');
const tint = (l) => `oklab(from var(--accent) ${l} calc(a * var(--hover-chroma)) calc(b * var(--hover-chroma)))`;
const own = (selector) => rules(sheet).find((r) => r.selector === selector)?.body ?? '';
if (!own(':root,\n[data-theme],\n[data-rainbow] .glim-hue').includes(`--hover: light-dark(${tint(0.861)}, ${tint(0.432)});`)) {
  problems.push('glimstone.css: --hover is not the web UI\'s, surface3\'s lightness in the accent\'s hue, declared again in a rainbow position');
}
if (!own('[data-oled],\n[data-oled][data-rainbow] .glim-hue').includes(`--hover: light-dark(${tint(0.861)}, ${tint(0.338)});`)) {
  problems.push('glimstone.css: OLED does not move --hover one step down');
}

const styles = (html) => [...html.matchAll(/<style>([\s\S]*?)<\/style>/g)].map((m) => m[1]).join('\n');
const sources = {
  'glimstone.css': sheet,
  'options.html': styles(read('src', 'options.html')),
  'popup.html': styles(read('src', 'popup.html')),
  'video-button.js': read('src', 'video-button.js').match(/const STYLE = `([\s\S]*?)`;/)?.[1] ?? '',
};
const LIT = /var\(--(hover|accent[\w-]*|item-hue[\w-]*|tile[\w-]*|brand[\w-]*|status-[\w-]+)\)|^color-mix\(in srgb, var\(--accent\)/;
// What stands on a lit row or tile keeps its own step above it: a switch's off
// track and a size plaque. A tip drawn in ::after is a bubble, not a ground.
const ON_THE_LIT = /\.glim-switch-track$|\.plaque$|::(after|before)$/;
let seen = 0;
for (const [name, css] of Object.entries(sources)) {
  if (!css) problems.push(`${name}: no stylesheet found to read`);
  for (const { selector, body } of rules(css)) {
    if (!/:hover/.test(selector.replace(/:not\([^()]*\)/g, ''))) continue;
    if (selector.split(',').every((s) => ON_THE_LIT.test(s.trim()))) continue;
    for (const m of body.matchAll(/(?<![\w-])background(?:-color)?\s*:\s*([^;]+)/g)) {
      seen++;
      const value = m[1].trim();
      if (value === 'transparent' || value === 'none' || LIT.test(value)) continue;
      problems.push(`${name}: ${selector.replace(/\s+/g, ' ')} hovers to ${value}, not var(--hover)`);
    }
  }
}
if (seen < 10) problems.push(`only ${seen} hover grounds found, has a stylesheet moved?`);

if (problems.length) {
  console.error(`${problems.length} problem(s):`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}
console.log(`ok: all ${seen} hover grounds in the extension light up in the accent`);
