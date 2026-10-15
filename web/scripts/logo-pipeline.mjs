// The steps the logo generators take every logo through, around
// src/lib/logoTile.ts: draw it at SIZE pixels, lift it off its plate and fit
// what is left to the size again, and read the colours and the mask its lit
// tile shows. Needs Node 22.18 or later, which runs the TypeScript module as it is.
import { createHash } from 'node:crypto';
import { logoTileOf, withoutPlate } from '../src/lib/logoTile.ts';

export const SIZE = 64;

const FIT = { fit: 'contain', background: { r: 0, g: 0, b: 0, alpha: 0 } };

async function pixels(image) {
  const { data, info } = await image.ensureAlpha().raw().toBuffer({ resolveWithObject: true });
  return { width: info.width, height: info.height, data: new Uint8ClampedArray(data) };
}

const fromPixels = (sharp, px) => sharp(Buffer.from(px.data), { raw: { width: px.width, height: px.height, channels: 4 } });

/** encode writes pixels as the small palette PNG the page loads. */
export const encode = (sharp, px) =>
  fromPixels(sharp, px).png({ palette: true, quality: 90, effort: 10, compressionLevel: 9 }).toBuffer();

/**
 * prepare reads a logo file or buffer into the PNG to ship, the mask for its
 * lit tile where it needs one of its own, and its entry for the generated
 * list. `disc` lets a round plate go as well as a square one. `image` tells
 * two files that draw the same picture.
 */
export async function prepare(sharp, input, { disc = false } = {}) {
  const meta = await sharp(input).metadata();
  // An SVG drawn at its own size is upscaled from a few pixels otherwise.
  const density = meta.format === 'svg' ? Math.max(72, (72 * SIZE * 2) / Math.min(meta.width, meta.height)) : undefined;
  const drawn = await pixels(sharp(input, { density }).resize(SIZE, SIZE, FIT));
  const lifted = withoutPlate(drawn, disc);
  const plain = lifted
    ? await pixels(sharp(await fromPixels(sharp, lifted).png().toBuffer()).trim({ threshold: 0 }).resize(SIZE, SIZE, FIT))
    : drawn;
  const { info, lit } = logoTileOf(drawn, plain);
  return {
    png: await encode(sharp, plain),
    lit: lit && (await encode(sharp, lit)),
    info,
    lifted: Boolean(lifted),
    image: createHash('sha256').update(drawn.data).digest('hex'),
  };
}

/** entry writes a logo's LogoTile as an object literal for a generated list. */
export function entry(info) {
  const fields = [`tile: '${info.tile}'`, `ink: '${info.ink}'`];
  if (info.rest) fields.push(`rest: '${info.rest}'`);
  if (info.lit) fields.push('lit: true');
  return `{ ${fields.join(', ')} }`;
}
