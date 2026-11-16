// A custom property the root sets inside a cascade layer has no unlayered root
// default that outranks it. Any declaration outside a layer beats every one
// inside, so `--window-bar: 0px` unlayered would collapse the desktop window
// bar that `:root:has(.glim-window-bar)` sets in a layer, without any error.
// The root is `:root`, `html` or a bare attribute selector like `[dir="rtl"]`.
// web/dist is not read: Tailwind's `@layer properties` fallbacks are meant to
// lose to the app's unlayered values.
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
