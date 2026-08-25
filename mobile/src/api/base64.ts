// Base64 and UTF-8 by hand, in both directions.
//
// By hand rather than via atob/btoa/TextEncoder because none of those is
// guaranteed present on every Hermes/React Native version this app might run
// on, and both things that need them here - decoding a pairing code during
// onboarding (api/pairing.ts) and carrying request bodies over the relay
// (api/relayClient.ts) - are load-bearing enough that "works on some engine
// builds" is not good enough.
//
// Both base64 alphabets are handled by one pair of functions: Go's
// encoding/json marshals a []byte with StdEncoding (+ / =), which is what
// every relay frame body uses, while routes_pairing.go encodes its offer with
// RawURLEncoding (- _ and no padding). Decoding accepts either, so a caller
// never has to know which one produced the string it is holding.

const STD = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';

// Lookup table rather than indexOf per character: decoding a task-list body
// runs this once per character of a payload that can be tens of kilobytes.
const VALUE: Record<string, number> = {};
for (let i = 0; i < STD.length; i++) VALUE[STD[i]] = i;
// The URL-safe alphabet differs in exactly two characters.
VALUE['-'] = 62;
VALUE['_'] = 63;

/** Accepts standard or URL-safe base64, padded or not. */
export function base64Decode(input: string): number[] {
  const bytes: number[] = [];
  let buffer = 0;
  let bits = 0;
  for (const ch of input.trim()) {
    if (ch === '=') break;
    const v = VALUE[ch];
    // Unknown characters (line breaks in a wrapped payload, stray spaces) are
    // skipped rather than treated as an error: every real caller here is
    // decoding something a machine produced, and a decoder that throws on
    // whitespace would fail on a perfectly valid payload someone pasted.
    if (v === undefined) continue;
    buffer = (buffer << 6) | v;
    bits += 6;
    if (bits >= 8) {
      bits -= 8;
      bytes.push((buffer >> bits) & 0xff);
    }
  }
  return bytes;
}

/** Standard base64, padded - what Go's encoding/json expects for a []byte. */
export function base64Encode(bytes: number[]): string {
  let out = '';
  for (let i = 0; i < bytes.length; i += 3) {
    const b0 = bytes[i], b1 = bytes[i + 1], b2 = bytes[i + 2];
    out += STD[b0 >> 2];
    out += STD[((b0 & 3) << 4) | ((b1 ?? 0) >> 4)];
    out += b1 === undefined ? '=' : STD[((b1 & 15) << 2) | ((b2 ?? 0) >> 6)];
    out += b2 === undefined ? '=' : STD[b2 & 63];
  }
  return out;
}

/**
 * Decodes the standard 1-4 byte UTF-8 sequences. Needed in full rather than
 * assuming ASCII: a task name, an error message or an instance name can carry
 * anything, and a decoder that only handled ASCII would corrupt exactly the
 * payloads a person notices.
 */
export function bytesToUtf8(bytes: number[]): string {
  let out = '';
  let i = 0;
  while (i < bytes.length) {
    const b0 = bytes[i++];
    if (b0 < 0x80) {
      out += String.fromCharCode(b0);
    } else if (b0 >= 0xc0 && b0 < 0xe0 && i < bytes.length) {
      const b1 = bytes[i++];
      out += String.fromCharCode(((b0 & 0x1f) << 6) | (b1 & 0x3f));
    } else if (b0 >= 0xe0 && b0 < 0xf0 && i + 1 < bytes.length) {
      const b1 = bytes[i++];
      const b2 = bytes[i++];
      out += String.fromCharCode(((b0 & 0x0f) << 12) | ((b1 & 0x3f) << 6) | (b2 & 0x3f));
    } else if (b0 >= 0xf0 && i + 2 < bytes.length) {
      const b1 = bytes[i++];
      const b2 = bytes[i++];
      const b3 = bytes[i++];
      const cp = ((b0 & 0x07) << 18) | ((b1 & 0x3f) << 12) | ((b2 & 0x3f) << 6) | (b3 & 0x3f);
      const c = cp - 0x10000;
      out += String.fromCharCode(0xd800 + (c >> 10), 0xdc00 + (c & 0x3ff));
    } else {
      out += String.fromCharCode(b0);
    }
  }
  return out;
}

/** The inverse: a JS string to its UTF-8 bytes, surrogate pairs included. */
export function utf8ToBytes(input: string): number[] {
  const out: number[] = [];
  for (let i = 0; i < input.length; i++) {
    let cp = input.charCodeAt(i);
    // A high surrogate followed by a low one is one codepoint, not two.
    if (cp >= 0xd800 && cp <= 0xdbff && i + 1 < input.length) {
      const next = input.charCodeAt(i + 1);
      if (next >= 0xdc00 && next <= 0xdfff) {
        cp = 0x10000 + ((cp - 0xd800) << 10) + (next - 0xdc00);
        i++;
      }
    }
    if (cp < 0x80) {
      out.push(cp);
    } else if (cp < 0x800) {
      out.push(0xc0 | (cp >> 6), 0x80 | (cp & 0x3f));
    } else if (cp < 0x10000) {
      out.push(0xe0 | (cp >> 12), 0x80 | ((cp >> 6) & 0x3f), 0x80 | (cp & 0x3f));
    } else {
      out.push(0xf0 | (cp >> 18), 0x80 | ((cp >> 12) & 0x3f), 0x80 | ((cp >> 6) & 0x3f), 0x80 | (cp & 0x3f));
    }
  }
  return out;
}

/** Convenience for the relay's frame bodies, which are always JSON text. */
export const encodeBody = (text: string): string => base64Encode(utf8ToBytes(text));
export const decodeBody = (b64: string): string => bytesToUtf8(base64Decode(b64));
