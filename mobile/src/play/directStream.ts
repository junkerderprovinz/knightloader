// The phone's half of a stream on the local network, kit/group/stream.go's
// streamFrom and streamReader: a direct call tagged with the peer-auth key,
// and an answer that is a sealed head with the stream's own key, the pieces
// sealed under that key, and an end the instance signs over the length and the
// SHA-256 of the whole. A stream that breaks off, runs long or does not match
// what the instance signed throws, and the caller plays through the relay.
import { base64Decode, bytesToUtf8, utf8ToBytes } from '../api/base64';
import { verifyResult } from '../api/memberKey';
import { openResult } from '../api/relayFrame';
import { sha256, toHex } from '../api/sha256';

/** STREAM_PREFIX is where an instance answers a call as a stream. */
export const STREAM_PREFIX = '/kit/group/v1/stream';

const STREAM_TYPE = 'application/x-kit-stream';

/**
 * The crypto a stream is opened with: AES-GCM and SHA-256 over a few MiB per
 * piece, which the phone's native code does many times faster than
 * JavaScript, fast enough for a film to keep up.
 */
export interface StreamCrypto {
  open(key: Uint8Array, nonce: Uint8Array, aad: Uint8Array, sealed: Uint8Array): Promise<Uint8Array>;
  sha256(data: Uint8Array): Promise<Uint8Array>;
}

/** What the phone sent, which an answer has to be signed against. */
export interface Sent {
  frameKey: Uint8Array;
  requestId: string;
  target: string;
  sealedCall: number[];
}

/** hmacSha256 is HMAC (RFC 2104) over sha256.ts. */
export function hmacSha256(key: Uint8Array, message: Uint8Array): Uint8Array {
  const block = new Uint8Array(64);
  block.set(key.length > 64 ? sha256(key) : key);
  const inner = new Uint8Array(64 + message.length);
  const outer = new Uint8Array(64 + 32);
  for (let i = 0; i < 64; i++) {
    inner[i] = block[i] ^ 0x36;
    outer[i] = block[i] ^ 0x5c;
  }
  inner.set(message, 64);
  outer.set(sha256(inner), 64);
  return sha256(outer);
}

/** directSignature tags a direct call's sender, time and body, mirroring the kit's. */
export function directSignature(peerAuth: Uint8Array, sender: string, unix: string, body: Uint8Array): string {
  const message = `direct\x00${sender}\x00${unix}\x00${toHex(sha256(body))}`;
  return toHex(hmacSha256(peerAuth, new Uint8Array(utf8ToBytes(message))));
}

/** endBody is what the instance signs at the end of a stream. */
function endBody(length: number, sum: Uint8Array): number[] {
  return utf8ToBytes(`{"length":${length},"sha256":"${toHex(sum)}"}`);
}

/** opened is a sealed answer opened and held to the target's signature. */
function opened(raw: Uint8Array, sent: Sent): { status: number; body: number[]; key: Uint8Array } {
  const response = JSON.parse(bytesToUtf8(Array.from(raw))) as { sealed?: unknown };
  const result = typeof response.sealed === 'string' ? openResult(sent.frameKey, sent.requestId, response.sealed) : null;
  if (!result) throw new Error('the instance answered unreadably');
  const body = typeof result.body === 'string' ? base64Decode(result.body) : [];
  const key = new Uint8Array(typeof result.key === 'string' ? base64Decode(result.key) : []);
  const sig = new Uint8Array(typeof result.sig === 'string' ? base64Decode(result.sig) : []);
  if (!verifyResult(sent.requestId, sent.target, sent.sealedCall, result.status, body, key, sig)) {
    throw new Error('the answer is not signed by the instance');
  }
  return { status: result.status, body, key };
}

/**
 * readAnswer reads the whole HTTP answer to a direct call: the stream a 200
 * carries, or the instance's refusal with its status.
 */
export async function readAnswer(raw: Uint8Array, sent: Sent, crypto: StreamCrypto): Promise<{ status: number; bytes: Uint8Array }> {
  let end = -1;
  for (let i = 0; i + 3 < raw.length; i++) {
    if (raw[i] === 13 && raw[i + 1] === 10 && raw[i + 2] === 13 && raw[i + 3] === 10) {
      end = i;
      break;
    }
  }
  if (end < 0) throw new Error('the answer has no head');
  const [line, ...headers] = bytesToUtf8(Array.from(raw.subarray(0, end))).split('\r\n');
  if (!/^HTTP\/1\.\d 200 /.test(line)) throw new Error(`the direct call was refused: ${line}`);
  const type = headers.find((h) => h.toLowerCase().startsWith('content-type:'))?.slice(13).trim() ?? '';
  const body = raw.subarray(end + 4);
  if (type !== STREAM_TYPE) {
    const answer = opened(body, sent);
    return { status: answer.status, bytes: new Uint8Array(answer.body) };
  }
  return { status: 200, bytes: await readStream(body, sent, crypto) };
}

async function readStream(body: Uint8Array, sent: Sent, crypto: StreamCrypto): Promise<Uint8Array> {
  const pieces: [number, Uint8Array][] = [];
  for (let at = 0; at < body.length; ) {
    if (at + 5 > body.length) throw new Error('the stream broke off');
    const n = ((body[at + 1] << 24) | (body[at + 2] << 16) | (body[at + 3] << 8) | body[at + 4]) >>> 0;
    if (at + 5 + n > body.length) throw new Error('the stream broke off');
    pieces.push([body[at], body.subarray(at + 5, at + 5 + n)]);
    at += 5 + n;
  }
  const [head, ...rest] = pieces;
  if (!head || head[0] !== 0x68) throw new Error('the stream began unreadably');
  const last = rest[rest.length - 1];
  if (!last || last[0] !== 0x65) throw new Error('the stream broke off');
  const opener = opened(head[1], sent);
  const { length, key } = JSON.parse(bytesToUtf8(opener.body)) as { length: number; key: string };
  const streamKey = new Uint8Array(base64Decode(key));
  // Every piece is opened at once: they do not depend on each other, and the
  // native code opens them side by side.
  const plain = await Promise.all(
    rest.map(([kind, sealed], count) => {
      if (kind !== 0x64 && count !== rest.length - 1) throw new Error('the stream holds a piece of no known kind');
      // A big-endian count in the last eight bytes. A range of a few MiB has
      // far fewer than 2^32 pieces, so the upper half stays zero.
      const nonce = new Uint8Array(12);
      new DataView(nonce.buffer).setUint32(8, count);
      return crypto.open(streamKey, nonce, new Uint8Array([kind]), sealed);
    }),
  );
  const out = new Uint8Array(length);
  let got = 0;
  for (const p of plain.slice(0, -1)) {
    if (got + p.length > length) throw new Error('the stream runs past its length');
    out.set(p, got);
    got += p.length;
  }
  const endPiece = JSON.parse(bytesToUtf8(Array.from(plain[plain.length - 1]))) as { length: number; sha256: string; sig: string };
  const sum = await crypto.sha256(out);
  if (got !== length || endPiece.length !== length || endPiece.sha256 !== toHex(sum)) {
    throw new Error('the stream does not match what the instance sent');
  }
  const sig = new Uint8Array(base64Decode(endPiece.sig));
  if (!verifyResult(sent.requestId, sent.target, sent.sealedCall, 200, endBody(length, sum), opener.key, sig)) {
    throw new Error('the end of the stream is not signed by the instance');
  }
  return out;
}
