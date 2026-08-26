// Key parity for the extension's 42 locale catalogues.
//
// The web UI and the app both get this for free: their locales are declared
// `: Dict`, so a missing key is a compile error. This extension deliberately
// has no build step at all - src/ ships as plain files so "load unpacked"
// works straight from a checkout (see ../embed.go) - which means nothing was
// checking it. A locale short one key silently falls back to English for that
// string, in one language, for whoever happens to speak it.
//
// Run by CI and by hand: `node extension/check-locales.mjs`.

import { existsSync, readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const src = readFileSync(join(here, 'src', 'i18n.js'), 'utf8');

// i18n.js is a plain script for <script> tags and importScripts, not a module,
// so it cannot be imported. Evaluating just the MESSAGES literal is enough and
// avoids pulling in the chrome.* calls further down the file.
const start = src.indexOf('const MESSAGES');
if (start === -1) throw new Error('MESSAGES not found in src/i18n.js');
const end = src.indexOf('\n};', start);
if (end === -1) throw new Error('could not find the end of MESSAGES');
const MESSAGES = new Function(`${src.slice(start, end + 3)}\nreturn MESSAGES;`)();

const locales = Object.keys(MESSAGES);
const english = Object.keys(MESSAGES.en);
const problems = [];

if (!MESSAGES.en) throw new Error('no en locale');

for (const loc of locales) {
  const keys = Object.keys(MESSAGES[loc]);
  for (const k of english) {
    if (!(k in MESSAGES[loc])) problems.push(`${loc}: missing ${k}`);
  }
  for (const k of keys) {
    if (!english.includes(k)) problems.push(`${loc}: stray ${k} (not in en)`);
  }
  // A placeholder dropped in translation is a message that renders "{count}"
  // to a person, or silently loses the number - neither is caught by a key
  // check on its own.
  for (const k of english) {
    const src = MESSAGES.en[k];
    const dst = MESSAGES[loc][k];
    if (typeof dst !== 'string') continue;
    for (const ph of src.match(/\{[a-zA-Z]+\}/g) ?? []) {
      if (!dst.includes(ph)) problems.push(`${loc}: ${k} lost the ${ph} placeholder`);
    }
  }
  // English text left in a non-English catalogue is the usual shape of a
  // half-done translation pass. Only exact equality is flagged: plenty of
  // short strings legitimately match across languages ("QR", "Token"), so
  // anything under 25 characters is left alone rather than made noisy.
  if (loc !== 'en') {
    for (const k of english) {
      const en = MESSAGES.en[k];
      if (typeof en === 'string' && en.length >= 25 && MESSAGES[loc][k] === en) {
        problems.push(`${loc}: ${k} is still the English text`);
      }
    }
  }
}

// A key that exists in all 42 catalogues and is never read is exactly as broken
// as a missing one, and this file could not see it: parity asks whether a key
// EXISTS everywhere, not whether anything renders it. Found the hard way - the
// appearance axes and the Problems heading were translated into every language
// while options.html kept its English markup and nothing overwrote it, so the
// page rendered half in the reader's language and half in English. It looked
// fine in every check that existed.
//
// A crude substring search over the sources is enough here, because that is how
// these keys are actually used: t('options.themeLabel'), spelled out. There is
// no key assembled at runtime in this extension, and if one is ever added, this
// will say so loudly rather than quietly stop covering it.
const SRC = join(here, 'src');
const sources = ['options.js', 'popup.js', 'picker.js', 'background.js', 'shared.js', 'appearance.js']
  .map((f) => join(SRC, f))
  .filter((f) => existsSync(f))
  .map((f) => readFileSync(f, 'utf8'))
  .join(String.fromCharCode(10));
for (const k of english) {
  if (!sources.includes(k)) {
    problems.push(`${k} is in every catalogue and read by nothing - dead translation, or a label that never got wired up`);
  }
}

if (problems.length > 0) {
  console.error(`${problems.length} problem(s) across ${locales.length} locales:`);
  for (const p of problems.slice(0, 40)) console.error(`  ${p}`);
  if (problems.length > 40) console.error(`  ... and ${problems.length - 40} more`);
  process.exit(1);
}
console.log(`ok: ${locales.length} locales, ${english.length} keys each, placeholders intact`);
