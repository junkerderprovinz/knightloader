// A stretched plot may not decide how tall its card is.
//
// `preserveAspectRatio="none"` turns the viewBox into a coordinate system, so
// one path draws a wide flat band as happily as a square. It does not stop the
// svg having an intrinsic aspect ratio: an svg with a viewBox and no height is
// a replaced element whose height follows its width, and in a flex column the
// parent grows to fit it. The Downloads head card was measured doing exactly
// that, 200px tall in a 1280px window and 369px in a 1920px one, while its own
// content stayed at 136px.
//
// So a stretched svg has to be handed a height, in one of three ways:
//
//   * `style={{ height }}` or a `height=` attribute, a size the caller picks,
//     as the Overview hero does.
//   * `h-0` beside `flex-auto`: no height of its own, grow into what is left.
//     The card decides, the curve follows, and a wider card is not a taller one.
//   * `h-full`, the parent's height, which the parent had better have.
//
// The height alone is not enough. `flex-1` is `flex: 1 1 0%`, and a percentage
// basis against a column whose height is not definite falls back to content
// sizing, which for a replaced element with a ratio means the width again;
// `h-0 flex-1` was measured changing nothing at all. `flex-auto` is
// `flex: 1 1 auto` and takes its basis from the height property, so there the
// `h-0` is read. A stretched svg may carry `flex-1` only beside a basis in real
// units (`basis-0`, `basis-[26px]`).
//
// Not checked: whether the height it ends up with is the right one, or whether
// another child of the same card is the tall one.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';

const ROOT = new URL('./src/', import.meta.url).pathname.replace(/^\/([A-Za-z]:)/, '$1');

function sources(dir) {
  const out = [];
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) out.push(...sources(p));
    else if (name.endsWith('.tsx') || name.endsWith('.ts')) out.push(p);
  }
  return out;
}

/** The whole `<svg ...>` opening tag around a given offset, attributes only. */
function openingTag(text, at) {
  const start = text.lastIndexOf('<svg', at);
  if (start < 0) return null;
  // The first '>' that is not inside a brace expression or a quoted value.
  let depth = 0;
  let quote = '';
  for (let i = start; i < text.length; i++) {
    const c = text[i];
    if (quote) {
      if (c === quote) quote = '';
      continue;
    }
    if (c === '"' || c === "'") quote = c;
    else if (c === '{') depth++;
    else if (c === '}') depth--;
    else if (c === '>' && depth === 0) return { start, tag: text.slice(start, i + 1) };
  }
  return null;
}

const HEIGHT = [
  /\bheight\s*=/, // height="40" or height={h}
  /style=\{\{[^}]*\bheight\b/, // style={{ height }}
  /className="[^"]*\bh-(?:0|full|\[[^\]]+\])\b/s, // h-0 / h-full / h-[26px]
  /className=\{`[^`]*\bh-(?:0|full|\[[^\]]+\])\b/s,
];

/** `flex-1` is a percentage basis; only a real basis beside it rescues one. */
const PERCENT_BASIS = /\bflex-1\b/;
const REAL_BASIS = /\bbasis-(?:0|px|\[[^\]]+\])\b/;

const bad = [];
for (const file of sources(ROOT)) {
  const text = readFileSync(file, 'utf8');
  for (const m of text.matchAll(/preserveAspectRatio="none"/g)) {
    const tag = openingTag(text, m.index);
    if (!tag) continue;
    const line = text.slice(0, tag.start).split('\n').length;
    const rel = file.replace(/\\/g, '/').replace(ROOT.replace(/\\/g, '/'), 'src/');
    const where = `${rel}:${line}\n    ${tag.tag.replace(/\s+/g, ' ').slice(0, 160)}`;
    if (!HEIGHT.some((re) => re.test(tag.tag))) {
      bad.push(`${where}\n    -> no height of its own, so its viewBox ratio picks one.`);
      continue;
    }
    if (PERCENT_BASIS.test(tag.tag) && !REAL_BASIS.test(tag.tag)) {
      bad.push(
        `${where}\n    -> flex-1 is a percentage basis, which falls back to the ratio and` +
          `\n       swallows the height beside it. flex-auto, or basis-0.`,
      );
    }
  }
}

if (bad.length > 0) {
  console.error(
    'A stretched svg (preserveAspectRatio="none") that sizes itself:\n\n' +
      bad.map((b) => `  ${b}`).join('\n\n') +
      '\n\nIts viewBox ratio will turn the width it is given into a height, and the\n' +
      'box around it grows to fit. Give it `style={{ height }}`, or `h-0` beside\n' +
      '`flex-auto` so it grows into the room its parent already has.\n',
  );
  process.exit(1);
}

console.log(`ok: every stretched svg carries a height (${sources(ROOT).length} files)`);
