// A picker tile that carries brand marks hovers to --carbon-tile-hover, and
// every mark on it stays visible there.
//
// On the dark theme the hover is #a8a8a8, a light grey behind marks that were
// picked for a dark ground. A coin's mark wears its palette position's hue,
// and gold on that grey measures 1.6:1, so the coin tile's mark switches to
// the tile's ink under the pointer (index.css, .kl-coin-tile). The app tiles
// on the App page carry vendor artwork that has one set of colours for every
// ground, so each of those has to bring a part that stands out on the grey by
// itself.
//
// Checks:
//   tokens   --carbon-tile-hover and --carbon-tile-hover-ink stand in all three
//            theme blocks of src/index.css, and the ink reads as text (4.5:1).
//   tiles    the coin tile and the app tile hover to the token and take its
//            ink with the same variant, and no class list hovers to white.
//   switch   the dark theme's coin tile rule gives the mark the ink.
//   marks    every mark reaches 2:1 on the hover: the ink, each accent preset
//            and rainbow hue as the light theme darkens it, and some opaque
//            colour of each vendor mark in BrowserTools.tsx and
//            lib/appMarks.ts.
//   tokens   a single-colour mark the stylesheet colours per theme (Android's
//            and Docker's, and Unraid's gradient through its two stops) is
//            wired to its class, reaches 3:1 at rest on the tile and 2:1 on
//            the hover, in every theme block.
//   fills    a mark that keeps its own colour at rest and only takes a deeper
//            fill on the hover (Windows') is wired the same way and reaches
//            2:1 with it.
//
// A two-tone mark with one visible half is still visible, so a vendor mark
// passes on its best colour. Colours under an opacity below 1, a gradient's
// shading overlays among them, are left out, since they do not paint at the
// value written.
//
// Not checked: a custom accent from the free colour field, and the resting
// state of the marks that bring their own colours, which the tiles have always
// had.
//
// Run: `node web/check-tile-hover.mjs`.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const src = join(here, 'src');
const read = (...parts) => readFileSync(join(src, ...parts), 'utf8');

const MARK_FLOOR = 2.0;
const TEXT_FLOOR = 4.5;
// What a graphic needs at rest, the value the about card's marks are tuned to.
const REST_FLOOR = 3.0;

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

const hex = ([r, g, b]) => `#${[r, g, b].map((v) => v.toString(16).padStart(2, '0')).join('')}`;

