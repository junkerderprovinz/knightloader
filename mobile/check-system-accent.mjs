// The phone wears the system's colour only where it chose it and the phone has
// one, and otherwise falls back to the instance's accent, never to a stale
// colour. Android hands its palette over as signed ARGB ints, which have to
// come out as the colour they name.
//
// src/theme/accentSource.ts imports nothing at run time, so node strips its
// types and these checks call the real functions.
//
// Run by hand and by CI, from mobile/: `node check-system-accent.mjs`
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const { SYSTEM_ACCENT, argbToHex, resolveAccent } = await import(
  pathToFileURL(join(here, 'src', 'theme', 'accentSource.ts')).href
);

const problems = [];
const expect = (what, got, want) => {
  if (got !== want) problems.push(`${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
};

expect('the word is stored, never the colour', SYSTEM_ACCENT, 'system');
expect('the system colour wins while it is chosen', resolveAccent('system', '#1D99F3', '#E0383E'), '#E0383E');
expect('without a source the instance accent shows', resolveAccent('system', '#1D99F3', undefined), '#1D99F3');
expect('without a source or an instance the default shows', resolveAccent('system', undefined, undefined), undefined);
expect('a colour chosen here wins over the instance', resolveAccent('#FF8389', '#1D99F3', '#E0383E'), '#FF8389');
expect('nothing chosen follows the instance', resolveAccent(undefined, '#1D99F3', '#E0383E'), '#1D99F3');
expect('an instance accent that is no colour leaves the default', resolveAccent(undefined, 'blue', undefined), undefined);

// Material You's blue as Kotlin's signed Int: 0xFF0061A4.
expect('an opaque colour arrives negative', argbToHex(0xff0061a4 | 0), '#0061A4');
expect('the alpha byte goes', argbToHex(0x00ffffff), '#FFFFFF');
expect('black keeps its six digits', argbToHex(0xff000000 | 0), '#000000');

if (problems.length) {
  console.error(`${problems.length} problem(s):`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}
console.log('ok: the system colour shows where it is chosen and there is one, the instance accent otherwise');
