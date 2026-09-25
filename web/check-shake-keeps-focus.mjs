// A refusal shakes the control in place and leaves the focus on it.
//
// The shake has to play again on a second identical refusal, and an animation
// at rest does not restart when its class leaves and comes back in one frame.
// Keying the element on the failure counter restarts it by mounting a new
// node, and the old node takes the keyboard focus with it: somebody who pressed
// Enter on a refused button lands back at the top of the page. lib/useShake.ts
// restarts the animation on the same node, and Button, IconBadge and Dropdown
// take the counter as `shake`.
//
// Two spellings of the old way are caught in web/src: a `key` built from a
// shake counter, and the glim-shake class written into a call site, which only
// replays when something remounts the element. useShake.ts itself adds the
// class and is the one file allowed to name it.
//
// Run: `node web/check-shake-keeps-focus.mjs`.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const web = dirname(fileURLToPath(import.meta.url));
const src = join(web, 'src');
const OWNER = join(src, 'lib', 'useShake.ts');

function sources(dir) {
  const found = [];
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) found.push(...sources(path));
    else if (/\.tsx?$/.test(entry)) found.push(path);
  }
  return found;
}

const problems = [];
let counters = 0;
for (const file of sources(src)) {
  const text = readFileSync(file, 'utf8');
  const where = relative(web, file).replaceAll('\\', '/');
  const lineOf = (i) => text.slice(0, i).split('\n').length;
  counters += (text.match(/shake=\{/g) || []).length + (text.match(/useShake</g) || []).length;
  for (const m of text.matchAll(/\bkey=\{[^}]*[sS]hake/g)) {
    problems.push(`${where}:${lineOf(m.index)}: keyed on a shake counter, which remounts the control and drops its focus`);
  }
  if (file === OWNER) continue;
  for (const m of text.matchAll(/'[^'\n]*\bglim-shake\b[^'\n]*'|`[^`]*\bglim-shake\b[^`]*`|"[^"\n]*\bglim-shake\b[^"\n]*"/g)) {
    problems.push(`${where}:${lineOf(m.index)}: sets glim-shake itself; pass the counter as shake or use useShake`);
  }
}

// Every refusal that shakes goes through `shake=` or useShake, so finding
// hardly any means the patterns have stopped matching the code.
if (counters < 20) problems.push(`found ${counters} shake counters handed to a control, expected at least 20`);

if (problems.length) {
  for (const p of problems) console.error(`✗ ${p}`);
  process.exit(1);
}
console.log(`ok: all ${counters} shake counters restart the animation in place, and none remounts a control`);
