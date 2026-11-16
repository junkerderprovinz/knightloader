// A custom property the root sets inside a cascade layer has no unlayered root
// default that outranks it.
//
// Any declaration outside a layer beats every declaration inside one, whatever
// the specificity. With `--window-bar: 0px` outside the layers and the 2.5rem
// of `:root:has(.glim-window-bar)` inside them, the desktop window bar
// collapses to nothing, and a `[dir="rtl"]` sign inside a layer never flips a
// tab's slide. Neither shows as an error, only as a page that starts in the
// wrong place or a tab that slides the wrong way.
//
// Both stylesheets keep their root tokens outside every layer, the window
// bar's default and its override included. The risk is a rule copied from
// GlimStone's tokens.css with its `@layer components` around it while the
// default here stays unlayered.
//
// What counts as the root: a selector that can land on the document element,
// `:root`, `html` or a bare attribute selector like `[dir="rtl"]`, with no
// combinator. A property set further down the tree is a different cascade.
//
// Not seen: what Tailwind writes into web/dist. Its `@layer properties` holds
// the fallback a browser without `@property` starts from, which is meant to
// lose to the app's own unlayered values.
//
// Run: `node web/check-root-layers.mjs`.
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const sheets = ['web/src/index.css', 'extension/src/glimstone.css'];

const onRoot = (selector) => /^(:root|html|\[)/.test(selector) && !/[\s>+~]/.test(selector);

// A selector list split at its top-level commas, leaving those inside :not() and :is() alone.
function selectors(list) {
  const parts = [''];
  let depth = 0;
  for (const ch of list) {
    if (ch === '(') depth++;
    if (ch === ')') depth--;
    if (ch === ',' && depth === 0) parts.push('');
    else parts[parts.length - 1] += ch;
  }
  return parts.map((s) => s.trim());
}

function rootDeclarations(css) {
  const layered = new Set();
  const unlayered = new Set();
  const open = [];
  let text = '';
  for (const ch of css.replace(/\/\*[\s\S]*?\*\//g, '')) {
    if (ch === '{') {
      open.push(text.trim());
      text = '';
    } else if (ch === '}') {
      open.pop();
      text = '';
    } else if (ch === ';') {
      const name = /^\s*(--[\w-]+)\s*:/.exec(text)?.[1];
      const selector = open.filter((s) => !s.startsWith('@')).at(-1);
      if (name && selector && selectors(selector).some(onRoot)) {
        (open.some((s) => s.startsWith('@layer')) ? layered : unlayered).add(name);
      }
      text = '';
    } else text += ch;
  }
  return { layered, unlayered };
}

let failed = 0;
for (const sheet of sheets) {
  const { layered, unlayered } = rootDeclarations(readFileSync(join(root, sheet), 'utf8'));
  for (const name of layered) {
    if (!unlayered.has(name)) continue;
    console.error(`${sheet}: ${name} is set on the root both inside a cascade layer and outside one, so the unlayered value always wins. Put both in the same layer.`);
    failed++;
  }
}
if (failed) process.exit(1);
console.log(`check-root-layers: no root token in ${sheets.join(' or ')} is shadowed by an unlayered default.`);
