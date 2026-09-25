// A tile that carries a brand's mark lights up in that brand's own colour under
// the pointer (GlimStone's "Brand tiles"), and its name and mark take the ink
// that holds on that colour.
//
// Checks:
//   rule     index.css fills a hovered .glim-brand-tile with var(--tile), gives
//            it var(--tile-ink), hands both to its mark as --mark-ink and
//            --mark-cut, turns the brand tokens its marks paint from to the
//            ink, and swaps a .glim-mark-rest for its .glim-mark-hover, all
//            behind @media (hover: hover).
//   grey     nothing is left of the grey hover: no --carbon-tile-hover or
//            --brand-*-hover in index.css, no tileHover class in a source.
//   colours  every .glim-tile-* and .kl-tile-* class and every coin in
//            lib/donate.ts names a colour and the ink the rule gives it: white
//            where white reaches 2:1 on it, #161616 below that.
//   tiles    AppTile names only tile classes index.css defines, every AppTile
//            in BrowserTools.tsx names its brand, and neither an app tile nor
//            an unpicked coin tile moves its colours through a transition.
//   marks    on the lit tile every mark paints in the ink alone: each part of
//            an app tile's mark in currentColor, a --brand-* token the rule
//            turns to the ink, var(--mark-ink, …) or var(--mark-cut, …), or
//            the mark brings a `lit` version that does; a coin's disc paints
//            through var(--mark-ink, …).
//   rest     the marks the stylesheet colours per theme keep 3:1 on the dark
//            tile and 1.35:1 on the light one, where the brand's own colour
//            stands.
//
// Not checked: the resting colours of the vendor marks that bring their own,
// and a mark's shape once flattened, which only a look at the lit tile shows.
//
// Run: `node web/check-tile-hover.mjs`.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const src = join(here, 'src');
const read = (...parts) => readFileSync(join(src, ...parts), 'utf8');

const INK_FLOOR = 2.0;
const DARK = '#161616';
const WHITE = '#ffffff';
const DARK_REST = 3.0;
const LIGHT_REST = 1.35;

const problems = [];
const fail = (message) => {
  console.error(`check-tile-hover: ${message}`);
  process.exit(1);
};

function rgb(hex) {
  let h = hex.trim().replace('#', '');
  if (h.length === 3) h = [...h].map((c) => c + c).join('');
  if (!/^[0-9a-f]{6}$/i.test(h)) return null;
  return [0, 2, 4].map((i) => parseInt(h.slice(i, i + 2), 16));
}

function luminance([r, g, b]) {
  const f = (v) => {
    const c = v / 255;
    return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b);
}

function contrast(a, b) {
  const [x, y] = [luminance(a), luminance(b)].sort((p, q) => q - p);
  return (x + 0.05) / (y + 0.05);
}

/** The ink the rule gives a tile colour, and a complaint if `ink` is another. */
function checkInk(label, color, ink) {
  const fill = rgb(color ?? '');
  if (!fill || !rgb(ink ?? '')) {
    problems.push(`${label}: the tile colour or its ink is not a hex colour`);
    return;
  }
  const white = contrast(rgb(WHITE), fill);
  const want = white >= INK_FLOOR ? WHITE : DARK;
  if (ink.toLowerCase() !== want) {
    problems.push(`${label}: ink ${ink} on ${color}, where the rule gives ${want} (white measures ${white.toFixed(2)}:1)`);
  }
  if (contrast(rgb(ink), fill) < INK_FLOOR) {
    problems.push(`${label}: ${ink} on ${color} is under ${INK_FLOOR}:1`);
  }
}

