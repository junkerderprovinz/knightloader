// Every page a server sentence sends the reader to is called what the rail
// calls it.
//
// The server writes refusals, module lines and route descriptions in English,
// and some of them send the reader somewhere: "create one on the Remote access
// page". The interface names its pages from the locale files, so a sentence
// that uses an old or invented name ("the Access page", "the General tab")
// points at a page nobody can find. This reads every string literal in the Go
// source outside tests and checks each page it names against the English page
// titles. The settings have no tabs, so "tab" is always the wrong word.
//
// Run: `node web/check-server-page-names.mjs`
import { readFileSync, readdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join, relative } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const root = join(here, '..');

const en = readFileSync(join(here, 'src', 'lib', 'locales', 'en.ts'), 'utf8');
const titles = new Set(
  [...en.matchAll(/^\s*'(?:settings\.)?nav\.\w+':\s*(['"])(.*?)\1,\s*$/gm)].map((m) => m[2]),
);

function* goFiles(dir) {
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    const p = join(dir, e.name);
    if (e.isDirectory()) yield* goFiles(p);
    else if (e.name.endsWith('.go') && !e.name.endsWith('_test.go')) yield p;
  }
}

const PAGE = /\bthe ([A-Z][\w'&-]*(?: [\w'&-]+)*?) (page|tab)\b/g;
const MENU = /\bSettings\s*>\s*([A-Z][\w&]*(?: [a-z][\w&]*)*)/g;

const problems = [];
let named = 0;
for (const dir of ['internal', 'cmd', 'desktop']) {
  for (const file of goFiles(join(root, dir))) {
    const lines = readFileSync(file, 'utf8').split('\n');
    lines.forEach((text, i) => {
      if (text.trimStart().startsWith('//')) return;
      for (const [, literal] of text.matchAll(/"((?:[^"\\]|\\.)*)"/g)) {
        const at = `${relative(root, file).replaceAll('\\', '/')}:${i + 1}`;
        for (const [, name, kind] of literal.matchAll(PAGE)) {
          named++;
          if (kind === 'tab') problems.push(`${at} calls "${name}" a tab; the settings have pages`);
          else if (!titles.has(name)) problems.push(`${at} names "the ${name} page", and no page is called "${name}"`);
        }
        for (const [, name] of literal.matchAll(MENU)) {
          named++;
          if (!titles.has(name)) problems.push(`${at} names "Settings > ${name}", and no page is called "${name}"`);
        }
      }
    });
  }
}

if (titles.size === 0) problems.push('no page titles found in en.ts; the pattern above does not match the locale file');
if (problems.length) {
  console.error('Server sentences that name a page the interface does not have:\n' + problems.map((p) => '  ' + p).join('\n'));
  process.exit(1);
}
console.log(`Server page names: ${named} mentions, each a page the rail shows.`);
