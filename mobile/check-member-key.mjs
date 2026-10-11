// The app signs a call the way the server checks it.
//
// src/api/memberKey.ts ports relay.CallMessage. When the two disagree, every
// signed call the phone makes is refused for a signature that does not hold.
// So the port signs the call of TestTheMemberCallVectorStaysTheSame in
// internal/relay/member_test.go with its seed, and the key and the signature
// have to come out as the Go test pins them.
//
// Run by hand and by CI, from mobile/: `node check-member-key.mjs`
import { readFileSync, writeFileSync, mkdtempSync, rmSync } from 'node:fs';
import { createRequire, stripTypeScriptTypes } from 'node:module';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const require = createRequire(import.meta.url);
const goTest = readFileSync(join(here, '..', 'internal', 'relay', 'member_test.go'), 'utf8');

// The source runs as it ships, beside its own base64 module, with the curve
// library taken from node_modules.
const source = stripTypeScriptTypes(readFileSync(join(here, 'src', 'api', 'memberKey.ts'), 'utf8'))
  .replace(`'@noble/curves/ed25519.js'`, `'${pathToFileURL(require.resolve('@noble/curves/ed25519.js')).href}'`)
  .replace(`'./base64'`, `'./base64.mjs'`);
const dir = mkdtempSync(join(tmpdir(), 'member-key-'));
const problems = [];
let port;
try {
  writeFileSync(join(dir, 'base64.mjs'), stripTypeScriptTypes(readFileSync(join(here, 'src', 'api', 'base64.ts'), 'utf8')));
  writeFileSync(join(dir, 'memberKey.mjs'), source);
  port = await import(pathToFileURL(join(dir, 'memberKey.mjs')).href);
} finally {
  rmSync(dir, { recursive: true, force: true });
}

const want = (name) => goTest.match(new RegExp(`${name}\\s*=\\s*"([^"]+)"`))?.[1];
const key = want('vectorKey');
const sig = want('vectorSig');
const call = goTest.match(/vectorCall\s*=\s*ProxyCall\{([\s\S]*?)\n\t\}/)?.[1];
if (!key || !sig || !call) {
  console.error('The vector was not found in member_test.go; the patterns in this check no longer match it');
  process.exit(1);
}
const field = (name) => call.match(new RegExp(`${name}:\\s*(?:\\[\\]byte\\()?\`?"?([^"\`,)]*)`))?.[1];
const fields = {
  id: field('ID'),
  method: field('Method'),
  path: field('Path'),
  body: call.match(/Body:\s*\[\]byte\(`([^`]*)`\)/)?.[1],
  authorization: field('Authorization'),
  from: field('From'),
  kind: 'mobile',
  sent: Number(field('Sent')),
};
const seed = new Uint8Array(32).fill(0x2a);
const b64 = (bytes) => Buffer.from(bytes).toString('base64');
const target = goTest.match(/vectorTarget\s*=\s*"([^"]+)"/)?.[1];

if (b64(port.memberPublicKey(seed)) !== key) problems.push(`the public key is ${b64(port.memberPublicKey(seed))}, the server has ${key}`);
const got = b64(port.signCall(seed, target, fields));
if (got !== sig) problems.push(`the signature is ${got}, the server checks for ${sig}; the signed message differs`);

if (problems.length) {
  console.error('The app signs calls the server refuses:\n' + problems.map((p) => '  ' + p).join('\n'));
  process.exit(1);
}
console.log('Member key: the app signs the server’s vector call byte for byte.');