// The three theme blocks, found as check-theme-tokens.mjs finds them.
const css = read('index.css').replace(/\/\*[\s\S]*?\*\//g, (c) => c.replace(/[^\n]/g, ' '));
const BLOCKS = [
  { name: 'dark', open: /:root\s*,\s*\[data-theme=(["'])dark\1\]\s*\{/g },
  { name: 'system light', open: /:root:not\(\[data-theme=(["'])dark\1\]\)\s*\{/g },
  { name: 'light', open: /(?::root)?\[data-theme=(["'])light\1\]\s*\{/g },
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

const theme = {};
for (const block of BLOCKS) {
  const body = blockBody(block.open);
  if (body === null) fail(`the "${block.name}" theme block was not found in src/index.css.`);
  const value = (token) => new RegExp(`${token}\\s*:\\s*([^;]+);`).exec(body)?.[1].trim();
  const tile = value('--carbon-tile-hover');
  const ink = value('--carbon-tile-hover-ink');
  const mix = value('--ink-mix');
  if (!tile || !rgb(tile)) problems.push(`index.css: --carbon-tile-hover is missing or not a hex colour in the ${block.name} block`);
  if (!ink || !rgb(ink)) problems.push(`index.css: --carbon-tile-hover-ink is missing or not a hex colour in the ${block.name} block`);
  theme[block.name] = { tile: tile && rgb(tile), ink: ink && rgb(ink), mix: mix ? parseFloat(mix) / 100 : null, value };
}
if (problems.length) report();

for (const [name, t] of Object.entries(theme)) {
  const c = contrast(t.ink, t.tile);
  if (c < TEXT_FLOOR) problems.push(`index.css: the tile ink on the tile hover is ${c.toFixed(2)}:1 in the ${name} block, under ${TEXT_FLOOR}:1 for the ticker and name`);
}

// The tiles.
const TILES = [
  { file: ['components', 'CryptoDonateDialog.tsx'], what: 'the coin tile' },
  { file: ['components', 'AppTile.tsx'], what: 'the app tile' },
];
for (const tile of TILES) {
  const text = read(...tile.file);
  const bg = /\b((?:[a-z-]+:)*)bg-carbon-tileHover\b/.exec(text);
  if (!bg) {
    problems.push(`${tile.file.join('/')}: ${tile.what} does not hover to bg-carbon-tileHover`);
    continue;
  }
  if (!bg[1].includes('hover:')) problems.push(`${tile.file.join('/')}: ${tile.what} wears bg-carbon-tileHover at rest`);
  if (!text.includes(`${bg[1]}text-carbon-tileHoverInk`)) {
    problems.push(`${tile.file.join('/')}: ${tile.what} hovers to the grey without ${bg[1]}text-carbon-tileHoverInk`);
  }
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
const WHITE_HOVER = /\bhover:bg-(?:white|\[#fff(?:fff)?\])(?![\w-])/;
for (const path of sources(src)) {
  const text = readFileSync(path, 'utf8');
  const m = WHITE_HOVER.exec(text);
  if (m) {
    const line = text.slice(0, m.index).split('\n').length;
    problems.push(`${path.slice(src.length + 1).split('\\').join('/')}:${line}: ${m[0]} - a tile hovers to bg-carbon-tileHover`);
  }
}

// The switch on the coin tile's mark.
const coinRule = /\[data-theme="dark"\]\s*\.kl-coin-tile:not\(\.glim-active\):hover\s+svg\s*\{([^}]*)\}/.exec(css);
if (!coinRule || !/color\s*:\s*var\(--carbon-tile-hover-ink\)/.test(coinRule[1])) {
  problems.push('index.css: the dark theme gives a hovered coin tile\'s mark no var(--carbon-tile-hover-ink)');
}

// The marks.
function checkMark(label, colours, ground, groundName) {
  const best = Math.max(...colours.map((c) => contrast(c, ground)));
  if (best < MARK_FLOOR) {
    problems.push(`${label}: at best ${best.toFixed(2)}:1 on the ${groundName} tile hover ${hex(ground)}, under ${MARK_FLOOR}:1`);
  }
}

checkMark('the coin mark in the tile ink', [theme.dark.ink], theme.dark.tile, 'dark');

// The light theme leaves a coin mark in --accent-ink, the hue mixed toward
// black by --ink-mix in sRGB, which scales each channel.
const appearance = read('lib', 'appearance.ts');
const listOf = (name) => {
  const body = new RegExp(`export const ${name}\\b[^=]*=\\s*\\[([\\s\\S]*?)\\];`).exec(appearance)?.[1] ?? '';
  return [...body.matchAll(/'(#[0-9A-Fa-f]{6})'/g)].map((m) => m[1]);
};
const hues = [...new Set([...listOf('ACCENTS'), ...listOf('RAINBOW')])];
if (hues.length < 8) fail(`only ${hues.length} accent and rainbow colours read from lib/appearance.ts - the reader went blind.`);
for (const name of ['system light', 'light']) {
  const t = theme[name];
  if (t.mix === null) fail(`no --ink-mix in the ${name} block.`);
  for (const h of hues) {
    const ink = rgb(h).map((v) => Math.round(v * t.mix));
    checkMark(`the coin mark in ${h} as --accent-ink ${hex(ink)}`, [ink], t.tile, name);
  }
}

/**
 * The colours an SVG paints at full strength. An element under an opacity
 * below 1, on itself or on a group around it, is skipped.
 */
function opaqueColours(svg) {
  const found = [];
  const classFill = new Map(
    [...svg.matchAll(/\.([\w-]+)\s*\{\s*fill\s*:\s*(#[0-9a-fA-F]{3,6})\s*;?\s*\}/g)].map((m) => [m[1], m[2]]),
  );
  const stack = [];
  for (const tag of svg.matchAll(/<(\/?)([A-Za-z][\w:-]*)\b([^>]*?)(\/?)>/g)) {
    const [, closing, , attrs, selfClosing] = tag;
    if (closing) {
      stack.pop();
      continue;
    }
    const faint = [...attrs.matchAll(/(?:^|[\s;"])(?:fill-|stop-)?opacity\s*[:=]\s*"?\s*([\d.]+)/g)].some((m) => parseFloat(m[1]) < 1);
    const hidden = (stack.length > 0 && stack[stack.length - 1]) || faint;
    if (!selfClosing) stack.push(hidden);
    if (hidden) continue;
    for (const m of attrs.matchAll(/(?:fill|stop-color)\s*[:=]\s*"?\s*(#[0-9a-fA-F]{3,6})\b/g)) found.push(m[1]);
    const cls = /\bclass="([^"]+)"/.exec(attrs)?.[1];
    for (const c of cls?.split(/\s+/) ?? []) if (classFill.has(c)) found.push(classFill.get(c));
  }
  return found.map(rgb).filter(Boolean);
}

// The vendor marks: the browsers' in BrowserTools.tsx, where they are drawn,
// and the app tiles' in lib/appMarks.ts, which BrowserTools.tsx draws too.
const browserTools = read('pages', 'settings', 'BrowserTools.tsx');
const appMarks = read('lib', 'appMarks.ts');
const vendor = [browserTools, appMarks].flatMap((text) => [...text.matchAll(/const (\w+)_SVG =\s*'([^']*)'/g)]);
if (vendor.length < 14) fail(`only ${vendor.length} vendor marks read from BrowserTools.tsx and lib/appMarks.ts - the reader went blind.`);
const marks = vendor.map((m) => ({ name: m[1], label: `the ${m[1].toLowerCase()} mark`, svg: m[2] }));

// Single-colour marks the stylesheet colours per theme instead of the tile
// ink: the const, the class its BrandMark wears, and the rest and hover tokens
// that class reads. A gradient has a pair per stop, which its markup reads as
// var() and the hover rule swaps.
const TOKEN_MARKS = [
  { name: 'ANDROID', cls: 'glim-android-mark', stops: [['--brand-android', '--brand-android-hover']] },
  { name: 'DOCKER', cls: 'glim-docker-mark', stops: [['--brand-docker', '--brand-docker-hover']] },
  { name: 'WINDOWS', cls: 'glim-windows-mark', stops: [['--brand-windows', '--brand-windows-hover']] },
  {
    name: 'UNRAID',
    cls: 'glim-unraid-mark',
    gradient: true,
    stops: [
      ['--brand-unraid-from', '--brand-unraid-from-hover'],
      ['--brand-unraid-to', '--brand-unraid-to-hover'],
    ],
  },
];

const ruleBody = (selector) => {
  const at = css.search(selector);
  if (at === -1) return null;
  const open = css.indexOf('{', at);
  return css.slice(open + 1, css.indexOf('}', open));
};

// Every rule inside an `@media (hover: hover)` block, so a block holding
// several marks' rules is read whole.
const hoverRules = [...css.matchAll(/@media\s*\(hover:\s*hover\)\s*\{/g)]
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
const hoverRule = (selector) => {
  const m = new RegExp(`${selector}\\s*\\{([^}]*)\\}`).exec(hoverRules);
  return m ? m[1] : null;
};

/** The mark's call site, where its BrandMark has to wear `cls`. */
function wired(name, cls) {
  if (!marks.some((m) => m.name === name)) {
    problems.push(`no ${name}_SVG in BrowserTools.tsx or lib/appMarks.ts, which ${cls} is measured for`);
    return false;
  }
  const call = new RegExp(`<BrandMark\\b[^>]*svg=\\{${name}_SVG\\}[^>]*>`).exec(browserTools)?.[0];
  if (!call || !new RegExp(`className="[^"]*\\b${cls}\\b`).test(call)) {
    problems.push(`BrowserTools.tsx: ${name}_SVG is drawn without className="${cls}"`);
  }
  return true;
}

for (const tm of TOKEN_MARKS) {
  const label = `the ${tm.name.toLowerCase()} mark`;
  if (!wired(tm.name, tm.cls)) continue;
  const svg = marks.find((m) => m.name === tm.name).svg;
  const hover = hoverRule(`\\.group:hover\\s+\\.${tm.cls}`);
  for (const [rest, lit] of tm.stops) {
    if (tm.gradient) {
      if (!svg.includes(`var(${rest})`)) problems.push(`${label}: its markup does not read var(${rest})`);
      if (!hover || !new RegExp(`${rest}\\s*:\\s*var\\(${lit}\\)`).test(hover)) {
        problems.push(`index.css: a hovered tile does not set ${rest} to var(${lit}) on .${tm.cls} behind @media (hover: hover)`);
      }
    } else {
      const restRule = ruleBody(new RegExp(`(^|\\n)\\.${tm.cls}\\s*\\{`));
      if (!restRule || !new RegExp(`\\bcolor\\s*:\\s*var\\(${rest}\\)`).test(restRule)) {
        problems.push(`index.css: .${tm.cls} does not take its colour from var(${rest})`);
      }
      if (!hover || !new RegExp(`\\bcolor\\s*:\\s*var\\(${lit}\\)`).test(hover)) {
        problems.push(`index.css: a hovered tile gives .${tm.cls} no var(${lit}) behind @media (hover: hover)`);
      }
    }
  }
  for (const [name, t] of Object.entries(theme)) {
    const ground = rgb(t.value('--carbon-surface2') ?? '');
    const restColours = tm.stops.map(([rest]) => rgb(t.value(rest) ?? ''));
    const litColours = tm.stops.map(([, lit]) => rgb(t.value(lit) ?? ''));
    if (!ground || [...restColours, ...litColours].some((c) => !c)) {
      problems.push(`index.css: a token of ${label} or --carbon-surface2 is missing or not a hex colour in the ${name} block`);
      continue;
    }
    const c = Math.max(...restColours.map((colour) => contrast(colour, ground)));
    if (c < REST_FLOOR) {
      problems.push(`${label} at rest: at best ${c.toFixed(2)}:1 on the ${name} tile ${hex(ground)}, under ${REST_FLOOR}:1`);
    }
    checkMark(`${label} as its hover tokens`, litColours, t.tile, name);
  }
}

for (const mark of marks) {
  if (TOKEN_MARKS.some((tm) => tm.name === mark.name)) continue;
  const colours = opaqueColours(mark.svg);
  // A mark painted in currentColor is the tile's ink, checked above.
  if (colours.length === 0) {
    if (!/currentColor/.test(mark.svg)) problems.push(`${mark.label}: no opaque colour found and no currentColor either`);
    continue;
  }
  for (const [name, t] of Object.entries(theme)) checkMark(mark.label, colours, t.tile, name);
}

report();

function report() {
  if (problems.length) {
    console.error(`check-tile-hover: ${problems.length} problem(s).`);
    for (const p of problems.sort()) console.error(`  ${p}`);
    process.exit(1);
  }
  console.log(
    `check-tile-hover: ${TILES.length} tiles hover to the tile grey, ${hues.length} hues and ${marks.length} marks clear ${MARK_FLOOR}:1 on it in every theme.`,
  );
  process.exit(0);
}
