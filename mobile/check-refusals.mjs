// A refused call shows the app's own sentence where it knows the reason. A
// direct connection whose token the instance dropped, after its password was
// changed or reset, says to connect again with the twelve words; a peer's 401
// and a relay connection's keep the server's words.
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

expect('a direct connection refused with 401', errorText(t, refusedCall(true, '/api', 401, 'unauthorized')), en['error.connectionEnded']);
expect('a peer refusing through the connected instance', errorText(t, refusedCall(true, '/api/instances/office', 401, 'unauthorized')), 'unauthorized');
expect('a relay connection refused with 401', errorText(t, refusedCall(false, '/api', 401, 'unauthorized')), 'unauthorized');
expect('a refusal with a code of its own', errorText(t, refusedCall(true, '/api', 409, '{"error":"peers are off","code":"federationOff"}')), en['error.federationOff']);
expect('a refusal with no code', errorText(t, refusedCall(true, '/api', 500, 'disk full')), 'disk full');

if (problems.length) {
  console.error(problems.join('\n'));
  process.exit(1);
}
console.log('ok: a dropped token on a direct connection reads as a connection to make again, every other refusal keeps its words');
