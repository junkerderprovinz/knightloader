// A stretched plot may not decide how tall its card is.
//
// WHY THIS EXISTS. `preserveAspectRatio="none"` is how this app draws a curve
// into a box of any shape: the viewBox stops being a size and becomes a
// coordinate system, so one path describes a wide flat band as happily as a
// square. What it does NOT do is stop the svg having an intrinsic aspect ratio.
// An svg with a viewBox and no height is a replaced element whose height
// follows its width, and inside a flex column that is a height the parent then
// grows to fit. The wider the card, the taller the card.
//
// That is not a theory. The Downloads head card (app/Layout.tsx's ShellBar) was
// measured on a live instance carrying exactly this defect, in a build where
// everything else in the card was flat:
//
//     window 1280x720   card 200px   curve 481x130
//     window 1400x900   card 229px   curve 601x163
//     window 1920x1080  card 369px   curve 1121x303
//
// The card's own content - three transport buttons stacked, 136px - never
// changed. The entire growth was the curve's viewBox ratio (148:40) turning the
// width it had been given into a height nobody asked for, and the taller the
// window the worse it reads: at 1920 a status bar was eating a third of the
// window. jdp reported it twice ("die kopfcard im downloadtab ist immer noch
// viel zu hoch!!"), and a round that went looking for wrapped text in the bar
// found nothing, because the height was never text.
//
// THE RULE. An svg that stretches must be handed a height. Any of these count,
// and they are the three honest ways to say it:
//
//   * `style={{ height }}` or a `height=` attribute - a size chosen by the
//     caller, which is what the Overview hero does.
//   * `h-0` beside `flex-auto` - no height of its own, grow into what is left.
//     This is what a plot that fills a card's height needs: the card decides,
//     the curve follows, and a wider card is not a taller one.
//   * `h-full` - take the parent's height, which the parent had better have.
//
// AND THE HEIGHT ALONE IS NOT ENOUGH, which is the part that cost an afternoon.
// `flex-1` is `flex: 1 1 0%`, and a PERCENTAGE basis against a column whose
// height is not definite is not zero - it falls back to content sizing, and for
// a replaced element with a ratio "content" means the width again. `h-0 flex-1`
// was measured doing exactly nothing: card 229px, curve 601x163, the same
// numbers as with no height at all. `flex-auto` is `flex: 1 1 auto`, which
// takes its basis from the height property, so there the `h-0` is read. So a
// stretched svg may not carry `flex-1` unless it also states a basis in real
// units (`basis-0`, `basis-[26px]`).
//
// WHAT IT CANNOT SEE. Whether the height it ends up with is the RIGHT one, and
// whether some other child of the same card is the tall one instead. This
// checks one specific way a card grows without anybody choosing to make it
// grow; it is not a layout test, and there is no layout test in this gate to
// hide behind.
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
