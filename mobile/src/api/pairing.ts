// Decodes a pairing-code string (pasted, or read off a scanned QR) into the
// offer it carries. Mirrors internal/api/routes_pairing.go's encodeOffer:
// base64.RawURLEncoding of {"n":name,"u":url,"t":token}. Decoded by hand
// rather than via atob/TextDecoder - neither is guaranteed present on every
// Hermes/React Native version this app might run on - so onboarding never
// depends on that.
export interface PairingOffer {
  name: string;
  url: string;
  token: string;
}

const BASE64URL_ALPHABET = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_';

function base64UrlToBytes(input: string): number[] {
  const bytes: number[] = [];
  let buffer = 0;
  let bits = 0;
  for (const ch of input) {
    const idx = BASE64URL_ALPHABET.indexOf(ch);
    if (idx === -1) continue;
    buffer = (buffer << 6) | idx;
    bits += 6;
    if (bits >= 8) {
      bits -= 8;
      bytes.push((buffer >> bits) & 0xff);
    }
  }
  return bytes;
}

// utf8BytesToString decodes the standard 1-4 byte UTF-8 sequences by hand -
// the offer's own fields (a hostname, a URL, a hex token) are ASCII in
// practice, but an instance NAME comes from os.Hostname() on the server and
// could in principle carry anything.
function utf8BytesToString(bytes: number[]): string {
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

// decodePairingCode returns the offer a pairing-code carries, or null if the
// string isn't one - the address-only QR on the SAME Access tab (a bare
// URL, see routes_remote.go's remoteAddresses/renderQR) is the other shape
// this app's QR scanner can receive, and gets treated as a plain address
// instead once this returns null for it.
export function decodePairingCode(code: string): PairingOffer | null {
  try {
    const bytes = base64UrlToBytes(code.trim());
    if (bytes.length === 0) return null;
    const json = utf8BytesToString(bytes);
    const parsed = JSON.parse(json);
    if (parsed && typeof parsed.u === 'string' && typeof parsed.t === 'string' && parsed.u && parsed.t) {
      return { name: typeof parsed.n === 'string' ? parsed.n : '', url: parsed.u, token: parsed.t };
    }
    return null;
  } catch {
    return null;
  }
}