const css = read('index.css').replace(/\/\*[\s\S]*?\*\//g, (c) => c.replace(/[^\n]/g, ' '));

// The rule.
const hoverBlocks = [...css.matchAll(/@media\s*\(hover:\s*hover\)\s*\{/g)]
  .map((m) => {
    let depth = 1;
    const from = m.index + m[0].length;
    for (let i = from; i < css.length; i++) {
      if (css[i] === '{') depth++;
      else if (css[i] === '}' && --depth === 0) return css.slice(from, i);
    }
    return '';
  })
  .join('\n');
const lit = /\.glim-brand-tile:hover\s*,\s*\.group:hover\s*>\s*\.glim-brand-tile\s*\{([^}]*)\}/.exec(hoverBlocks)?.[1];
if (!lit) fail('index.css has no `.glim-brand-tile:hover, .group:hover > .glim-brand-tile` rule behind @media (hover: hover).');
const sets = (prop, value) => new RegExp(`(^|[\\s;])${prop}\\s*:\\s*${value.replace(/[()]/g, '\\$&')}\\s*;`).test(lit);
for (const [prop, value] of [
  ['background-color', 'var(--tile)'],
  ['color', 'var(--tile-ink)'],
  ['--mark-ink', 'var(--tile-ink)'],
  ['--mark-cut', 'var(--tile)'],
]) {
  if (!sets(prop, value)) problems.push(`index.css: a lit brand tile does not set ${prop}: ${value}`);
}
if (/\btransition\b/.test(lit)) problems.push('index.css: a lit brand tile carries a transition');
// The shape-morph rule hands every element a colour transition, so a brand
// tile needs a rule of its own that outranks it and leaves the colours out.
const armed = /\.glim-shape-armed\s+\.glim-brand-tile\s*,\s*\.glim-shape-armed\s+\.glim-brand-tile\s+\*\s*\{([^}]*)\}/.exec(css)?.[1];
if (!armed) problems.push('index.css: no .glim-shape-armed .glim-brand-tile rule keeps the shape-morph fade off a brand tile');
else if (/\b(background-color|color|fill|stroke|border-color|all)\s+var/.test(armed)) {
  problems.push('index.css: the .glim-shape-armed .glim-brand-tile rule still fades a colour');
}
// The brand tokens a lit tile turns to its ink, which the marks may paint from.
const inked = new Set([...lit.matchAll(/(--brand-[a-z-]+)\s*:\s*var\(--tile-ink\)/g)].map((m) => m[1]));
const swap = (cls, display) =>
  new RegExp(
    `:is\\(\\.glim-brand-tile:hover,\\s*\\.group:hover\\s*>\\s*\\.glim-brand-tile\\)\\s*\\.${cls}\\s*\\{\\s*display\\s*:\\s*${display}\\s*;?\\s*\\}`,
  ).test(hoverBlocks);
if (!swap('glim-mark-rest', 'none') || !swap('glim-mark-hover', 'block')) {
  problems.push('index.css: a lit brand tile does not swap .glim-mark-rest for .glim-mark-hover');
}
if (!/(^|\n)\.glim-mark-hover\s*\{\s*display\s*:\s*none\s*;?\s*\}/.test(css)) {
  problems.push('index.css: .glim-mark-hover is not hidden at rest');
}

// The grey.
for (const m of css.matchAll(/--carbon-tile-hover[\w-]*|--brand-[a-z-]+-hover\b/g)) {
  problems.push(`index.css:${css.slice(0, m.index).split('\n').length}: ${m[0]} belongs to the grey hover`);
}

function sources(dir) {
  const found = [];
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) found.push(...sources(path));
    else if (/\.tsx?$/.test(entry)) found.push(path);
  }
  return found;
}
const show = (path) => path.slice(src.length + 1).split('\\').join('/');
for (const path of sources(src)) {
  const text = readFileSync(path, 'utf8');
  const m = /\bcarbon-tileHover\w*/.exec(text);
  if (m) problems.push(`${show(path)}:${text.slice(0, m.index).split('\n').length}: ${m[0]} belongs to the grey hover`);
}

