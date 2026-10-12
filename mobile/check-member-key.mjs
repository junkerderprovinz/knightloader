// The app makes its id from its key, signs a call and proves its id the way the
// server checks them, and takes an instance's answer only as the server signs
// it.
//
// src/api/memberKey.ts ports relay.MemberID, CallMessage, HelloMessage and
// ResultMessage. When they disagree, every call the phone makes is refused or
// every answer dropped. So the port runs the vectors of
// internal/relay/member_test.go with their seed, and what it makes has to come
// out as the Go tests pin it.
//
// Run by hand and by CI, from mobile/: `node check-member-key.mjs`
import { createPrivateKey, sign as cryptoSign } from 'node:crypto';
import { readFileSync, writeFileSync, mkdtempSync, rmSync } from 'node:fs';
import { createRequire, stripTypeScriptTypes } from 'node:module';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const require = createRequire(import.meta.url);
const goTest = readFileSync(join(here, '..', 'internal', 'relay', 'member_test.go'), 'utf8');

// The source runs as it ships, beside its own base64 and sha256 modules, with
// the curve library taken from node_modules.
const source = stripTypeScriptTypes(readFileSync(join(here, 'src', 'api', 'memberKey.ts'), 'utf8'))
  .replace(`'@noble/curves/ed25519.js'`, `'${pathToFileURL(require.resolve('@noble/curves/ed25519.js')).href}'`)
  .replace(`'./base64'`, `'./base64.mjs'`)
  .replace(`'./sha256'`, `'./sha256.mjs'`);
const dir = mkdtempSync(join(tmpdir(), 'member-key-'));
const problems = [];
let port;
try {
  writeFileSync(join(dir, 'base64.mjs'), stripTypeScriptTypes(readFileSync(join(here, 'src', 'api', 'base64.ts'), 'utf8')));
  writeFileSync(join(dir, 'sha256.mjs'), stripTypeScriptTypes(readFileSync(join(here, 'src', 'api', 'sha256.ts'), 'utf8')));
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

for (const [kind, name] of [['instance', 'vectorInstanceID'], ['mobile', 'vectorPhoneID'], ['extension', 'vectorBrowserID']]) {
  const id = port.memberId(kind, port.memberPublicKey(seed));
  if (!want(name) || id !== want(name)) problems.push(`the ${kind} id is ${id}, the server makes ${want(name)}`);
}

// The answer the vector key signs to the call that arrived as the vector's
// sealed bytes, which the phone takes only as it stands and only for that
// call as it was sent, and the proof of its id it gives
// the relay in its hello.
const fromB64 = (s) => Array.from(Buffer.from(s ?? '', 'base64'));
const instanceId = want('vectorInstanceID');
const resultSig = new Uint8Array(fromB64(want('vectorResultSig')));
const removed = Array.from(Buffer.from('removed'));
const pub = port.memberPublicKey(seed);
const asked = fromB64(want('vectorSealedCall'));
if (!port.verifyResult(fields.id, instanceId, asked, 410, removed, pub, resultSig)) {
  problems.push('the server\'s signed answer does not verify; resultMessage differs from relay.ResultMessage');
}
if (port.verifyResult(fields.id, instanceId, asked, 200, removed, pub, resultSig)) problems.push('an answer whose status was changed still verifies');
const altered = asked.map((b, i) => (i === asked.length - 1 ? b ^ 1 : b));
if (port.verifyResult(fields.id, instanceId, altered, 410, removed, pub, resultSig)) {
  problems.push('an answer the instance gave to a call altered on the way verifies');
}
const forgerSeed = new Uint8Array(32).fill(9);
const forgerKey = createPrivateKey({ key: Buffer.concat([Buffer.from('302e020100300506032b657004220420', 'hex'), forgerSeed]), format: 'der', type: 'pkcs8' });
const forged = new Uint8Array(cryptoSign(null, port.resultMessage(fields.id, instanceId, asked, 410, removed), forgerKey));
if (port.verifyResult(fields.id, instanceId, asked, 410, removed, port.memberPublicKey(forgerSeed), forged)) {
  problems.push('an answer signed by another key than the one the target\x27s id is made from verifies');
}
// The fingerprint the settings screen shows has to read as the instance shows
// the phone's key while it waits for approval.
if (port.fingerprint(pub) !== want('vectorFingerprint')) {
  problems.push(`the fingerprint is ${port.fingerprint(pub)}, the server shows ${want('vectorFingerprint')}`);
}
const challenge = new Uint8Array(fromB64(want('vectorChallenge')));
const relayKey = goTest.match(/vectorRelayKey\s*=\s*"([^"]+)"/)?.[1];
const hello = b64(port.proveId(seed, relayKey, want('vectorRelayHost'), want('vectorPhoneID'), challenge));
if (hello !== want('vectorHelloSig')) problems.push(`the hello proof is ${hello}, the server checks for ${want('vectorHelloSig')}`);

if (problems.length) {
  console.error('The app signs calls the server refuses:\n' + problems.map((p) => '  ' + p).join('\n'));
  process.exit(1);
}
console.log('Member key: the app makes the server’s ids and signs its vector call byte for byte.');
