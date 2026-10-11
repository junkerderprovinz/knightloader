// The phone's half of internal/relay/member.go: the id a member key makes, the
// messages a member signs, and the signatures. The phone's relay id is made
// from its key, an instance takes a call under that id only with a signature
// by it, and the phone takes an instance's answer only signed by the key the
// instance's id is made from. Every message has to match the Go side byte for
// byte; check-member-key.mjs holds them to the vectors in
// internal/relay/member_test.go.
//
// Ed25519 comes from @noble/curves, from the same audited family as the
// cipher in relayFrame.ts; React Native ships no WebCrypto to do it.
import { ed25519 } from '@noble/curves/ed25519.js';
import { utf8ToBytes } from './base64';
import { sha256, toHex } from './sha256';

/** The fields of relay.ProxyCall a signature covers. body is the text the call
 *  carries, which travels as its UTF-8 bytes. */
export interface SignedFields {
  id: string;
  method: string;
  path: string;
  body?: string;
  from: string;
  kind: string;
  sent: number;
}

const CALL_DOMAIN = 'knightloader/relay/member-call/v2\x00';
const RESULT_DOMAIN = 'knightloader/relay/member-result/v1\x00';
const HELLO_DOMAIN = 'knightloader/relay/member-hello/v1\x00';

/** Eight bytes, big-endian. Lengths, statuses and the time stay far below 2^53. */
function uint64(n: number): number[] {
  const hi = Math.floor(n / 0x100000000);
  const lo = n >>> 0;
  return [hi >>> 24, (hi >>> 16) & 255, (hi >>> 8) & 255, hi & 255, lo >>> 24, (lo >>> 16) & 255, (lo >>> 8) & 255, lo & 255];
}

/** Mirrors signedMessage in relay/member.go: the domain, every field with its length,
 *  then n. */
function signedMessage(domain: string, n: number, fields: (string | number[])[]): Uint8Array {
  const out: number[] = utf8ToBytes(domain);
  for (const field of fields) {
    const bytes = typeof field === 'string' ? utf8ToBytes(field) : field;
    out.push(...uint64(bytes.length));
    // One at a time: a body of pasted links outgrows a spread's argument limit.
    for (const b of bytes) out.push(b);
  }
  out.push(...uint64(n));
  return new Uint8Array(out);
}

/** Mirrors relay.CallMessage. */
export function callMessage(target: string, call: SignedFields): Uint8Array {
  return signedMessage(CALL_DOMAIN, call.sent, [call.id, target, call.method, call.path, call.body ?? '', call.from, call.kind]);
}

/** Mirrors relay.ResultMessage: what instance from signs when it answers
 *  requestId. body is the answer's raw bytes. */
export function resultMessage(requestId: string, from: string, status: number, body: number[]): Uint8Array {
  return signedMessage(RESULT_DOMAIN, status, [requestId, from, body]);
}

/** Mirrors relay.HelloMessage. */
export function helloMessage(relayKey: string, id: string, kind: string, sent: number): Uint8Array {
  return signedMessage(HELLO_DOMAIN, sent, [relayKey, id, kind]);
}

/** The public half of the member key a 32-byte seed makes. */
export function memberPublicKey(seed: Uint8Array): Uint8Array {
  return ed25519.getPublicKey(seed);
}

const ID_PREFIX: Record<string, string> = { mobile: 'phone-', extension: 'browser-' };

/** Mirrors relay.MemberID: the id a member of kind goes by with publicKey. An
 *  instance's is the first 20 bytes of the key's SHA-256 in hex, a phone's and
 *  a browser's the whole hash after their prefix. */
export function memberId(kind: 'instance' | 'mobile' | 'extension', publicKey: Uint8Array): string {
  const hash = toHex(sha256(publicKey));
  return kind === 'instance' ? hash.slice(0, 40) : ID_PREFIX[kind] + hash;
}

/** Signs a call for target with the member key seed makes. */
export function signCall(seed: Uint8Array, target: string, call: SignedFields): Uint8Array {
  return ed25519.sign(callMessage(target, call), seed);
}

/** Mirrors relay.ProveID: the signature with which this phone proves on the
 *  relay key relayKey that id is its own, so the relay lets it take the place
 *  of a connection it left behind. */
export function proveId(seed: Uint8Array, relayKey: string, id: string, sent: number): Uint8Array {
  return ed25519.sign(helloMessage(relayKey, id, 'mobile', sent), seed);
}

/** Mirrors relay.VerifyResult: whether an answer to requestId is target's,
 *  signed by the key target's instance id is made from. */
export function verifyResult(
  requestId: string,
  target: string,
  status: number,
  body: number[],
  key: Uint8Array,
  sig: Uint8Array,
): boolean {
  if (key.length !== 32 || sig.length !== 64 || memberId('instance', key) !== target) return false;
  try {
    return ed25519.verify(sig, resultMessage(requestId, target, status, body), key);
  } catch {
    return false;
  }
}