// The colours.
const tileClasses = new Map();
for (const m of css.matchAll(/\.((?:glim|kl)-tile-[a-z]+)\s*\{([^}]*)\}/g)) {
  const color = /--tile\s*:\s*(#[0-9a-fA-F]{3,6})\s*;/.exec(m[2])?.[1];
  const ink = /--tile-ink\s*:\s*(#[0-9a-fA-F]{3,6})\s*;?/.exec(m[2])?.[1];
  tileClasses.set(m[1], true);
  checkInk(`index.css: .${m[1]}`, color, ink);
}
if (tileClasses.size < 9) fail(`only ${tileClasses.size} tile classes read from index.css - the reader went blind.`);

const { CRYPTO_COINS: coins } = await import(pathToFileURL(join(src, 'lib', 'donate.ts')).href);
if (!Array.isArray(coins) || coins.length < 4) fail('no coin list read from lib/donate.ts.');
for (const coin of coins) {
  if (!coin.tile) problems.push(`lib/donate.ts: ${coin.id} has no tile colour`);
  else checkInk(`lib/donate.ts: ${coin.id}`, coin.tile.color, coin.tile.ink);
}

// The tiles.
const appTile = read('components', 'AppTile.tsx');
const brands = new Map(
  [...(/const TILES = \{([\s\S]*?)\}/.exec(appTile)?.[1] ?? '').matchAll(/(\w+): '([\w-]+)'/g)].map((m) => [m[1], m[2]]),
);
if (brands.size < 9) fail(`only ${brands.size} brands read from AppTile.tsx's TILES - the reader went blind.`);
for (const [brand, cls] of brands) {
  if (!tileClasses.has(cls)) problems.push(`AppTile.tsx: ${brand} names .${cls}, which index.css does not define`);
}
if (/\btransition/.test(appTile)) problems.push('AppTile.tsx: an app tile moves its colours through a transition');

const browserTools = read('pages', 'settings', 'BrowserTools.tsx');
const tags = [...browserTools.matchAll(/<AppTile\b[\s\S]*?\/>/g)].map((m) => m[0]);
if (tags.length < 10) fail(`only ${tags.length} AppTile tags read from BrowserTools.tsx - the reader went blind.`);
for (const tag of tags) {
  const name = /\bname=(?:"([^"]+)"|\{([^}]+)\})/.exec(tag)?.slice(1).find(Boolean) ?? tag.slice(0, 40);
  const brand = /\bbrand="(\w+)"/.exec(tag)?.[1];
  if (!brand) problems.push(`BrowserTools.tsx: the ${name} tile names no brand`);
  else if (!brands.has(brand)) problems.push(`BrowserTools.tsx: the ${name} tile names ${brand}, which AppTile does not know`);
}

// The picked coin keeps the accent and may fade into it; everything else on
// the coin tile, the ticker included, changes in the frame the tile does.
const dialogFile = read('components', 'CryptoDonateDialog.tsx');
// The ticker's classes and the tile, apart from the network chips above them.
const dialog = dialogFile.slice(dialogFile.indexOf('const HIDDEN_TICKER'));
if (!/function CoinTile\b/.test(dialog)) fail('CoinTile was not found after HIDDEN_TICKER in CryptoDonateDialog.tsx.');
const picked = /'glim-active bg-accent[^']*'/.exec(dialog)?.[0];
if (!picked) fail('the picked coin tile\'s classes were not found in CryptoDonateDialog.tsx.');
if (!/'glim-brand-tile\b[^']*'/.test(dialog)) problems.push('CryptoDonateDialog.tsx: an unpicked coin tile is no .glim-brand-tile');
const moving = /\btransition(?:-all|-colors)?(?![\w-])/.exec(dialog.replace(picked, ''));
if (moving) problems.push(`CryptoDonateDialog.tsx: ${moving[0]} moves a coin tile's colours`);
if (!/'--tile': coin\.tile\.color/.test(dialog) || !/'--tile-ink': coin\.tile\.ink/.test(dialog)) {
  problems.push('CryptoDonateDialog.tsx: a coin tile does not take its coin\'s --tile and --tile-ink');
}

// The marks.
const appMarks = read('lib', 'appMarks.ts');
const markup = new Map(
  [browserTools, appMarks].flatMap((text) => [...text.matchAll(/const (\w+) =\s*'(<svg[^']*)'/g)]).map((m) => [m[1], m[2]]),
);
if (markup.size < 20) fail(`only ${markup.size} marks read from BrowserTools.tsx and lib/appMarks.ts - the reader went blind.`);

const SHAPES = new Set(['path', 'circle', 'rect', 'ellipse', 'polygon', 'polyline', 'line', 'use']);
const INK = /^(currentColor|none|var\(--mark-(?:ink|cut)\b.*)$/i;

/**
 * What in an SVG would keep a colour of its own on the lit tile: a shape whose
 * fill, its own or inherited, is none of currentColor, var(--mark-ink, …),
 * var(--mark-cut, …) or a gradient whose every stop is a token the lit tile
 * turns to the ink.
 */
function ownColours(svg) {
  const stopsOf = new Map();
  for (const g of svg.matchAll(/<(linearGradient|radialGradient)\b[^>]*\bid="([^"]+)"[^>]*>([\s\S]*?)<\/\1>/g)) {
    stopsOf.set(g[2], [...g[3].matchAll(/stop-color\s*[:=]\s*"?\s*([^;"/>]+)/g)].map((s) => s[1].trim()));
  }
  const paints = (value) => {
    if (INK.test(value)) return true;
    const ref = /^url\(#([^)]+)\)$/.exec(value)?.[1];
    const stops = ref && stopsOf.get(ref);
    return Boolean(stops?.length) && stops.every((s) => inked.has(/^var\((--brand-[a-z-]+)\)$/.exec(s)?.[1]));
  };
  const found = [];
  const stack = [];
  let inGradient = 0;
  for (const tag of svg.matchAll(/<(\/?)([A-Za-z][\w:-]*)\b([^>]*?)(\/?)>/g)) {
    const [, closing, name, attrs, selfClosing] = tag;
    if (/Gradient$/.test(name)) {
      if (closing) inGradient--;
      else if (!selfClosing) inGradient++;
      continue;
    }
    if (closing) {
      stack.pop();
      continue;
    }
    if (inGradient) continue;
    const own = /\bfill="([^"]+)"/.exec(attrs)?.[1] ?? /style="[^"]*\bfill\s*:\s*([^;"]+)/.exec(attrs)?.[1];
    const fill = own?.trim() ?? stack.at(-1) ?? 'black';
    if (!selfClosing) stack.push(fill);
    if (SHAPES.has(name) && !paints(fill)) found.push(fill);
  }
  return [...new Set(found)];
}

