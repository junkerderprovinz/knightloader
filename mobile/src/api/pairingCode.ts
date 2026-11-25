// Reads the QR code the web UI shows beside the phrase, as relay.PairingCode
// writes it: the twelve words, and on a second line the relay the instance
// dials when that is not the project relay. A code without that line pairs on
// DEFAULT_RELAY_URL, the same as typed words.
//
// Imports nothing at run time, so check-pairing-code.mjs can run it in node.

export type PairingCode =
  | { words: string; relay?: string }
  | {
      words: string;
      /** A relay the code names that this app will not dial. */
      refused: string;
    };

const NAMED_RELAY = /\s(\S+:\/\/\S*)\s*$/;

export function readPairingCode(text: string): PairingCode {
  const m = text.match(NAMED_RELAY);
  if (!m || m.index === undefined) return { words: text.trim() };
  const words = text.slice(0, m.index).trim();
  const relay = secureRelay(m[1]);
  return relay ? { words, relay } : { words, refused: m[1] };
}

// The socket a typed relay address dials, or null for one the app will not
// dial, the same rule as the extension's relayDialURL: https becomes wss, and a
// path that does not end in /connect gets /relay/connect below it, which is
// what relay.PairingCode writes for the same address.
export function relayAddress(address: string): string | null {
  const m = address.trim().match(/^(?:https|wss):\/\/([^/?#@\s]+)((?:\/[^?#\s]*)?)$/i);
  if (!m) return null;
  const path = m[2].replace(/\/+$/, '');
  return `wss://${m[1]}${path.endsWith('/connect') ? path : `${path}/relay/connect`}`;
}

// wss only. The app permits no cleartext traffic, and an address that arrives
// in a QR code is one nobody on the phone typed or looked at.
function secureRelay(address: string): string | null {
  const m = address.match(/^wss:\/\/([^/?#@]+)(\/[^?#]*)?$/i);
  return m ? `wss://${m[1]}${m[2] ?? ''}` : null;
}
