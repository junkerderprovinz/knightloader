// The phone's half of internal/relay/member.go: the id a member key makes, the
// message a member signs for a call, and the signature. The phone's relay id
// is made from its key, and an instance takes a call under that id only with a
// signature by it, so the id and the message have to match the Go side byte
// for byte. check-member-key.mjs holds both to the vector in
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
  authorization?: string;
  from: string;
  kind: string;
  sent: number;
}

const CALL_DOMAIN = 'knightloader/relay/member-call/v1\x00';

/** Eight bytes, big-endian. Lengths and the time stay far below 2^53. */
function uint64(n: number): number[] {
  const hi = Math.floor(n / 0x100000000);
  const lo = n >>> 0;
  return [hi >>> 24, (hi >>> 16) & 255, (hi >>> 8) & 255, hi & 255, lo >>> 24, (lo >>> 16) & 255, (lo >>> 8) & 255, lo & 255];
}

/** Mirrors relay.CallMessage: every field with its length, then the time. */
export function callMessage(target: string, call: SignedFields): Uint8Array {
  const out: number[] = utf8ToBytes(CALL_DOMAIN);
  for (const field of [
    call.id,
    target,
    call.method,
    call.path,
    call.body ?? '',
    call.authorization ?? '',
    call.from,
    call.kind,
  ]) {
    const bytes = utf8ToBytes(field);
    out.push(...uint64(bytes.length));
    // One at a time: a body of pasted links outgrows a spread's argument limit.
    for (const b of bytes) out.push(b);
  }
  out.push(...uint64(call.sent));
  return new Uint8Array(out);
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
