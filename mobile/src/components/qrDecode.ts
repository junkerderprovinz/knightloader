import jpeg from 'jpeg-js';
import jsQR from 'jsqr';
import { base64Decode } from '../api/base64';

/** Reads the QR code in a base64 JPEG photo, or returns null when it holds none. */
export function qrFromJpeg(base64: string): string | null {
  const image = jpeg.decode(Uint8Array.from(base64Decode(base64)), { useTArray: true, formatAsRGBA: true });
  const pixels = new Uint8ClampedArray(image.data.buffer, image.data.byteOffset, image.data.byteLength);
  // Both polarities, since a dark theme can draw the code light on dark.
  const code = jsQR(pixels, image.width, image.height, { inversionAttempts: 'attemptBoth' });
  return code ? code.data : null;
}
