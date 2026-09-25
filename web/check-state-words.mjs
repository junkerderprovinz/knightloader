// A state badge says its state in a word or two, and the sentence behind it
// goes into the badge's (i).
//
// LabelBadge is the pill beside a name that reports where something stands:
// Online, Offline, Failed. A paragraph in that pill, the explanation and the
// fix together, stands where the eye expects a word to compare with the words
// on the cards beside it. The badge takes `tip` for the paragraph.
//
// Every translation key that reaches a LabelBadge's `label` is read: written
// into the attribute, or into the `const` the attribute names in the same file.
// Its English has to be short, and has to be a word rather than a sentence. A
// short question passes, since a badge such as "How does this work?" opens an
// explanation rather than reporting a state.
//
// Not seen: a label built by a helper function, such as health's
// healthLabel(), whose keys are read nowhere here.
//
// Run: `node web/check-state-words.mjs`.
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

const en = readFileSync(join(src, 'lib', 'locales', 'en.ts'), 'utf8');
const english = (key) => {
  const m = en.match(new RegExp(`^ {2}'${key.replaceAll('.', '\\.')}':\\s*\\n?\\s*'((?:[^'\\\\]|\\\\.)*)'`, 'm'));
  return m ? m[1] : null;
};

/** The words a state may take at most; "Waiting for a slot" is four. */
const MAX_WORDS = 4;

const problems = [];
let keys = 0;
for (const file of sources(src)) {
  const text = readFileSync(file, 'utf8');
  const where = relative(web, file).replaceAll('\\', '/');
  for (const m of text.matchAll(/<LabelBadge\b[\s\S]*?\/>/g)) {
    const label = /\blabel=\{([^}]*)\}/.exec(m[0])?.[1];
    if (!label) continue;
    let source = label;
    const named = /^\s*([A-Za-z_$][\w$]*)\s*$/.exec(label)?.[1];
    if (named) source = new RegExp(`const ${named} =[^;]*;`).exec(text)?.[0] ?? '';
    for (const k of source.matchAll(/\bt\('([\w.]+)'/g)) {
      const value = english(k[1]);
      if (value === null) continue;
      keys++;
      const words = value.split(/\s+/).length;
      if (words > MAX_WORDS || /[.!]\s|[.!]$/.test(value)) {
        const line = text.slice(0, m.index).split('\n').length;
        problems.push(`${where}:${line}: ${k[1]} is a sentence of ${words} words in a state badge; put it in the badge's tip`);
      }
    }
  }
}

if (keys < 5) problems.push(`found ${keys} keys behind state badges, expected at least 5`);

if (problems.length) {
  for (const p of problems) console.error(`✗ ${p}`);
  process.exit(1);
}
console.log(`ok: all ${keys} keys behind state badges are a word or two, with the sentences in their tips`);
