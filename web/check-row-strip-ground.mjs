// A strip that floats over a list row wears the ROW'S ground, never a token of
// its own.
//
// THE PROBLEM IT EXISTS FOR, and it is the reason this is a script and not a
// note: the download list's row-end action strip has now been repainted three
// times against the same report ("der löschen button hat immer noch den dunklen
// Hintergrund bzw. rand", jdp). Each round picked a surface token that looked
// like the right one - `--carbon-surface` first, then `--carbon-surface2` - and
// a token is a SECOND OPINION about what the row underneath is painting. A link
// row hovers to `--carbon-hover` at half alpha over the card, which is neither
// of those, and in Rainbow mode it also carries a hue wash that no grey token
// has. Measured on the running instance, dark theme: the hovered row is
// rgb(45,45,45) and the strip meant to match it was rgb(57,57,57); light theme,
// rgb(239,239,239) against rgb(232,232,232) - too light in one theme and too
// dark in the other, from the same one token.
//
// So the rule is not "use this token" (the last two rounds were that rule, and
// both were wrong). It is: the row publishes what it paints as `--row-ground`,
// and anything lying on the row reads it. A variable cannot be a step off.
//
// WHAT COUNTS AS SUCH A STRIP: in a file that draws list ROWS (one carrying
// `data-row-key`), a class list that pins a FLEX run of controls over a row's
// full height at its trailing edge - `absolute`, `inset-y-*`, `end-*` and
// `flex` together. That is the shape both of this list's strips have, and it is
// the shape anybody copying one would reach for. A resize handle pinned the
// same way is not a run of controls and paints nothing, so it is not this; nor
// is a progress fill or an input's own stepper, which lie on a control rather
// than on a row.
//
// Run by hand or from CI: `node web/check-row-strip-ground.mjs`.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const src = join(here, 'src');

function sources(dir) {
  const found = [];
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) found.push(...sources(path));
    else if (/\.tsx?$/.test(entry)) found.push(path);
  }
  return found;
}

/** Every string literal and template chunk, with the line it starts on. */
function literals(text) {
  const out = [];
  const re = /(['"`])((?:\\.|(?!\1)[^\\])*)\1/gs;
  let m;
  while ((m = re.exec(text)) !== null) {
    out.push({ body: m[2], line: text.slice(0, m.index).split('\n').length });
  }
  return out;
}

const PINNED = /\babsolute\b/;
const FULL_HEIGHT = /\binset-y-(?:px|0)\b/;
const TRAILING = /\b(?:end|right)-[A-Za-z0-9.[\]]+/;
const CONTROLS = /\bflex\b/;
// Anchored so `bg-carbon-surface2/80` and `bg-carbon-hover/50` are caught too.
const OWN_FILL = /\bbg-carbon-[A-Za-z0-9]+(?:\/\d+)?/;
const ROW_GROUND = 'bg-[var(--row-ground)]';

let failed = 0;
for (const file of sources(src)) {
  const text = readFileSync(file, 'utf8');
  const where = relative(here, file).replace(/\\/g, '/');
  // Only a file that draws list rows can have a strip lying on one.
  const drawsRows = text.includes('data-row-key');
  for (const { body, line } of literals(text)) {
    if (!drawsRows) break;
    if (!PINNED.test(body) || !FULL_HEIGHT.test(body)) continue;
    if (!TRAILING.test(body) || !CONTROLS.test(body)) continue;
    const fill = body.match(OWN_FILL);
    if (fill) {
      failed++;
      console.error(
        `${where}:${line}  a strip pinned over a row paints ${fill[0]} of its own.\n` +
          `        It has to wear the row's ${ROW_GROUND}, or it will be a step off the row in some theme.`,
      );
      continue;
    }
    if (!body.includes(ROW_GROUND)) {
      failed++;
      console.error(
        `${where}:${line}  a strip pinned over a row paints no ground at all.\n` +
          `        Give it ${ROW_GROUND} so the cells it covers cannot read through it.`,
      );
    }
  }
  // A consumer of the variable needs a row that publishes it, or the strip is
  // painted with nothing and the check above passes on a page that is wrong.
  if (text.includes(ROW_GROUND) && !/\[--row-ground:/.test(text)) {
    failed++;
    console.error(`${where}  reads --row-ground but no row in this file ever sets it.`);
  }
}

if (failed > 0) {
  console.error(`\n${failed} row strip(s) painting their own ground.`);
  process.exit(1);
}
console.log('row strips: every one of them wears the row it lies on.');
