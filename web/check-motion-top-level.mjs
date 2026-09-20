// The top motion level is `wild`, no rule names it, and a value saved under the
// retired name still resolves.
//
// The top level of GlimStone 2.0.0's motion axis is called `wild`, where the
// language once said `full`. That string is not wording: it goes into the
// `data-motion` attribute on <html>, stylesheet selectors match on it and
// localStorage holds it, so an app keeping the old spelling speaks a different
// wire format from the language it adopts and from its own phone app. None of
// the failures below is visible in a build, a type check or a screenshot:
//
//   names     The four levels are `off`, `subtle`, `wild`, `storm`. An app that
//             spells the third one `full` gets the tokens it wrote itself and
//             none of the ones it copies, and a component rule taken from
//             reference/tokens.css matches nothing.
//
//   attribute Nothing says `data-motion="full"` any more, in a selector or in a
//             comment. A comment naming an attribute value the app does not
//             write is a claim about the DOM the next reader has no reason to
//             doubt.
//
//   selector  A rule names the quiet levels rather than the lively one.
//             `[data-motion="wild"] .thing` excludes `storm`, which asks for
//             more motion and would lose the livelier treatment for exactly the
//             elements somebody wrote a special rule for.
//             `:root:not([data-motion="subtle"]):not([data-motion="off"])` says
//             the same thing and keeps saying it when a fifth level is added
//             above. The storm's own token block is the one exception and is
//             recognised by shape: a `:root[data-motion="storm"]` whose body is
//             nothing but custom properties holds the level's numbers rather
//             than a component rule.
//
//   label     Every catalogue carries `settings.motion.wild` and none carries
//             `settings.motion.full`. The picker builds its label from the key
//             (`settings.motion.${m}` in Look.tsx), so a catalogue that kept
//             the old one renders the raw key string in that language and the
//             type check sees nothing, the lookup being a template.
//
//   stored    A `full` sits in the localStorage of every instance somebody has
//             used, and it has to keep resolving to the top level, so the module
//             is imported and asked. Leaving it to the fallback works only while
//             DEFAULT_MOTION happens to be that same level: change the default,
//             which is a settings decision rather than a compatibility one, and
//             every instance that ever touched the setting loses it.
//
// Not seen: whether the level looks livelier than the one below it, which is
// check-motion-tokens.mjs's shape check and, past that, a screen.
//
// Run from web/: `node check-motion-top-level.mjs`
import { readFileSync, readdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const read = (rel) => readFileSync(join(here, rel), 'utf8');

const problems = [];
const fail = (why) => problems.push(why);

/** The four, in order. The third one is the whole subject of this file. */
const LEVELS = ['off', 'subtle', 'wild', 'storm'];
const TOP = 'wild';
const RETIRED = 'full';

// The axis' own module names the four levels.

const appRel = 'src/lib/appearance.ts';
const appRaw = read(appRel);
const app = appRaw.replace(/\/\*[\s\S]*?\*\//g, '').replace(/^\s*\/\/.*$/gm, '');

const union = app.match(/export type Motion\s*=\s*([^;]+);/);
if (!union) fail(`${appRel}: no "export type Motion" union - the four level names have no single source`);
else {
  const spelled = [...union[1].matchAll(/'([^']+)'/g)].map((m) => m[1]);
  if (spelled.join('|') !== LEVELS.join('|')) {
    fail(
      `${appRel}: type Motion is ${spelled.map((s) => `'${s}'`).join(' | ')}, and the four names are fixed: ` +
        `${LEVELS.map((s) => `'${s}'`).join(' | ')}`,
    );
  }
}

const listOf = (name) => {
  const m = app.match(new RegExp(`${name}\\s*:[^=]*=\\s*\\[([^\\]]*)\\]`));
  return m ? [...m[1].matchAll(/'([^']+)'/g)].map((x) => x[1]) : null;
};

const picker = listOf('MOTION_LEVELS');
if (picker && !picker.includes(TOP)) {
  fail(`${appRel}: MOTION_LEVELS is [${picker.join(', ')}] - the top level a picker offers is '${TOP}'`);
}

const def = app.match(/DEFAULT_MOTION\s*:\s*Motion\s*=\s*'([^']+)'/);
if (!def) fail(`${appRel}: no DEFAULT_MOTION`);
else if (def[1] !== TOP) {
  fail(`${appRel}: DEFAULT_MOTION is '${def[1]}' - the default is the top visible level, '${TOP}'`);
}

// The retired spelling is gone from the source tree, in code and in comments.
// The one place it may still appear is the migration that accepts it, which is
// recognised by the constant naming it.

const walk = (dir) => {
  const out = [];
  for (const name of readdirSync(dir, { withFileTypes: true })) {
    const p = join(dir, name.name);
    if (name.isDirectory()) out.push(...walk(p));
    else if (/\.(ts|tsx|css)$/.test(name.name)) out.push(p);
  }
  return out;
};

const RETIRED_ATTR = new RegExp(`data-motion\\s*=\\s*["']?${RETIRED}["']?`, 'g');
for (const file of walk(join(here, 'src'))) {
  const rel = file.slice(here.length + 1).replace(/\\/g, '/');
  const text = readFileSync(file, 'utf8');
  for (const m of text.matchAll(RETIRED_ATTR)) {
    const line = text.slice(0, m.index).split('\n').length;
    fail(
      `${rel}:${line} still says data-motion="${RETIRED}" - the attribute this app writes is ` +
        `data-motion="${TOP}" (GlimStone 2.0.0), so this names a state the DOM never reaches`,
    );
  }
}

// The stylesheet gets a stricter reading than the rest of the tree: a quoted
// `full` in a CSS file can only be a level name, so the sentence listing the
// levels above the tokens is caught too. In the .ts tree the same pattern would
// fire on the migration that has to name the old spelling.
const cssRel = 'src/index.css';
const cssRaw = read(cssRel);
for (const m of cssRaw.matchAll(new RegExp(`["']${RETIRED}["']`, 'g'))) {
  const line = cssRaw.slice(0, m.index).split('\n').length;
  fail(
    `${cssRel}:${line} names the level "${RETIRED}" - this axis' levels are ` +
      `${LEVELS.map((s) => `"${s}"`).join(', ')} (GlimStone 2.0.0)`,
  );
}

// A rule names the quiet levels, never the lively one. Comments blanked,
// offsets kept byte for byte, so line numbers stay right and the paragraphs
// describing the axis are not read as selectors.
const css = cssRaw.replace(/\/\*[\s\S]*?\*\//g, (c) => c.replace(/[^\n]/g, ' '));
const lineAt = (i) => css.slice(0, i).split('\n').length;

/** The [start, end) of the block whose `{` is at `at`, braces counted. */
function block(at) {
  let depth = 0;
  for (let i = at; i < css.length; i++) {
    if (css[i] === '{') depth++;
    else if (css[i] === '}' && --depth === 0) return [at + 1, i];
  }
  throw new Error(`unclosed block at ${cssRel}:${lineAt(at)}`);
}

/** Everything from the previous brace up to this selector's own `{`. */
function selectorAt(i) {
  const open = css.indexOf('{', i);
  const start = Math.max(css.lastIndexOf('}', i), css.lastIndexOf('{', i)) + 1;
  return { text: css.slice(start, open).trim(), open };
}

for (const lively of [TOP, 'storm']) {
  const re = new RegExp(`\\[data-motion\\s*=\\s*["']${lively}["']\\]`, 'g');
  for (const m of css.matchAll(re)) {
    const { text, open } = selectorAt(m.index);
    // The storm's own token block: `:root[data-motion='storm']` and nothing
    // else in the selector, holding custom properties and nothing else. Those
    // are the level's numbers, the one thing that has to name it.
    const bare = /^:root\[data-motion\s*=\s*["']storm["']\]$/.test(text);
    const body = css.slice(...block(open));
    const onlyTokens = body.replace(/--[\w-]+\s*:[^;]*;/g, '').trim() === '';
    if (lively === 'storm' && bare && onlyTokens) continue;
    fail(
      `${cssRel}:${lineAt(m.index)} the selector "${text}" names the lively level '${lively}'. ` +
        `A rule that names the top level forgets the next one - write ` +
        `:root:not([data-motion="subtle"]):not([data-motion="off"]) instead (GlimStone 2.0.0)`,
    );
  }
}

// The catalogues carry the new key and not the old one.

const locDir = join(here, 'src/lib/locales');
for (const file of readdirSync(locDir)) {
  if (!file.endsWith('.ts') || file === 'index.ts') continue;
  const text = readFileSync(join(locDir, file), 'utf8');
  if (!text.includes(`'settings.motion.${TOP}'`)) {
    fail(`src/lib/locales/${file}: no 'settings.motion.${TOP}' - the picker renders the raw key in this language`);
  }
  if (text.includes(`'settings.motion.${RETIRED}'`)) {
    fail(
      `src/lib/locales/${file}: still carries 'settings.motion.${RETIRED}', a key nothing reads any more ` +
        `(Look.tsx builds the label from settings.motion.\${m}, and m is '${TOP}')`,
    );
  }
}

// A saved `full` still resolves to the top level, measured rather than read:
// the module is imported with the two globals it touches stubbed, and asked
// what it makes of the value real instances are holding.

globalThis.document = {
  documentElement: { dataset: {}, style: { setProperty() {}, removeProperty() {} } },
};
const cell = { value: null };
globalThis.localStorage = {
  getItem: (k) => (k === 'kl-motion' ? cell.value : null),
  setItem: (k, v) => {
    if (k === 'kl-motion') cell.value = String(v);
  },
  removeItem: () => {},
};

const mod = await import('./src/lib/appearance.ts');

cell.value = RETIRED;
const resolved = mod.readCachedMotionIntensity();
if (resolved !== TOP) {
  fail(
    `${appRel}: a stored '${RETIRED}' reads back as '${resolved}', not '${TOP}'. Every instance that ever ` +
      `touched this setting is holding that string, and dropping it is the setting forgetting itself`,
  );
}
if (cell.value === RETIRED) {
  fail(
    `${appRel}: reading a stored '${RETIRED}' left it in storage. The alias is a bridge, not a second ` +
      `spelling to carry for ever - rewrite it on the first read that finds it`,
  );
}

// ...and the migration must not become a way to smuggle the old name back in.
cell.value = 'nonsense';
if (mod.readCachedMotionIntensity() !== mod.DEFAULT_MOTION) {
  fail(`${appRel}: an unrecognised stored value no longer falls back to DEFAULT_MOTION`);
}
if (mod.MOTION_STORED.includes(RETIRED)) {
  fail(
    `${appRel}: MOTION_STORED contains '${RETIRED}'. That list is what a live value may legally be; ` +
      `the retired spelling is translated on the way in, never admitted to the set`,
  );
}

if (problems.length) {
  console.error(`check-motion-top-level: ${problems.length} problem(s) with the top motion level.`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}

console.log(
  "ok: the top motion level is 'wild' everywhere, no rule names a lively level, " +
    "every catalogue has the key, and a stored 'full' still resolves",
);