/** The markup a BrandMark or Logo draws, by the const it names. */
const svgOf = (expr) => markup.get(/^\{?(\w+)\}?$/.exec(expr.trim())?.[1] ?? '');

// A class a BrandMark wears that colours it has to do so from a token the lit
// tile turns to the ink.
const markClasses = new Map();
for (const m of css.matchAll(/(^|\n)\.(glim-[a-z]+-mark)\s*\{\s*color\s*:\s*var\((--brand-[a-z-]+)\)\s*;?\s*\}/g)) {
  markClasses.set(m[2], m[3]);
}

let marks = 0;
for (const call of browserTools.matchAll(/<BrandMark\b([^>]*?)\/>/g)) {
  marks++;
  const attrs = call[1];
  const svg = svgOf(/\bsvg=\{(\w+)\}/.exec(attrs)?.[1] ?? '');
  const name = /\bsvg=\{(\w+)\}/.exec(attrs)?.[1] ?? '?';
  if (!svg) {
    problems.push(`BrowserTools.tsx: the markup of ${name} was not found`);
    continue;
  }
  for (const cls of /\bclassName="([^"]*)"/.exec(attrs)?.[1].split(/\s+/).filter(Boolean) ?? []) {
    const token = markClasses.get(cls);
    if (token && !inked.has(token)) problems.push(`index.css: .${cls} paints from ${token}, which a lit tile leaves in its colour`);
  }
  const litName = /\blit=\{(\w+)\}/.exec(attrs)?.[1];
  if (litName) {
    const hover = svgOf(litName);
    if (!hover) problems.push(`BrowserTools.tsx: the markup of ${litName} was not found`);
    else for (const c of ownColours(hover)) problems.push(`${litName}: paints ${c} on the lit tile, where only its ink belongs`);
    continue;
  }
  for (const c of ownColours(svg)) {
    problems.push(`${name}: paints ${c} on the lit tile and brings no single-colour version for it`);
  }
}
if (marks < 14) fail(`only ${marks} BrandMark calls read from BrowserTools.tsx - the reader went blind.`);

const donateMarks = read('components', 'donateMarks.tsx');
const coinMark = /export function CoinMark\b[\s\S]*?\n\}/.exec(donateMarks)?.[0] ?? '';
if (!/<Mark\b[^>]*\bfill="var\(--mark-ink,[^"]*\)"/.test(coinMark)) {
  problems.push('donateMarks.tsx: a coin\'s disc does not paint through var(--mark-ink, …)');
}

// The rest.
const BLOCKS = [
  { name: 'dark', open: /:root\s*,\s*\[data-theme=(["'])dark\1\]\s*\{/g, floor: DARK_REST },
  { name: 'system light', open: /:root:not\(\[data-theme=(["'])dark\1\]\)\s*\{/g, floor: LIGHT_REST },
  { name: 'light', open: /(?::root)?\[data-theme=(["'])light\1\]\s*\{/g, floor: LIGHT_REST },
];
function blockBody(open) {
  const found = [...css.matchAll(open)];
  if (found.length !== 1) return null;
  let depth = 1;
  const from = found[0].index + found[0][0].length;
  for (let i = from; i < css.length; i++) {
    if (css[i] === '{') depth++;
    else if (css[i] === '}' && --depth === 0) return css.slice(from, i);
  }
  return null;
}
// A gradient is visible by its best stop.
const REST = [
  ['android', ['--brand-android']],
  ['docker', ['--brand-docker']],
  ['windows', ['--brand-windows']],
  ['unraid', ['--brand-unraid-from', '--brand-unraid-to']],
];
for (const block of BLOCKS) {
  const body = blockBody(block.open);
  if (body === null) fail(`the "${block.name}" theme block was not found in src/index.css.`);
  const value = (token) => rgb(new RegExp(`${token}\\s*:\\s*([^;]+);`).exec(body)?.[1] ?? '');
  const ground = value('--carbon-surface2');
  for (const [mark, tokens] of REST) {
    const colours = tokens.map(value);
    if (!ground || colours.some((c) => !c)) {
      problems.push(`index.css: a token of the ${mark} mark or --carbon-surface2 is missing in the ${block.name} block`);
      continue;
    }
    const best = Math.max(...colours.map((c) => contrast(c, ground)));
    if (best < block.floor) {
      problems.push(`the ${mark} mark at rest: ${best.toFixed(2)}:1 on the ${block.name} tile, under ${block.floor}:1`);
    }
  }
}

if (problems.length) {
  console.error(`check-tile-hover: ${problems.length} problem(s).`);
  for (const p of problems.sort()) console.error(`  ${p}`);
  process.exit(1);
}
console.log(
  `check-tile-hover: ${tileClasses.size} tile classes and ${coins.length} coins light up in their brand's colour with its ink, and ${marks} marks turn that ink.`,
);
