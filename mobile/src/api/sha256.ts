// SHA-256, by hand, because the phrase has to be decoded on the phone before
// there is any server to ask.
//
// Not expo-crypto: that is a native module, and this app spent a release
// removing native surface it did not use (the manifest audit that found the
// microphone and overlay permissions). One hash per phrase somebody types is
// not a reason to add a native dependency back, or to re-open a build that
// has already fought the Android NDK once.
//
// Checked against the Go side rather than trusted: internal/seedphrase's own
// vectors are run through this file and compared, so a phrase this app
// derives a key from lands on the same key the server does. A quiet
// disagreement here would look exactly like "the relay never connects".

// The first 32 bits of the fractional parts of the cube roots of the first
// 64 primes - FIPS 180-4's own table, not something to be regenerated.
const K = new Uint32Array([
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
  0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
  0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
  0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
  0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
  0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
]);

function rotr(x: number, n: number): number {
  return ((x >>> n) | (x << (32 - n))) >>> 0;
}

/** sha256 hashes bytes and returns the 32-byte digest. */
export function sha256(input: Uint8Array): Uint8Array {
  const h = new Uint32Array([
    0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19,
  ]);

  // Pad: a 0x80 byte, then zeroes, then the length in BITS as a 64-bit
  // big-endian number, to a multiple of 64 bytes.
  const bitLen = input.length * 8;
  const padded = new Uint8Array(Math.ceil((input.length + 9) / 64) * 64);
  padded.set(input);
  padded[input.length] = 0x80;
  // A phrase secret is 16 bytes, so the high half of the length is always
  // zero here - written out anyway, because a helper that is only correct
  // for short inputs is a trap for whoever reuses it.
  const view = new DataView(padded.buffer);
  view.setUint32(padded.length - 8, Math.floor(bitLen / 0x100000000), false);
  view.setUint32(padded.length - 4, bitLen >>> 0, false);

  const w = new Uint32Array(64);
  for (let off = 0; off < padded.length; off += 64) {
    for (let i = 0; i < 16; i++) w[i] = view.getUint32(off + i * 4, false);
    for (let i = 16; i < 64; i++) {
      const s0 = rotr(w[i - 15], 7) ^ rotr(w[i - 15], 18) ^ (w[i - 15] >>> 3);
      const s1 = rotr(w[i - 2], 17) ^ rotr(w[i - 2], 19) ^ (w[i - 2] >>> 10);
      w[i] = (w[i - 16] + s0 + w[i - 7] + s1) >>> 0;
    }

    let [a, b, c, d, e, f, g, hh] = h;
    for (let i = 0; i < 64; i++) {
      const S1 = rotr(e, 6) ^ rotr(e, 11) ^ rotr(e, 25);
      const ch = (e & f) ^ (~e & g);
      const t1 = (hh + S1 + ch + K[i] + w[i]) >>> 0;
      const S0 = rotr(a, 2) ^ rotr(a, 13) ^ rotr(a, 22);
      const maj = (a & b) ^ (a & c) ^ (b & c);
      const t2 = (S0 + maj) >>> 0;
      hh = g;
      g = f;
      f = e;
      e = (d + t1) >>> 0;
      d = c;
      c = b;
      b = a;
      a = (t1 + t2) >>> 0;
    }
    h[0] = (h[0] + a) >>> 0;
    h[1] = (h[1] + b) >>> 0;
    h[2] = (h[2] + c) >>> 0;
    h[3] = (h[3] + d) >>> 0;
    h[4] = (h[4] + e) >>> 0;
    h[5] = (h[5] + f) >>> 0;
    h[6] = (h[6] + g) >>> 0;
    h[7] = (h[7] + hh) >>> 0;
  }

  const out = new Uint8Array(32);
  const outView = new DataView(out.buffer);
  for (let i = 0; i < 8; i++) outView.setUint32(i * 4, h[i], false);
  return out;
}

/** toHex renders bytes the way the relay key is written on the wire. */
export function toHex(b: Uint8Array): string {
  let s = '';
  for (const x of b) s += x.toString(16).padStart(2, '0');
  return s;
}

/**
 * fromHex reverses toHex, for the frame key a saved relay connection stores
 * as hex (types.ts's relayFrameKey).
 *
 * Anything that is not an even run of hex digits returns empty rather than a
 * half-decoded key: a short key is rejected outright by the seal, which is a
 * clear failure, while a silently truncated one would be a valid-looking key
 * that simply never opens anything.
 */
export function fromHex(s: string): Uint8Array {
  if (s.length % 2 !== 0 || !/^[0-9a-fA-F]*$/.test(s)) return new Uint8Array(0);
  const out = new Uint8Array(s.length / 2);
  for (let i = 0; i < out.length; i++) out[i] = parseInt(s.slice(i * 2, i * 2 + 2), 16);
  return out;
}

/** utf8 encodes a string as bytes, for the one ASCII domain string hashed here. */
export function utf8(s: string): Uint8Array {
  const out = new Uint8Array(s.length * 4);
  let n = 0;
  for (const ch of s) {
    let cp = ch.codePointAt(0) as number;
    if (cp < 0x80) {
      out[n++] = cp;
    } else if (cp < 0x800) {
      out[n++] = 0xc0 | (cp >> 6);
      out[n++] = 0x80 | (cp & 0x3f);
    } else if (cp < 0x10000) {
      out[n++] = 0xe0 | (cp >> 12);
      out[n++] = 0x80 | ((cp >> 6) & 0x3f);
      out[n++] = 0x80 | (cp & 0x3f);
    } else {
      out[n++] = 0xf0 | (cp >> 18);
      out[n++] = 0x80 | ((cp >> 12) & 0x3f);
      out[n++] = 0x80 | ((cp >> 6) & 0x3f);
      out[n++] = 0x80 | (cp & 0x3f);
    }
  }
  return out.slice(0, n);
}
