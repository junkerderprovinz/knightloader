// A file the phone plays on the local network comes as a stream of the kit
// (kit/group/stream.go): a sealed head with the stream's key, pieces sealed
// under it, and an end the instance signs. The phone takes the bytes only when
// every piece opens, the length and the SHA-256 match and the end is signed,
// and plays through the relay otherwise. The direct call it sends is tagged
// with the peer-auth key the way the kit tags it.
//
// The seal of the head and the signatures have checks of their own
// (check-member-key.mjs and the vectors in internal/federation/wire_test.go),
// so here they stand in, and the pieces are sealed for real. The app opens the
// pieces and sums the stream with the phone's own crypto; here noble and
// sha256.ts stand in for it, counted, so the check sees every piece go there.
//
// Run by hand and by CI, from mobile/: `node check-direct-stream.mjs`
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { gcm } from '@noble/ciphers/aes.js';
import { compile } from './stand-in-react.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const api = (name) => pathToFileURL(join(here, 'src', 'api', name)).href;
const base64 = await import(api('base64.ts'));
const sha = await import(api('sha256.ts'));

let signed = () => true;
const { readAnswer, hmacSha256, directSignature } = compile(join(here, 'src', 'play', 'directStream.ts'), {
  '@noble/ciphers/aes.js': { gcm },
  '../api/base64': base64,
  '../api/sha256': sha,
  '../api/relayFrame': { openResult: (_key, _id, sealed) => JSON.parse(sealed) },
  '../api/memberKey': { verifyResult: (...args) => signed(...args) },
});

const problems = [];
const expect = (what, got, want) => {
  const g = JSON.stringify(got);
  const w = JSON.stringify(want);
  if (g !== w) problems.push(`${what}: got ${g}, want ${w}`);
};
const throws = async (what, f) => {
  try {
    await f();
    problems.push(`${what}: it was taken`);
  } catch {
    // Refused, as it should be.
  }
};
const used = { open: 0, sum: 0 };
const crypto = {
  open: async (key, nonce, aad, sealed) => {
    used.open++;
    return gcm(key, nonce, aad).decrypt(sealed);
  },
  sha256: async (bytes) => {
    used.sum++;
    return sha.sha256(bytes);
  },
};

const enc = (s) => new Uint8Array(base64.utf8ToBytes(s));
const b64 = (bytes) => base64.base64Encode(Array.from(bytes));

// RFC 4231, test case 2.
expect('HMAC-SHA256', sha.toHex(hmacSha256(enc('Jefe'), enc('what do ya want for nothing?'))), '5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843');
expect(
  'the tag of a direct call',
  directSignature(new Uint8Array(Array.from({ length: 32 }, (_, i) => i)), 'phone-1', '1790260200', enc('{"requestId":"r1"}')),
  '754d8285bd6dd037a956aa402729e1e323db6263e23bb0bb9ff616725ef9c363',
);

const sent = { frameKey: new Uint8Array(32), requestId: 'r1', target: 'nas', sealedCall: [] };
const answerOf = (status, body) => JSON.stringify({ sealed: JSON.stringify({ status, body: b64(body), key: b64(new Uint8Array(32)), sig: b64(new Uint8Array(64)) }) });
const piece = (kind, b) => {
  const head = new Uint8Array(5);
  head[0] = kind.charCodeAt(0);
  new DataView(head.buffer).setUint32(1, b.length);
  return [head, b];
};
const http = (type, ...parts) => {
  const head = enc(`HTTP/1.1 200 OK\r\nContent-Type: ${type}\r\n\r\n`);
  const all = [head, ...parts.flat()];
  const out = new Uint8Array(all.reduce((n, p) => n + p.length, 0));
  let at = 0;
  for (const p of all) {
    out.set(p, at);
    at += p.length;
  }
  return out;
};

const film = enc('frames of a film, '.repeat(9000));
const streamOf = (data, { length = data.length, sum = sha.sha256(data), tamper = false, cut = false } = {}) => {
  const key = new Uint8Array(32).fill(7);
  const seal = (kind, n, plain) => {
    const nonce = new Uint8Array(12);
    new DataView(nonce.buffer).setUint32(8, n);
    return gcm(key, nonce, new Uint8Array([kind.charCodeAt(0)])).encrypt(plain);
  };
  const parts = [piece('h', enc(answerOf(200, enc(JSON.stringify({ length, key: b64(key) })))))];
  let n = 0;
  for (let at = 0; at < data.length; at += 65536) {
    const sealed = seal('d', n++, data.subarray(at, at + 65536));
    if (tamper && n === 2) sealed[0] ^= 1;
    parts.push(piece('d', sealed));
  }
  if (!cut) parts.push(piece('e', seal('e', n, enc(JSON.stringify({ length, sha256: sha.toHex(sum), sig: b64(new Uint8Array(64)) })))));
  return http('application/x-kit-stream', ...parts);
};

used.open = used.sum = 0;
const whole = await readAnswer(streamOf(film), sent, crypto);
expect('a whole stream', whole.bytes.length, film.length);
expect('its bytes', sha.toHex(sha.sha256(whole.bytes)), sha.toHex(sha.sha256(film)));
expect('every piece and the end opened by the given crypto', used.open, Math.ceil(film.length / 65536) + 1);
expect('the stream summed by the given crypto', used.sum, 1);
await throws('a piece that was changed', () => readAnswer(streamOf(film, { tamper: true }), sent, crypto));
await throws('a stream without its end', () => readAnswer(streamOf(film, { cut: true }), sent, crypto));
await throws('a stream whose sum is not the bytes', () => readAnswer(streamOf(film, { sum: new Uint8Array(32) }), sent, crypto));
await throws('a stream shorter than it announced', () => readAnswer(streamOf(film, { length: film.length + 1 }), sent, crypto));
signed = (_id, _target, _call, _status, body) => !base64.bytesToUtf8(body).startsWith('{"length"');
await throws('an end the instance did not sign', () => readAnswer(streamOf(film), sent, crypto));
signed = () => false;
await throws('a head the instance did not sign', () => readAnswer(streamOf(film), sent, crypto));
signed = () => true;

expect('a refusal', (await readAnswer(http('application/json', [enc(answerOf(404, enc('{"error":"no streams","code":"noStreams"}')))]), sent, crypto)).status, 404);
await throws('a direct call turned away', () => readAnswer(enc('HTTP/1.1 403 Forbidden\r\nContent-Type: text/plain\r\n\r\nforbidden\n'), sent, crypto));
await throws('an answer without a head', () => readAnswer(enc('HTTP/1.1 200 OK\r\n'), sent, crypto));

if (problems.length) {
  console.error(problems.join('\n'));
  process.exit(1);
}
console.log('ok: a stream on the local network is taken only whole and signed, and the call is tagged as the kit tags it');
