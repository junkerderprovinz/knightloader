/**
 * Generates the browser-store graphics for the extension:
 *
 *   store-icon-128.png         Chrome Web Store icon: 96x96 artwork inside 16px
 *                              of transparent padding, the size the store asks for
 *   logo-300.png               Edge Add-ons logo, 1:1
 *   promo-small-440x280.png    small promo tile (Chrome: required, Edge: optional)
 *   promo-marquee-1400x560.png marquee / large promo tile (optional in both)
 *
 * Why not reuse src/icons/icon128.png: that one is a toolbar icon on an opaque
 * two-tone tile that runs to the edge. A store renders its own card around the
 * icon and asks for transparent padding instead.
 *
 * Bree Serif for the name, Lato for the line under it, both fetched to the OS
 * temp dir and handed to resvg as font files, so rendering needs no installed
 * font. opentype.js only MEASURES the text, for the layout.
 *
 * The text is real SVG <text>, set by resvg, and NOT glyph outlines turned into
 * paths the way .github/assets/gen-banner.mjs does it. Outlines drew broken
 * letters here, depending on where a line landed: a caption rendered as one
 * path stopped 296 px in, and as one path per glyph it still drew the first
 * "n" of "Connect" as a stub. The same strings set as <text> come out whole.
 *
 * No browser is named anywhere in these images. Edge's policy 1.1.2 rejects a
 * listing that references another browser, and one set of images serves all
 * three stores.
 *
 * Deps (global): opentype.js, @resvg/resvg-js.
 * Run: node extension/store/gen-store-assets.mjs
 */
import { readFileSync, writeFileSync, existsSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { tmpdir } from "node:os";
import { createRequire } from "node:module";
import { execSync } from "node:child_process";

const require = createRequire(import.meta.url);
const groot = execSync("npm root -g").toString().trim();
const opentype = require(`${groot}/opentype.js`);
const { Resvg } = require(`${groot}/@resvg/resvg-js`);

const __dir = dirname(fileURLToPath(import.meta.url));
const LOGO = join(__dir, "..", "src", "logo.svg");
const NAME = "KnightLoader";
const LINE = "Send links to your own download manager";
const BG = "#0d1117", NAME_FILL = "#e6edf3", LINE_FILL = "#9aa4ad";

async function fontFile(file, url) {
  const path = join(tmpdir(), file);
  if (!existsSync(path)) {
    const res = await fetch(url);
    if (!res.ok) throw new Error(`${file}: font fetch ${res.status}`);
    writeFileSync(path, Buffer.from(await res.arrayBuffer()));
  }
  return path;
}
const nameFile = await fontFile("KnightLoader-BreeSerif-Regular.ttf", "https://github.com/google/fonts/raw/main/ofl/breeserif/BreeSerif-Regular.ttf");
const lineFile = await fontFile("KnightLoader-Lato-Regular.ttf", "https://github.com/google/fonts/raw/main/ofl/lato/Lato-Regular.ttf");
const nameFont = opentype.parse(readFileSync(nameFile));
const lineFont = opentype.parse(readFileSync(lineFile));
const FONTS = { fontFiles: [nameFile, lineFile], loadSystemFonts: false, defaultFontFamily: "Lato" };

const logoRaw = readFileSync(LOGO, "utf8").replace(/<\?xml[^>]*\?>\s*/, "");
const [, , , vbW, vbH] = logoRaw.match(/viewBox="([\d.\-]+)\s+([\d.\-]+)\s+([\d.]+)\s+([\d.]+)"/).map(Number);

/** The logo fitted into a w x h box at (x, y), centred, aspect kept. */
function logo(x, y, w, h) {
  const s = Math.min(w / vbW, h / vbH);
  const lw = vbW * s, lh = vbH * s;
  return logoRaw.replace(
    /<svg\b[^>]*>/,
    `<svg x="${(x + (w - lw) / 2).toFixed(2)}" y="${(y + (h - lh) / 2).toFixed(2)}" width="${lw.toFixed(2)}" height="${lh.toFixed(2)}" viewBox="0 0 ${vbW} ${vbH}" xmlns="http://www.w3.org/2000/svg">`,
  );
}

function emit(file, w, h, body, background) {
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${w}" height="${h}" viewBox="0 0 ${w} ${h}">${body}</svg>`;
  const opts = { fitTo: { mode: "width", value: w }, font: FONTS };
  if (background) opts.background = background;
  writeFileSync(join(__dir, file), new Resvg(svg, opts).render().asPng());
  console.log(`wrote ${file} (${w}x${h})`);
}

/** Logo on the left, name and line to its right, the group centred. */
function tile(file, w, h, logoH, nameSize, lineSize, gap) {
  const logoW = logoH * (vbW / vbH);
  const nameW = nameFont.getAdvanceWidth(NAME, nameSize);
  const lineW = lineFont.getAdvanceWidth(LINE, lineSize);
  const groupW = logoW + gap + Math.max(nameW, lineW);
  const x0 = (w - groupW) / 2;
  const textX = x0 + logoW + gap;
  const nameAsc = nameFont.ascender * (nameSize / nameFont.unitsPerEm);
  const nameDesc = -nameFont.descender * (nameSize / nameFont.unitsPerEm);
  const lineAsc = lineFont.ascender * (lineSize / lineFont.unitsPerEm);
  const lineGap = lineSize * 0.35;
  const blockH = nameAsc + nameDesc + lineGap + lineAsc;
  const nameBase = h / 2 - blockH / 2 + nameAsc;
  const lineBase = nameBase + nameDesc + lineGap + lineAsc;
  if (groupW > w * 0.94) throw new Error(`${file}: text does not fit (${groupW.toFixed(0)} of ${w})`);
  emit(file, w, h, `<rect width="${w}" height="${h}" fill="${BG}"/>
    ${logo(x0, (h - logoH) / 2, logoW, logoH)}
    <text x="${textX.toFixed(2)}" y="${nameBase.toFixed(2)}" font-family="Bree Serif" font-size="${nameSize}" fill="${NAME_FILL}">${NAME}</text>
    <text x="${textX.toFixed(2)}" y="${lineBase.toFixed(2)}" font-family="Lato" font-size="${lineSize}" fill="${LINE_FILL}">${LINE}</text>`, BG);
}

emit("store-icon-128.png", 128, 128, logo(16, 16, 96, 96));
emit("logo-300.png", 300, 300, logo(22, 22, 256, 256));
tile("promo-small-440x280.png", 440, 280, 168, 40, 13.5, 20);
tile("promo-marquee-1400x560.png", 1400, 560, 440, 128, 42, 70);
