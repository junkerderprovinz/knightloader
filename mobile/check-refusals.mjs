// A refused call shows the app's own sentence where it knows the reason, and
// the server's words everywhere else.
//
// src/api/refusal.ts imports nothing at run time, so node strips its types and
// these checks call the real functions with the English catalogue.
//
// Run by hand and by CI, from mobile/: `node check-refusals.mjs`
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const { refusedCall, errorText } = await import(pathToFileURL(join(here, 'src', 'api', 'refusal.ts')).href);
const { en } = await import(pathToFileURL(join(here, 'src', 'i18n', 'en.ts')).href);

const problems = [];
const expect = (what, got, want) => {
  if (got !== want) problems.push(`${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
};
const t = (key) => en[key];

expect('a refusal with its own words', errorText(t, refusedCall(401, 'unauthorized')), 'unauthorized');
expect('a refusal with a code of its own', errorText(t, refusedCall(409, '{"error":"peers are off","code":"federationOff"}')), en['error.federationOff']);
expect('an instance reset since this phone paired', errorText(t, refusedCall(410, 'reset')), en['error.groupReset']);
expect('a phone taken out of the group', errorText(t, refusedCall(410, 'removed')), 'removed');
expect('a refusal with no code', errorText(t, refusedCall(500, 'disk full')), 'disk full');

if (problems.length) {
  console.error(problems.join('\n'));
  process.exit(1);
}
console.log("ok: a refusal the app knows reads in its own words, every other one keeps the server's");
