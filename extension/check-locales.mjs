// Key parity for the extension's locale catalogues. The web UI and the app get
// it from the type checker; the extension has no build step, so a missing key
// would quietly fall back to English.
//
// Run by CI and by hand: `node extension/check-locales.mjs`.

import { existsSync, readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const src = readFileSync(join(here, 'src', 'i18n.js'), 'utf8');

// i18n.js is a plain script, not a module, so only the MESSAGES literal is
// evaluated, which also skips its chrome.* calls.
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
  for (const k of english) {
    const src = MESSAGES.en[k];
    const dst = MESSAGES[loc][k];
    if (typeof dst !== 'string') continue;
    for (const ph of src.match(/\{[a-zA-Z]+\}/g) ?? []) {
      if (!dst.includes(ph)) problems.push(`${loc}: ${k} lost the ${ph} placeholder`);
    }
  }
  // English left in another catalogue marks an unfinished translation. Short
  // strings such as "QR" or "Token" are often the same in every language.
  if (loc !== 'en') {
    for (const k of english) {
      const en = MESSAGES.en[k];
      if (typeof en === 'string' && en.length >= 25 && MESSAGES[loc][k] === en) {
        problems.push(`${loc}: ${k} is still the English text`);
      }
    }
  }
}

// A key that nothing reads leaves its label in English on the page. A substring
// search is enough because every key is spelled out at its t() call; a key
// assembled at runtime would show up here as unread.
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
