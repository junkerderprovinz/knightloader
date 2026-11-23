// The phone tags a direct call on the local network with the same peer-auth
// key the instances derive from the phrase, or they turn it away unread. The
// values come from TestTheKeysAPhraseMakesStayTheSame in
// internal/federation/wire_test.go.
//
// Run by hand and by CI, from mobile/: `node check-peer-auth.mjs`
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { compile } from './stand-in-react.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const api = (name) => join(here, 'src', 'api', name);
const sha = await import(pathToFileURL(api('sha256.ts')).href);
const words = await import(pathToFileURL(api('wordlist.ts')).href);
const { derivePeerAuth } = compile(api('seedphrase.ts'), { './sha256': sha, './wordlist': words });
const goTest = readFileSync(join(here, '..', 'internal', 'federation', 'wire_test.go'), 'utf8');

const problems = [];
const rows = {
  'peer-auth key of nothing': new Uint8Array(),
  'peer-auth key of a word': sha.utf8('knightloader'),
};
for (const [name, secret] of Object.entries(rows)) {
  const want = goTest.match(new RegExp(`\\{"${name}", [^,]+, "([0-9a-f]{64})"\\}`))?.[1];
  if (!want) {
    problems.push(`"${name}" is not in TestTheKeysAPhraseMakesStayTheSame`);
    continue;
  }
  const got = sha.toHex(derivePeerAuth(secret));
  if (got !== want) problems.push(`${name}: got ${got}, want ${want}`);
}

if (problems.length) {
  console.error(problems.join('\n'));
  process.exit(1);
}
console.log('ok: the phone derives the peer-auth key the instances derive');
