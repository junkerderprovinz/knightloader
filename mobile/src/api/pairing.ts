import { base64Decode, bytesToUtf8 } from './base64';

// Decodes a pairing-code string (pasted, or read off a scanned QR) into the
// offer it carries. Mirrors internal/api/routes_pairing.go's encodeOffer:
// base64.RawURLEncoding of {"n":name,"u":url,"t":token}. The base64 and UTF-8
// work lives in api/base64.ts, shared with the relay client rather than
// carried twice - see that file for why neither is taken from the engine.
export interface PairingOffer {
  name: string;
  /** The direct address, empty for a code that can only be redeemed over a relay. */
  url: string;
  /**
   * The issuing instance's own id on its relay, empty for a plain code.
   *
   * The app cannot redeem such a code by itself - it has no federation of its
   * own to register the other side with - but it must still RECOGNISE one,
   * because the alternative is worse: an unrecognised code falls through to
   * "this is a plain address" and the whole base64 blob lands in the address
   * field, which is exactly the bug the QR scanner had before decodePairingCode
   * existed.
   */
  relayId: string;
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
    if (!parsed || typeof parsed.t !== 'string' || !parsed.t) return null;
    const url = typeof parsed.u === 'string' ? parsed.u : '';
    const relayId = typeof parsed.r === 'string' ? parsed.r : '';
    // Either way in makes it a pairing code. Requiring the URL is what made a
    // relay-only code read as "not a pairing code" and get treated as a plain
    // address instead.
    if (!url && !relayId) return null;
    return { name: typeof parsed.n === 'string' ? parsed.n : '', url, relayId, token: parsed.t };
  } catch {
    return null;
  }
}
