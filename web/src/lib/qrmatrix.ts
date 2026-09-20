import qrcode from 'qrcode-generator';

import type { QRMatrix } from './api';

/**
 * qrMatrix encodes a string as the module grid the server sends for pairing
 * codes, so components/QRCode.tsx draws both. Pairing codes are encoded on the
 * server beside the addresses they carry; this is for constants such as the
 * donation addresses, which need no round trip. Type 0 picks the smallest
 * version; level M suits a screen.
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
