// A button that shows words wears a fill (rule 38).
//
// A button without a ground and with words beside its glyph reads as a line of
// text with an icon in front. A bare button may only be a glyph alone, in a
// toolbar or a list row.
//
// Each <Button> is read from its opening tag to its end. It fails when the tag
// carries the bare kind and either a prop that prints its title as words, or
// children, which are its words.
//
// Not checked: a kind chosen at run time, a lowercase <button> styled by hand,
// and pages that draw their buttons in plain CSS.
//
// Run: node check-bare-buttons.mjs [--bare='kind="ghost"'] [--worded=labelled] <dir>...
// Every .tsx under each directory is read, test files aside. --bare is the
// attribute that makes a Button bare, and --worded a comma-separated list of
// props that show words without children.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';

const flags = new Map();
const dirs = [];
for (const arg of process.argv.slice(2)) {
  const m = /^--(bare|worded)=(.*)$/.exec(arg);
  if (m) flags.set(m[1], m[2]);
  else dirs.push(arg);
}
if (!dirs.length) {
  console.error('usage: node check-bare-buttons.mjs [--bare=ATTR] [--worded=PROP,...] <dir>...');
  process.exit(2);
}
const bare = flags.get('bare') ?? 'kind="ghost"';
const worded = (flags.get('worded') ?? 'labelled').split(',').filter(Boolean);
const escape = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
const bareRe = new RegExp(`\\s${escape(bare)}`);
const wordedRe = worded.length ? new RegExp(`\\s(?:${worded.map(escape).join('|')})(?=[\\s=/>])`) : null;

function* sources(dir) {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) {
      if (name !== 'node_modules') yield* sources(p);
    } else if (p.endsWith('.tsx') && !p.endsWith('.test.tsx')) yield p;
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
for (const dir of dirs) {
  for (const file of sources(dir)) {
    const text = readFileSync(file, 'utf8');
    for (const m of text.matchAll(/<Button\b/g)) {
      const tag = openingTag(text, m.index);
      if (!bareRe.test(tag)) continue;
      if (!wordedRe?.test(tag) && tag.endsWith('/>')) continue;
      const line = text.slice(0, m.index).split('\n').length;
      problems.push(`${relative(process.cwd(), file)}:${line}  a bare Button with words; give it a fill, or drop the words`);
    }
  }
}

if (problems.length) {
  console.error(problems.join('\n'));
  console.error(`\n${problems.length} bare button(s) with words.`);
  process.exit(1);
}
console.log('ok: every Button that shows words has a fill');
