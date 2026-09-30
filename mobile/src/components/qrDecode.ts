import jpeg from 'jpeg-js';
import jsQR from 'jsqr';
import { base64Decode } from '../api/base64';

/** How much of the photo's shorter side the search looks at, around the
 *  middle where the scanner draws its frame. */
const CROP = 0.7;

/**
 * Reads the QR code in the middle of a base64 JPEG photo, or returns null when
 * it holds none there.
 */
export function qrFromJpeg(base64: string): string | null {
  const image = jpeg.decode(Uint8Array.from(base64Decode(base64)), { useTArray: true, formatAsRGBA: true });
  const { pixels, side } = middleSquare(image.data, image.width, image.height);
  // Both polarities, since a dark theme can draw the code light on dark.
  const code = jsQR(pixels, side, side, { inversionAttempts: 'attemptBoth' });
  if (code) return code.data;
  const bright = brightStretched(pixels);
  return bright ? (jsQR(bright, side, side, { inversionAttempts: 'dontInvert' })?.data ?? null) : null;
}

/** middleSquare copies the square around the photo's centre, so the search
 *  spends nothing on the room around the code. */
function middleSquare(data: Uint8Array, width: number, height: number): { pixels: Uint8ClampedArray; side: number } {
  const side = Math.floor(Math.min(width, height) * CROP);
  const left = Math.floor((width - side) / 2);
  const top = Math.floor((height - side) / 2);
  const pixels = new Uint8ClampedArray(side * side * 4);
  for (let y = 0; y < side; y++) {
    const from = ((top + y) * width + left) * 4;
    pixels.set(data.subarray(from, from + side * 4), y * side * 4);
  }
  return { pixels, side };
}

/**
 * brightStretched spreads the bright part of the square over the whole range.
 * A monitor photographed at the room's exposure comes out as pale grey on
 * white, a difference too small for the search, which takes a patch with less
 * than 24 levels between its darkest and brightest pixel for one colour. The
 * bright part is told from the room by Otsu's threshold, and the darkest and
 * brightest two percent of it are left out, so a reflection does not set the
 * range. Returns null for a square without two such parts.
 */
function brightStretched(pixels: Uint8ClampedArray): Uint8ClampedArray | null {
  const count = pixels.length / 4;
  const luma = new Uint8Array(count);
  const histogram = new Uint32Array(256);
  for (let i = 0; i < count; i++) {
    const p = i * 4;
    const l = (pixels[p] * 77 + pixels[p + 1] * 150 + pixels[p + 2] * 29) >> 8;
    luma[i] = l;
    histogram[l]++;
  }

  const threshold = otsu(histogram, count);
  let inBright = 0;
  for (let l = threshold; l < 256; l++) inBright += histogram[l];
  const cut = inBright * 0.02;
  let lo = threshold;
  for (let seen = 0; lo < 255 && seen + histogram[lo] <= cut; lo++) seen += histogram[lo];
  let hi = 255;
  for (let seen = 0; hi > lo && seen + histogram[hi] <= cut; hi--) seen += histogram[hi];
  if (hi - lo < 4) return null;

  const out = new Uint8ClampedArray(pixels.length);
  const scale = 255 / (hi - lo);
  for (let i = 0; i < count; i++) {
    const v = (luma[i] - lo) * scale;
    const p = i * 4;
    out[p] = out[p + 1] = out[p + 2] = v;
    out[p + 3] = 255;
  }
  return out;
}

/** otsu is the brightness that best splits the histogram into two groups. */
function otsu(histogram: Uint32Array, count: number): number {
  let total = 0;
  for (let l = 0; l < 256; l++) total += l * histogram[l];
  let below = 0;
  let belowSum = 0;
  let best = 0;
  let threshold = 0;
  for (let l = 0; l < 256; l++) {
    below += histogram[l];
    if (below === 0) continue;
    const above = count - below;
    if (above === 0) break;
    belowSum += l * histogram[l];
    const between = below * above * (belowSum / below - (total - belowSum) / above) ** 2;
    if (between > best) {
      best = between;
      threshold = l + 1;
    }
  }
  return threshold;
}
