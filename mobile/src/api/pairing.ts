import { base64Decode, bytesToUtf8 } from './base64';

// Decodes a pairing-code string (pasted, or read off a scanned QR) into the
// offer it carries. Mirrors internal/api/routes_pairing.go's encodeOffer:
// base64.RawURLEncoding of {"n":name,"u":url,"t":token}. The base64 and UTF-8
// work lives in api/base64.ts, shared with the relay client rather than
// carried twice - see that file for why neither is taken from the engine.
export interface PairingOffer {
  name: string;
  url: string;
  token: string;
}

// decodePairingCode returns the offer a pairing-code carries, or null if the
// string isn't one - the address-only QR on the SAME Access tab (a bare
// URL, see routes_remote.go's remoteAddresses/renderQR) is the other shape
// this app's QR scanner can receive, and gets treated as a plain address
// instead once this returns null for it.
export function decodePairingCode(code: string): PairingOffer | null {
  try {
    const bytes = base64Decode(code.trim());
    if (bytes.length === 0) return null;
    const parsed = JSON.parse(bytesToUtf8(bytes));
    if (parsed && typeof parsed.u === 'string' && typeof parsed.t === 'string' && parsed.u && parsed.t) {
      return { name: typeof parsed.n === 'string' ? parsed.n : '', url: parsed.u, token: parsed.t };
    }
    return null;
  } catch {
    return null;
  }
}
