// The phone's half of the connection phrase.
//
// It decodes locally rather than asking a server, because the whole point of
// the phrase is the case where this phone cannot reach any instance yet -
// there is nobody to ask. A port of internal/seedphrase, deliberately line
// for line: the two must agree on every one of the 2048 words and on the bit
// packing, or the key derived here quietly differs from the server's and the
// symptom is "the relay never connects" with nothing in any log.
//
// The UI must never call this a wallet seed. It is a Verbindungsphrase.

import { sha256, toHex, utf8 } from './sha256';
import { WORDS } from './wordlist';

export const SECRET_LEN = 16;
export const WORD_COUNT = 12;
const BITS_PER_WORD = 11;
const CHECKSUM_BITS = (SECRET_LEN * 8) / 32; // BIP39's own rule

// relay.DefaultRelayURL, compiled in on the server side for the same reason
// it is a constant here: it is what keeps a phrase twelve words instead of a
// URL plus a key.
export const DEFAULT_RELAY_URL = 'wss://relay.knightloader.app/relay/connect';

// relay.keyDomain. Changing this string orphans every phrase in existence.
const KEY_DOMAIN = 'knightloader/relay/group-key/v1';

const INDEX = new Map<string, number>(WORDS.map((w, i) => [w, i]));

/** Why a phrase was refused, so the caller can say so in the reader's language. */
export type PhraseProblem =
  | { reason: 'word_count'; count: number }
  | { reason: 'unknown_word'; word: string; position: number }
  | { reason: 'checksum' };

export class PhraseError extends Error {
  problem: PhraseProblem;
  constructor(problem: PhraseProblem) {
    super(problem.reason);
    this.name = 'PhraseError';
    this.problem = problem;
  }
}

/** bitsAt reads count bits from b starting at bit offset, most significant first. */
function bitsAt(b: Uint8Array, offset: number, count: number): number {
  let v = 0;
  for (let i = 0; i < count; i++) {
    const bit = offset + i;
    v <<= 1;
    if (b[bit >> 3] & (1 << (7 - (bit & 7)))) v |= 1;
  }
  return v;
}

/** setBits is bitsAt's inverse; the target bits start clear. */
function setBits(b: Uint8Array, offset: number, count: number, v: number): void {
  for (let i = 0; i < count; i++) {
    if (v & (1 << (count - 1 - i))) {
      const bit = offset + i;
      b[bit >> 3] |= 1 << (7 - (bit & 7));
    }
  }
}

/**
 * decodePhrase parses twelve words back into the secret they carry.
 *
 * Input is normalised first, because it arrives from a paste, a QR scan, or
 * somebody typing what was read to them: case is ignored and any run of
 * whitespace counts as one separator.
 *
 * Throws PhraseError, never a bare string, so the screen can pick a
 * translated sentence instead of showing one this file chose.
 */
export function decodePhrase(phrase: string): Uint8Array {
  const got = phrase.trim().toLowerCase().split(/\s+/).filter(Boolean);
  if (got.length !== WORD_COUNT) {
    throw new PhraseError({ reason: 'word_count', count: got.length });
  }

  const full = new Uint8Array(SECRET_LEN + 1);
  for (let i = 0; i < got.length; i++) {
    const idx = INDEX.get(got[i]);
    if (idx === undefined) {
      // Naming the word and its position is the point: bisecting a
      // twelve-word phrase by hand is not a thing to ask of anybody.
      throw new PhraseError({ reason: 'unknown_word', word: got[i], position: i + 1 });
    }
    setBits(full, i * BITS_PER_WORD, BITS_PER_WORD, idx);
  }

  const secret = full.slice(0, SECRET_LEN);
  const sum = sha256(secret);
  const want = bitsAt(sum.slice(0, 1), 0, CHECKSUM_BITS);
  const have = bitsAt(full, SECRET_LEN * 8, CHECKSUM_BITS);
  if (want !== have) throw new PhraseError({ reason: 'checksum' });
  return secret;
}

/**
 * deriveKey turns the secret into what the relay is actually told.
 *
 * The secret never travels. The relay matches connections presenting the same
 * derived key and cannot work backwards to the words, which is what lets
 * somebody else run the relay without being able to reconstruct anybody's
 * phrase - including us.
 */
export function deriveKey(secret: Uint8Array): string {
  const domain = utf8(KEY_DOMAIN);
  const buf = new Uint8Array(domain.length + secret.length);
  buf.set(domain);
  buf.set(secret, domain.length);
  return toHex(sha256(buf));
}

/** keyFromPhrase is the whole journey, for the one caller that wants it. */
export function keyFromPhrase(phrase: string): string {
  return deriveKey(decodePhrase(phrase));
}

/**
 * FRAME_KEY_DOMAIN is the second domain over the same secret, mirroring
 * internal/relay/key.go's frameDomain. Both strings must match byte for byte
 * or this phone joins a group it cannot talk to.
 */
const FRAME_KEY_DOMAIN = 'knightloader/relay/frame-key/v1';

/**
 * deriveFrameKey returns the 32-byte key that seals proxy frames, mirroring
 * relay.DeriveFrameKey.
 *
 * The separate domain is the entire point and is worth restating here, where
 * somebody reading only the phone's half will see it: the relay is HANDED
 * deriveKey's output in every hello frame. A frame key derived from that
 * value, or under the same domain, would be a key the relay already holds,
 * and the encryption would protect nothing from the one party it is aimed at.
 */
export function deriveFrameKey(secret: Uint8Array): Uint8Array {
  const domain = utf8(FRAME_KEY_DOMAIN);
  const buf = new Uint8Array(domain.length + secret.length);
  buf.set(domain);
  buf.set(secret, domain.length);
  return sha256(buf);
}

/** frameKeyFromPhrase is deriveFrameKey's counterpart to keyFromPhrase. */
export function frameKeyFromPhrase(phrase: string): Uint8Array {
  return deriveFrameKey(decodePhrase(phrase));
}
