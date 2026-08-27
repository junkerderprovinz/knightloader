// Sealing and opening relay proxy frames, the phone's half of
// internal/relay/seal.go and the SealCall/OpenResult pair in protocol.go.
//
// The two implementations have to agree byte for byte or this app connects to
// a group and can speak to nobody in it, so everything that could drift is
// stated in one place here and checked against the Go side by
// relayFrame.test.ts: the domain string (seedphrase.ts), the additional-data
// layout below, the nonce length, and the nonce||ciphertext framing.
//
// WHY A LIBRARY, when sha256.ts next door is hand-written: that file's own
// comment gives the rule it was following, which was "one hash per phrase is
// not worth a dependency". AES-GCM is the other side of that rule. It is an
// authenticated cipher whose failure modes are silent and total - a repeated
// nonce does not corrupt one message, it hands over the authentication key -
// and hand-rolling one to save a dependency would be trading a small download
// for a class of bug nobody notices until it matters. @noble/ciphers is
// audited, dependency-free and pure JavaScript, so it adds no native surface
// at all.
//
// expo-crypto IS native, and is here for exactly one call: real randomness
// for the nonce. React Native ships no crypto.getRandomValues, and
// Math.random is not a source of nonces - it is seeded, predictable, and
// repeats. That is the one thing AES-GCM cannot survive, so it is the one
// thing worth a native module.
import { gcm } from '@noble/ciphers/aes.js';
import { getRandomBytes } from 'expo-crypto';

/** Mirrors relay.ProxyCall. */
export interface ProxyCall {
  method: string;
  path: string;
  /** Base64, as the Go side's []byte field is on the wire. */
  body?: string;
  authorization?: string;
}

/** Mirrors relay.ProxyResult. */
export interface ProxyResult {
  status: number;
  /** Base64, as above. */
  body?: string;
}

/** relay.nonceLen. AES-GCM's standard nonce size. */
const NONCE_LEN = 12;

const utf8 = (s: string): Uint8Array => {
  // Written out rather than TextEncoder: Hermes has one, but seedphrase.ts
  // already hand-rolls this for the same reason (one fewer thing that has to
  // be present on every runtime this ships to), and the two must agree.
  const out: number[] = [];
  for (const ch of s) {
    const cp = ch.codePointAt(0)!;
    if (cp < 0x80) out.push(cp);
    else if (cp < 0x800) out.push(0xc0 | (cp >> 6), 0x80 | (cp & 63));
    else if (cp < 0x10000) out.push(0xe0 | (cp >> 12), 0x80 | ((cp >> 6) & 63), 0x80 | (cp & 63));
    else out.push(0xf0 | (cp >> 18), 0x80 | ((cp >> 12) & 63), 0x80 | ((cp >> 6) & 63), 0x80 | (cp & 63));
  }
  return new Uint8Array(out);
};

const fromUtf8 = (b: Uint8Array): string => {
  let out = '';
  for (let i = 0; i < b.length; ) {
    const c = b[i];
    if (c < 0x80) {
      out += String.fromCodePoint(c);
      i += 1;
    } else if (c < 0xe0) {
      out += String.fromCodePoint(((c & 31) << 6) | (b[i + 1] & 63));
      i += 2;
    } else if (c < 0xf0) {
      out += String.fromCodePoint(((c & 15) << 12) | ((b[i + 1] & 63) << 6) | (b[i + 2] & 63));
      i += 3;
    } else {
      out += String.fromCodePoint(
        ((c & 7) << 18) | ((b[i + 1] & 63) << 12) | ((b[i + 2] & 63) << 6) | (b[i + 3] & 63),
      );
      i += 4;
    }
  }
  return out;
};

/**
 * requestAAD and responseAAD mirror the functions of the same names in
 * protocol.go: the routing fields that necessarily travel in the clear,
 * bound into the seal so a relay cannot redirect a frame and have it open
 * somewhere else. The \x00 separator and the per-direction label both matter
 * - the label is what stops an answer being replayed as a question.
 */
const requestAAD = (requestId: string, target: string): Uint8Array =>
  utf8(`proxy-request\x00${requestId}\x00${target}`);

const responseAAD = (requestId: string): Uint8Array => utf8(`proxy-response\x00${requestId}`);

