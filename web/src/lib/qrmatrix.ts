import qrcode from 'qrcode-generator';

import type { QRMatrix } from './api';

/**
 * A string as the same module grid the server hands over for a pairing code,
 * so components/QRCode.tsx draws both the same way.
 *
 * Encoding normally stays on the server here, and that rule is not being
 * abandoned: it exists because the pairing QR encodes an address list the same
 * response already carries, and an encoder on each side is two places that can
 * disagree about what was encoded. Neither half of that applies to a donation
 * address. It is a constant in this bundle, it never changes at runtime, and
 * there is no response to be out of step with - asking a server to encode a
 * string the page already has would be a round trip that can fail, in a window
 * that otherwise works with the network unplugged.
 *
 * Type 0 asks for the smallest version that fits and level M is the usual
 * trade for a screen, where a code is not going to be smudged or folded.
 */
export function qrMatrix(value: string): QRMatrix {
  const qr = qrcode(0, 'M');
  qr.addData(value);
  qr.make();
  const size = qr.getModuleCount();
  const bits: string[] = [];
  for (let y = 0; y < size; y++) {
    let row = '';
    for (let x = 0; x < size; x++) row += qr.isDark(y, x) ? '1' : '0';
    bits.push(row);
  }
  return { size, bits };
}
