/**
 * The extension's half of the connection phrase.
 *
 * A line-for-line port of internal/seedphrase and mobile/src/api/seedphrase.ts.
 * All three must agree on the 2048 words and the bit packing, or the derived
 * key differs and the relay never connects, with nothing in any log.
 *
 * It decodes locally because the phrase is for a browser that cannot reach any
 * instance yet. Unlike the phone it hashes with WebCrypto, so it is async.
 *
 * The UI calls this a connection phrase, never a wallet seed.
 */

const SECRET_LEN = 16;
const WORD_COUNT = 12;
const BITS_PER_WORD = 11;
const CHECKSUM_BITS = (SECRET_LEN * 8) / 32; // BIP39's own rule

/** relay.DefaultRelayURL. A fixed relay keeps the phrase at twelve words
 *  instead of a URL plus a key. */
const DEFAULT_RELAY_URL = 'wss://relay.halleluja.design/relay/connect';

/** relay.keyDomain. Changing this string orphans every phrase in existence. */
const KEY_DOMAIN = 'knightloader/relay/group-key/v1';

/**
 * The second domain over the same secret, mirroring frameDomain in
 * internal/relay/key.go. The relay receives the group key in every hello frame,
 * so a frame key under the same domain would be one the relay already holds.
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
 * decodePhrase parses twelve words back into the secret they carry. Case is
 * ignored and any whitespace separates words, since input comes from pastes,
 * QR scans and typing. Throws PhraseError so the caller can show a translated
 * reason.
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
      // The word and its position spare the user a search through twelve.
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
 * deriveKey turns the secret into the key the relay sees. The secret never
 * travels, so whoever runs the relay cannot recover the words.
 */
async function deriveKey(secret) {
  const domain = phraseUtf8(KEY_DOMAIN);
  const buf = new Uint8Array(domain.length + secret.length);
  buf.set(domain);
  buf.set(secret, domain.length);
  return toHex(await sha256(buf));
}

/** deriveFrameKey returns the 32-byte key that seals proxy frames, mirroring
 *  relay.DeriveFrameKey. */
async function deriveFrameKey(secret) {
  const domain = phraseUtf8(FRAME_KEY_DOMAIN);
  const buf = new Uint8Array(domain.length + secret.length);
  buf.set(domain);
  buf.set(secret, domain.length);
  return await sha256(buf);
}

/** Both keys from the words. */
async function keysFromPhrase(phrase) {
  const secret = await decodePhrase(phrase);
  return { key: await deriveKey(secret), frameKey: await deriveFrameKey(secret) };
}