/** Base64 of the raw bytes, the shape encoding/json gives a Go []byte. */
function toBase64(bytes: Uint8Array): string {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';
  let out = '';
  for (let i = 0; i < bytes.length; i += 3) {
    const a = bytes[i];
    const b = i + 1 < bytes.length ? bytes[i + 1] : 0;
    const c = i + 2 < bytes.length ? bytes[i + 2] : 0;
    out += alphabet[a >> 2];
    out += alphabet[((a & 3) << 4) | (b >> 4)];
    out += i + 1 < bytes.length ? alphabet[((b & 15) << 2) | (c >> 6)] : '=';
    out += i + 2 < bytes.length ? alphabet[c & 63] : '=';
  }
  return out;
}

function fromBase64(s: string): Uint8Array {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';
  const clean = s.replace(/=+$/, '');
  const out = new Uint8Array((clean.length * 3) >> 2);
  let acc = 0;
  let bits = 0;
  let n = 0;
  for (const ch of clean) {
    const v = alphabet.indexOf(ch);
    if (v < 0) continue;
    acc = (acc << 6) | v;
    bits += 6;
    if (bits >= 8) {
      bits -= 8;
      out[n++] = (acc >> bits) & 0xff;
    }
  }
  return out.subarray(0, n);
}

/**
 * seal encrypts one payload, returning base64 of nonce||ciphertext||tag - the
 * exact framing relay.seal writes, since the Go side reads the first
 * NONCE_LEN bytes as the nonce and hands the rest to GCM.
 */
function seal(key: Uint8Array, aad: Uint8Array, plaintext: Uint8Array): string {
  if (key.length !== 32) throw new Error('relay: frame key must be 32 bytes');
  const nonce = getRandomBytes(NONCE_LEN);
  const sealed = gcm(key, nonce, aad).encrypt(plaintext);
  const framed = new Uint8Array(NONCE_LEN + sealed.length);
  framed.set(nonce);
  framed.set(sealed, NONCE_LEN);
  return toBase64(framed);
}

/**
 * open reverses seal. It returns null for every failure - a wrong key, a
 * truncated frame, a tampered one - deliberately not distinguishing between
 * them, the same choice relay.ErrSealed makes and for the same reason: the
 * caller's move is identical in every case, and telling them apart tells an
 * attacker which guess was closer.
 */
function open(key: Uint8Array, aad: Uint8Array, sealedB64: string): Uint8Array | null {
  if (key.length !== 32) return null;
  const framed = fromBase64(sealedB64);
  if (framed.length <= NONCE_LEN) return null;
  try {
    return gcm(key, framed.subarray(0, NONCE_LEN), aad).decrypt(framed.subarray(NONCE_LEN));
  } catch {
    return null;
  }
}

/** sealCall seals one outbound call. Mirrors relay.SealCall. */
export function sealCall(key: Uint8Array, requestId: string, target: string, call: ProxyCall): string {
  return seal(key, requestAAD(requestId, target), utf8(JSON.stringify(call)));
}

/** openCall opens an inbound call. Mirrors relay.OpenCall. */
export function openCall(
  key: Uint8Array,
  requestId: string,
  target: string,
  sealedB64: string,
): ProxyCall | null {
  const plain = open(key, requestAAD(requestId, target), sealedB64);
  if (!plain) return null;
  try {
    return JSON.parse(fromUtf8(plain)) as ProxyCall;
  } catch {
    return null;
  }
}

/** sealResult seals one outbound answer. Mirrors relay.SealResult. */
export function sealResult(key: Uint8Array, requestId: string, result: ProxyResult): string {
  return seal(key, responseAAD(requestId), utf8(JSON.stringify(result)));
}

/** openResult opens an inbound answer. Mirrors relay.OpenResult. */
export function openResult(key: Uint8Array, requestId: string, sealedB64: string): ProxyResult | null {
  const plain = open(key, responseAAD(requestId), sealedB64);
  if (!plain) return null;
  try {
    return JSON.parse(fromUtf8(plain)) as ProxyResult;
  } catch {
    return null;
  }
}

/** Exported for the cross-implementation test, which needs a fixed nonce to
 *  compare against a fixed Go vector rather than a fresh random one. */
export const __testing = { seal, open, requestAAD, responseAAD, toBase64, fromBase64, utf8, NONCE_LEN };
