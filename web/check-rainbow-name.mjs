// Rainbow mode has one name in English and one in German, in all three apps.
//
// The web UI's switch says "Rainbow mode" and „Regenbogen-Modus“. A bubble
// that spells the mode another way, or a switch in the phone app that says
// only „Regenbogen“, sends the reader looking for a setting whose name is not
// on the switch beside it.
//
// The name is the web's settings.rainbow. The phone's settings.rainbow and the
// extension's options.rainbow have to be it, and every English and German
// string in the three catalogues that names the mode has to spell it that way.
// A bare „Regenbogen“ meaning the mode is not caught, since the same word is
// also the colours themselves.
//
// Run: `node web/check-rainbow-name.mjs`.
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const read = (p) => readFileSync(join(root, p), 'utf8');

/** The quoted values of a catalogue written one `'key': 'value',` per line. */
function values(text) {
  const out = new Map();
  for (const m of text.matchAll(/^\s+'([\w.]+)':\s*(['`])((?:\\.|(?!\2).)*)\2,?$/gm)) out.set(m[1], m[3]);
  return out;
}

/** The extension keeps every language in one file, a block per language. */
function extensionBlock(lang) {
  const text = read('extension/src/i18n.js');
  const start = text.indexOf(`\n  ${lang}: {`);
  const end = text.indexOf('\n  },', start);
  return values(text.slice(start, end));
}

const catalogues = {
  en: { web: values(read('web/src/lib/locales/en.ts')), mobile: values(read('mobile/src/i18n/en.ts')), extension: extensionBlock('en') },
  de: { web: values(read('web/src/lib/locales/de.ts')), mobile: values(read('mobile/src/i18n/de.ts')), extension: extensionBlock('de') },
};
const SWITCH = { web: 'settings.rainbow', mobile: 'settings.rainbow', extension: 'options.rainbow' };
// Any spelling of the mode's name: rainbow mode, rainbow-mode, Regenbogenmodus.
const SPELLINGS = { en: /\brainbow[\s-]?mode\b/gi, de: /\bRegenbogen[\s-]?[Mm]odus\b/g };

const problems = [];
let checked = 0;
for (const [lang, apps] of Object.entries(catalogues)) {
  const name = apps.web.get(SWITCH.web);
  if (!name) {
    problems.push(`web ${lang}: no ${SWITCH.web}, the name the others follow`);
    continue;
  }
  for (const [app, strings] of Object.entries(apps)) {
    const label = strings.get(SWITCH[app]);
    if (label !== name) problems.push(`${app} ${lang}: the switch says ${JSON.stringify(label)}, want ${JSON.stringify(name)}`);
    for (const [key, value] of strings) {
      for (const m of value.matchAll(SPELLINGS[lang])) {
        checked++;
        if (m[0].toLowerCase() !== name.toLowerCase()) {
          problems.push(`${app} ${lang}: ${key} writes ${JSON.stringify(m[0])}, want ${JSON.stringify(name)}`);
        }
      }
    }
  }
}

if (checked < 6) problems.push(`found ${checked} mentions of the mode, expected at least 6`);

if (problems.length) {
  for (const p of problems) console.error(`✗ ${p}`);
  process.exit(1);
}
console.log(`ok: rainbow mode has one name in English and German across the web UI, the app and the extension (${checked} mentions)`);
