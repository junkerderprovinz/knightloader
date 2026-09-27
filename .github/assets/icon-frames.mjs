// Square icon frames drawn from the logo, and the ICO file that holds them.
// Each frame is rendered from the SVG at its own size rather than scaled down
// from a large one, so the small ones stay sharp.
import { readFileSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { createRequire } from "node:module";
import { execSync } from "node:child_process";

const require = createRequire(import.meta.url);
const groot = execSync("npm root -g").toString().trim();
const { Resvg } = require(`${groot}/@resvg/resvg-js`);

const __dir = dirname(fileURLToPath(import.meta.url));
const LOGO_RAW = readFileSync(join(__dir, "logo.svg"), "utf8");
const LOGO = LOGO_RAW.replace(/<\?xml[^>]*\?>\s*/, "");
const vbMatch = LOGO_RAW.match(/viewBox="[\d.\-]+\s+[\d.\-]+\s+([\d.]+)\s+([\d.]+)"/);
if (!vbMatch) throw new Error("logo.svg: no viewBox found");
const VB_W = parseFloat(vbMatch[1]), VB_H = parseFloat(vbMatch[2]);

// The logo is taller than wide and runs to the edges of its viewBox, so a frame
// the full height of the canvas is as large as the mark can be drawn. The left
// edge sits on a whole pixel, which keeps the shield's outline crisp.
export function frame(size, margin = 0) {
  return render(size, margin).asPng();
}

function render(size, margin) {
  const h = size - margin * 2;
  const w = (VB_W / VB_H) * h;
  const x = Math.round((size - w) / 2);
  const embedded = LOGO.replace(
    /<svg\b[^>]*>/,
    `<svg x="${x}" y="${margin}" width="${w.toFixed(3)}" height="${h}" viewBox="0 0 ${VB_W} ${VB_H}" xmlns="http://www.w3.org/2000/svg">`,
  );
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}" viewBox="0 0 ${size} ${size}">${embedded}</svg>`;
  return new Resvg(svg, { fitTo: { mode: "width", value: size } }).render();
}

// ico packs a frame for each size into an ICO file. The 256 px frame is a PNG,
// as Windows expects there; the smaller ones are 32-bit bitmaps, which every
// reader of the format takes, where some misread a small PNG entry.
export function ico(sizes) {
  const frames = sizes.map((size) => ({ size, data: size >= 256 ? frame(size) : bitmap(size) }));
  const header = Buffer.alloc(6 + 16 * frames.length);
  header.writeUInt16LE(0, 0);
  header.writeUInt16LE(1, 2);
  header.writeUInt16LE(frames.length, 4);
  let offset = header.length;
  frames.forEach(({ size, data }, i) => {
    const at = 6 + 16 * i;
    // The format writes a side of 256 as zero.
    header.writeUInt8(size >= 256 ? 0 : size, at);
    header.writeUInt8(size >= 256 ? 0 : size, at + 1);
    header.writeUInt8(0, at + 2);
    header.writeUInt8(0, at + 3);
    header.writeUInt16LE(1, at + 4);
    header.writeUInt16LE(32, at + 6);
    header.writeUInt32LE(data.length, at + 8);
    header.writeUInt32LE(offset, at + 12);
    offset += data.length;
  });
  return Buffer.concat([header, ...frames.map((f) => f.data)]);
}

// bitmap is one frame in the ICO's own bitmap form: a BITMAPINFOHEADER that
// counts the colour rows and the mask rows together, the pixels bottom row
// first in BGRA, and a mask the alpha channel makes redundant.
function bitmap(size) {
  const { pixels } = render(size, 0);
  const maskRow = Math.ceil(size / 32) * 4;
  const out = Buffer.alloc(40 + size * size * 4 + maskRow * size);
  out.writeUInt32LE(40, 0);
  out.writeInt32LE(size, 4);
  out.writeInt32LE(size * 2, 8);
  out.writeUInt16LE(1, 12);
  out.writeUInt16LE(32, 14);
  out.writeUInt32LE(size * size * 4 + maskRow * size, 20);
  for (let y = 0; y < size; y++) {
    for (let x = 0; x < size; x++) {
      const from = (y * size + x) * 4;
      const to = 40 + ((size - 1 - y) * size + x) * 4;
      out[to] = pixels[from + 2];
      out[to + 1] = pixels[from + 1];
      out[to + 2] = pixels[from];
      out[to + 3] = pixels[from + 3];
    }
  }
  return out;
}
