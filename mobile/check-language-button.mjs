// The language button in the settings names the language in use with its own
// flag, both taken from one catalogue entry, so the button and the ticked row
// on the language page never disagree.
//
// src/i18n/catalogue.ts reaches the catalogues only as data, so node strips
// their types and the check calls the real function.
//
// Run by hand and by CI, from mobile/: `node check-language-button.mjs`
import { registerHooks } from 'node:module';

// The app imports its own modules without an extension, which Metro resolves
// and Node does not.
const src = new URL('./src/', import.meta.url).href;
registerHooks({
  resolve(specifier, context, next) {
    const ours = context.parentURL?.startsWith(src);
    if (ours && /^\.\.?\//.test(specifier) && !/\.\w+$/.test(specifier)) return next(`${specifier}.ts`, context);
    return next(specifier, context);
  },
});

const { languageOf, LANGUAGES, flagEmoji } = await import(new URL('./src/i18n/catalogue.ts', import.meta.url).href);

const problems = [];
const expect = (what, got, want) => {
  const g = JSON.stringify(got);
  const w = JSON.stringify(want);
  if (g !== w) problems.push(`${what}: got ${g}, want ${w}`);
};

expect('German by its own name and flag', languageOf('de'), { label: 'Deutsch', flag: flagEmoji('de') });
expect('English under the British flag', languageOf('en'), { label: 'English', flag: flagEmoji('gb') });
expect('Catalan under its country flag', languageOf('ca').flag, flagEmoji('es'));
expect('a code the app does not list shows as itself, without a flag', languageOf('xx'), { label: 'xx', flag: '' });

for (const l of LANGUAGES) {
  const got = languageOf(l.code);
  if (got.label !== l.label || got.flag !== flagEmoji(l.flag)) {
    problems.push(`${l.code}: the button shows ${got.label} ${got.flag}, the language page ${l.label} ${flagEmoji(l.flag)}`);
  }
}

if (problems.length) {
  console.error(`${problems.length} problem(s):`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}
console.log(`ok: the language button names all ${LANGUAGES.length} languages as the language page does`);
