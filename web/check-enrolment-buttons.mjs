// The two controls that start an enrolment say the same word and wear different
// glyphs.
//
// GlimStone 2.1.0, and the report behind it was about these two cards: the
// second factor said *Enable* and the passkey card said *Add passkey*. Neither
// is wrong. Together they are, because a reader meeting them in one tab has to
// work out whether the different wording means a different thing. It does not -
// both open a guided sequence that ends with a capability armed - so both say
// the app's own word for starting a setup.
//
// WHAT BREAKS WITHOUT IT:
//
//   same word   Two verbs for one act. It costs a reader a decision on the one
//               surface where being unsure is most expensive, and it is
//               invisible to everything else in this repo: each label is a
//               perfectly good string in 42 languages, and no type, no build and
//               no screenshot compares them to each other.
//
//   a lookup    ...and the word is a LOOKUP of what the app already says, never
//               a fresh translation. That is why this check compares the
//               catalogues VALUE for VALUE rather than checking that somebody
//               translated the new key: a language that got a second, freshly
//               invented word for one act is exactly the failure, and it passes
//               any check that only counts keys.
//
//   a glyph     A control named this way MUST carry one, because once two
//               buttons in one tab read the same the glyph is the only thing
//               left telling them apart. It is picked from the meaning of the
//               CAPABILITY - a shield for the second factor, a plus for a key
//               joining a list - never from the shared verb, which is why the
//               two marks also have to differ from each other.
//
//   the table   A third enrolment card added without a line here is a third
//               button free to grow a third verb. So the table is written out by
//               hand and every card in the folder has to be in it - the same
//               shape check-donate-addresses.mjs uses, and for the same reason:
//               a list derived from the thing it guards agrees with any mistake
//               in it.
//
// WHAT IT DOES NOT SEE. Whether the glyph is the RIGHT one for the capability -
// that is a judgement, and it is made by looking at the card. It sees only that
// there is one and that the two are not the same drawing.
//
// Run by CI and by hand, from web/: `node check-enrolment-buttons.mjs`
import { readFileSync, readdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const read = (rel) => readFileSync(join(here, rel), 'utf8');

const problems = [];
const fail = (why) => problems.push(why);

/**
 * The cards that enrol something, and the key each one's entry control reads.
 *
 * THE KEYS STAY PUT WHEN THE WORDING MOVES. A key is what a glyph and a search
 * index hang off; the visible text is not. So the second factor's control still
 * reads `auth.twoFactor.enable` although it no longer says "Enable" - renaming
 * the key to match a label is how a stable handle becomes another thing that
 * moves.
 */
const ENROLMENTS = [
  {
    file: 'src/pages/settings/access/TwoFactorCard.tsx',
    key: 'auth.twoFactor.enable',
    capability: 'the second factor',
  },
  {
    file: 'src/pages/settings/access/PasskeyCard.tsx',
    key: 'auth.passkey.add',
    capability: 'a passkey',
  },
];

// --- the table covers the folder -------------------------------------------

const dir = 'src/pages/settings/access';
for (const name of readdirSync(join(here, dir))) {
  if (!name.endsWith('.tsx')) continue;
  const rel = `${dir}/${name}`;
  if (!ENROLMENTS.some((e) => e.file === rel)) {
    fail(
      `${rel} is a card in the enrolment folder with no line in this script's table. ` +
        `Add it, or its entry button is free to grow a verb of its own`,
    );
  }
}

// --- each entry control: one glyph, and the opening tag that carries it -----

/** The opening tag starting at `<Button` at `at`, quotes and braces counted. */
function openingTag(src, at) {
  let depth = 0;
  let quote = '';
  for (let i = at; i < src.length; i++) {
    const c = src[i];
    if (quote) {
      if (c === quote && src[i - 1] !== '\\') quote = '';
      continue;
    }
    if (c === '"' || c === "'" || c === '`') quote = c;
    else if (c === '{') depth++;
    else if (c === '}') depth--;
    else if (c === '>' && depth === 0) return { text: src.slice(at, i + 1), end: i + 1 };
  }
  return null;
}

const glyphs = new Map();
for (const e of ENROLMENTS) {
  let src;
  try {
    src = read(e.file);
  } catch {
    fail(`${e.file}: listed in the table and not on disk`);
    continue;
  }
  const label = src.indexOf(`{t('${e.key}')}`);
  if (label < 0) {
    fail(`${e.file}: nothing renders {t('${e.key}')} - the entry control for ${e.capability} moved or was renamed`);
    continue;
  }
  const btn = src.lastIndexOf('<Button', label);
  const tag = btn < 0 ? null : openingTag(src, btn);
  if (!tag || tag.end > label) {
    fail(`${e.file}: {t('${e.key}')} is not the label of a <Button> - this check cannot see what carries it`);
    continue;
  }
  const line = src.slice(0, btn).split('\n').length;
  const icon = tag.text.match(/\bicon=\{\s*<(Icon\w+)/);
  if (!icon) {
    fail(
      `${e.file}:${line} the control that starts ${e.capability} carries no glyph. Once two buttons in ` +
        `one tab read the same word the glyph is what tells them apart, so this one is required ` +
        `(GlimStone 2.1.0), picked from the capability and never from the shared verb`,
    );
    continue;
  }
  glyphs.set(e.key, { glyph: icon[1], file: e.file, line });
}

const seen = new Map();
for (const [key, g] of glyphs) {
  const first = seen.get(g.glyph);
  if (first) {
    fail(
      `${g.file}:${g.line} wears <${g.glyph}>, the same drawing as ${first}. Two same-worded buttons are ` +
        `only safe because each wears its own mark`,
    );
  } else seen.set(g.glyph, `${key} in ${g.file}`);
}

// --- the word: one act, one word, in every language ------------------------

const locDir = 'src/lib/locales';
/** The literal value of one key in one catalogue, quotes handled. */
function valueOf(text, key) {
  const at = text.indexOf(`'${key}':`);
  if (at < 0) return null;
  const rest = text.slice(at + key.length + 3);
  const m = rest.match(/^\s*(['"])((?:\\.|(?!\1).)*)\1/);
  return m ? m[2].replace(/\\'/g, "'").replace(/\\"/g, '"') : null;
}

for (const file of readdirSync(join(here, locDir))) {
  if (!file.endsWith('.ts') || file === 'index.ts') continue;
  const text = readFileSync(join(here, locDir, file), 'utf8');
  const values = ENROLMENTS.map((e) => ({ key: e.key, value: valueOf(text, e.key) }));
  const missing = values.filter((v) => v.value === null);
  if (missing.length) {
    fail(`${locDir}/${file}: no value for ${missing.map((v) => v.key).join(', ')}`);
    continue;
  }
  const distinct = [...new Set(values.map((v) => v.value))];
  if (distinct.length > 1) {
    fail(
      `${locDir}/${file}: one act, ${distinct.length} words - ` +
        values.map((v) => `${v.key} is "${v.value}"`).join(', ') +
        `. Every control that starts an enrolment says the same thing, and that thing is a LOOKUP of ` +
        `what this language already calls starting a setup (GlimStone 2.1.0)`,
    );
  }
}

if (problems.length) {
  console.error(`check-enrolment-buttons: ${problems.length} problem(s) with the enrolment entry controls.`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}

console.log(
  `ok: ${ENROLMENTS.length} enrolment controls, one word per language across every catalogue, ` +
    'each wearing its own glyph',
);
