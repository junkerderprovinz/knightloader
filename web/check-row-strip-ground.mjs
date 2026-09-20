// A strip that floats over a list row wears the row's ground, never a token of
// its own.
//
// A surface token is a second opinion about what the row underneath paints. A
// link row hovers to `--carbon-hover` at half alpha over the card, which no
// surface token matches, and in Rainbow mode it carries a hue wash that no grey
// token has: measured in the dark theme, the hovered row is rgb(45,45,45) and
// the strip meant to match it rgb(57,57,57); in the light theme rgb(239,239,239)
// against rgb(232,232,232). So the row publishes what it paints as
// `--row-ground` and anything lying on it reads that.
//
// Such a strip is, in a file that draws list rows (one carrying
// `data-row-key`), a class list that pins a flex run of controls over a row's
// full height at its trailing edge: `absolute`, `inset-y-*`, `end-*` and `flex`
// together. A resize handle pinned the same way paints nothing, and a progress
// fill or an input's stepper lies on a control rather than on a row.
//
// Run: `node web/check-row-strip-ground.mjs`.
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
