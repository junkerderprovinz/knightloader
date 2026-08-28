/**
 * The extension's half of the connection phrase.
 *
 * A port of internal/seedphrase and the phone's own mobile/src/api/seedphrase.ts,
 * deliberately line for line: all three have to agree on every one of the 2048
 * words and on the bit packing, or the key derived here quietly differs from the
 * server's and the symptom is "the relay never connects" with nothing in any log.
 *
 * It decodes locally rather than asking a server, for the same reason the phone
 * does: the whole point of the phrase is the case where this browser cannot
 * reach any instance yet — there is nobody to ask.
 *
 * The one deliberate difference from the phone: hashing is WebCrypto, so
 * everything here is async. The phone hand-rolls SHA-256 because its runtime has
 * no reliable subtle crypto; a browser extension always does, and reimplementing
 * a hash to keep a signature synchronous would be trading a real risk for a
 * cosmetic one.
 *
 * The UI must never call this a wallet seed. It is a Verbindungsphrase.
 */

const SECRET_LEN = 16;
const WORD_COUNT = 12;
const BITS_PER_WORD = 11;
const CHECKSUM_BITS = (SECRET_LEN * 8) / 32; // BIP39's own rule

/** relay.DefaultRelayURL. Compiled in on the server for the same reason it is a
 *  constant here: it is what keeps a phrase twelve words instead of a URL plus
 *  a key. */
const DEFAULT_RELAY_URL = 'wss://relay.knightloader.app/relay/connect';

/** relay.keyDomain. Changing this string orphans every phrase in existence. */
const KEY_DOMAIN = 'knightloader/relay/group-key/v1';

/**
 * The second domain over the same secret, mirroring internal/relay/key.go's
 * frameDomain.
 *
 * The separate domain is the entire point, and worth restating where somebody
 * reading only this file will see it: the relay is HANDED the group key in every
 * hello frame. A frame key derived from that value, or under the same domain,
 * would be a key the relay already holds — and the encryption would protect
 * nothing from the one party it is aimed at.
 */
const FRAME_KEY_DOMAIN = 'knightloader/relay/frame-key/v1';

const PHRASE_INDEX = new Map(WORDS.map((w, i) => [w, i]));

/** Why a phrase was refused, so the caller can say so in the reader's language. */
class PhraseError extends Error {
  constructor(problem) {
    super(problem.reason);
    this.name = 'PhraseError';
    this.problem = problem;
  }
}

const phraseUtf8 = (s) => new TextEncoder().encode(s);

const toHex = (bytes) => Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');

async function sha256(bytes) {
  return new Uint8Array(await crypto.subtle.digest('SHA-256', bytes));
}

/** bitsAt reads count bits from b starting at bit offset, most significant first. */
function bitsAt(b, offset, count) {
  let v = 0;
  for (let i = 0; i < count; i++) {
    const bit = offset + i;
    v <<= 1;
    if (b[bit >> 3] & (1 << (7 - (bit & 7)))) v |= 1;
  }
  return v;
}

/** setBits is bitsAt's inverse; the target bits start clear. */
function setBits(b, offset, count, v) {
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
 * Throws PhraseError, never a bare string, so the options page can pick a
 * translated sentence instead of showing one this file chose.
 */
async function decodePhrase(phrase) {
  const got = String(phrase).trim().toLowerCase().split(/\s+/).filter(Boolean);
  if (got.length !== WORD_COUNT) {
    throw new PhraseError({ reason: 'word_count', count: got.length });
  }

  const full = new Uint8Array(SECRET_LEN + 1);
  for (let i = 0; i < got.length; i++) {
    const idx = PHRASE_INDEX.get(got[i]);
    if (idx === undefined) {
      // Naming the word and its position is the point: bisecting a twelve-word
      // phrase by hand is not a thing to ask of anybody.
      throw new PhraseError({ reason: 'unknown_word', word: got[i], position: i + 1 });
    }
    setBits(full, i * BITS_PER_WORD, BITS_PER_WORD, idx);
  }

  const secret = full.slice(0, SECRET_LEN);
  const sum = await sha256(secret);
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
 * phrase — including us.
 */
async function deriveKey(secret) {
  const domain = phraseUtf8(KEY_DOMAIN);
  const buf = new Uint8Array(domain.length + secret.length);
  buf.set(domain);
  buf.set(secret, domain.length);
  return toHex(await sha256(buf));
}

/** deriveFrameKey returns the 32-byte key that seals proxy frames, mirroring
 *  relay.DeriveFrameKey. See FRAME_KEY_DOMAIN above for why it is a second
 *  domain rather than the same one. */
async function deriveFrameKey(secret) {
  const domain = phraseUtf8(FRAME_KEY_DOMAIN);
  const buf = new Uint8Array(domain.length + secret.length);
  buf.set(domain);
  buf.set(secret, domain.length);
  return await sha256(buf);
}

/** Both keys from the words, which is what every caller here actually wants. */
async function keysFromPhrase(phrase) {
  const secret = await decodePhrase(phrase);
  return { key: await deriveKey(secret), frameKey: await deriveFrameKey(secret) };
}
