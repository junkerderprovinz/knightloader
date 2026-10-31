// A button that shows words wears a fill.
//
// The ghost kind draws no ground at all, so a ghost Button with words beside
// its glyph reads as a line of text with an icon in front: "Done" on the
// overview's needs card, Cancel in the windows and the copy buttons in the
// settings were all reported as "not recognisable as a button". A ghost Button
// may only be a glyph alone, in a row of its kind, such as a strip's close
// cross or the move up and down of a card being arranged.
//
// A Button is read from its opening tag to its end. It fails when it says
// kind="ghost" and either carries `labelled`, which prints its title as words,
// or has children, which are its words.
//
// Not checked: a kind chosen at run time, a lowercase <button> styled by hand,
// and the extension's own pages, which draw their buttons in plain CSS.
//
// Run: `node web/check-bare-buttons.mjs`.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const web = dirname(fileURLToPath(import.meta.url));
const src = join(web, 'src');

function* sources(dir) {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) yield* sources(p);
    else if (p.endsWith('.tsx') && !p.endsWith('.test.tsx')) yield p;
  }
}

/** The opening tag starting at i, up to the > that closes it outside braces. */
function openingTag(text, i) {
  let depth = 0;
  for (let j = i; j < text.length; j++) {
    const c = text[j];
    if (c === '{') depth++;
    else if (c === '}') depth--;
    else if (c === '>' && depth === 0) return text.slice(i, j + 1);
  }
  return text.slice(i);
}

const problems = [];
for (const file of sources(src)) {
  const text = readFileSync(file, 'utf8');
  for (const m of text.matchAll(/<Button\b/g)) {
    const tag = openingTag(text, m.index);
    if (!/\bkind="ghost"/.test(tag)) continue;
    const worded = /\slabelled(?=[\s=/>])/.test(tag) || !tag.endsWith('/>');
    if (!worded) continue;
    const line = text.slice(0, m.index).split('\n').length;
    problems.push(`${relative(web, file)}:${line}  a ghost Button with words; give it kind="secondary", or drop the words`);
  }
}

if (problems.length) {
  console.error(problems.join('\n'));
  console.error(`\n${problems.length} bare button(s) with words.`);
  process.exit(1);
}
console.log('ok: every Button that shows words has a fill');
