// The wheel changes a value only on a control that has focus.
//
// A dropdown or a number field that answers the wheel while the pointer merely
// rests on it changes whatever the page is scrolled past, such as a link's
// variant on the collector, which goes to the server at once. The page stops
// scrolling as well, since the listener has to call preventDefault.
// Asking for focus first means the control was entered on purpose, the same
// gesture that already hands it the arrow keys. GlimStone's number-field rule
// says so; its rule 14 still describes a closed <select> stepping on hover.
//
// Every listener registered with `{ passive: false }` for 'wheel' is one that
// can take the wheel away from the page, so each of them has to compare
// document.activeElement with its control. The handler is read where it is
// written: inline in the call, or a function or const of that name in the same
// file. The web UI and the extension are both read, since the extension's
// language picker is a dropdown too.
//
// Not seen: a handler imported from another file, which reads as missing and
// fails, and a focus test written some other way, which fails the same way.
//
// Run: `node web/check-wheel-focus.mjs`.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const web = dirname(fileURLToPath(import.meta.url));
const roots = [join(web, 'src'), join(web, '..', 'extension', 'src')];

function sources(dir) {
  const found = [];
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) found.push(...sources(path));
    else if (/\.(tsx?|js)$/.test(entry)) found.push(path);
  }
  return found;
}

/** The text from `open` to its matching closer, both included. */
function balanced(text, open, pair) {
  const [o, c] = pair;
  let depth = 0;
  for (let i = open; i < text.length; i++) {
    if (text[i] === o) depth++;
    else if (text[i] === c && --depth === 0) return text.slice(open, i + 1);
  }
  return null;
}

/** The body of the function or const called `name` in `text`. */
function definition(text, name) {
  const at = text.search(new RegExp(`(function ${name}\\s*\\(|const ${name}\\s*=)`));
  if (at < 0) return null;
  const brace = text.indexOf('{', at);
  return brace < 0 ? null : balanced(text, brace, '{}');
}

const problems = [];
let seen = 0;
for (const file of roots.flatMap(sources)) {
  const text = readFileSync(file, 'utf8');
  const where = relative(join(web, '..'), file).replaceAll('\\', '/');
  for (const m of text.matchAll(/addEventListener\(/g)) {
    const call = balanced(text, m.index + m[0].length - 1, '()');
    if (!call || !/^\(\s*'wheel'\s*,/.test(call) || !/passive:\s*false/.test(call)) continue;
    seen++;
    const line = text.slice(0, m.index).split('\n').length;
    const named = /^\(\s*'wheel'\s*,\s*([A-Za-z_$][\w$]*)\s*,/.exec(call);
    const body = named ? definition(text, named[1]) : call;
    if (!body) problems.push(`${where}:${line}: the handler ${named[1]} is not defined in this file`);
    else if (!body.includes('document.activeElement')) {
      problems.push(`${where}:${line}: takes the wheel without asking whether its control has focus`);
    }
  }
}

// A rename of the listeners would otherwise pass this check by finding none.
if (seen < 3) problems.push(`found ${seen} wheel listeners that take the wheel from the page, expected at least 3`);

if (problems.length) {
  for (const p of problems) console.error(`✗ ${p}`);
  process.exit(1);
}
console.log(`ok: all ${seen} wheel listeners that stop the page scrolling act only on a control with focus`);
