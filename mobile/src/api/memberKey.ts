// The phone's half of internal/relay/member.go: the message a member signs for
// a call, and the signature. The instance pins this phone's public key to its
// relay id on the first signed call and from then on accepts no other key for
// that id, so the message has to match the Go side byte for byte.
// check-member-key.mjs holds it to the vector in internal/relay/member_test.go.
//
// Ed25519 comes from @noble/curves, from the same audited family as the
// cipher in relayFrame.ts; React Native ships no WebCrypto to do it.
import { ed25519 } from '@noble/curves/ed25519.js';
import { utf8ToBytes } from './base64';

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

/** Signs a call for target with the member key seed makes. */
export function signCall(seed: Uint8Array, target: string, call: SignedFields): Uint8Array {
  return ed25519.sign(callMessage(target, call), seed);
}
