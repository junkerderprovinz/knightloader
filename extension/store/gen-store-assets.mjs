/**
 * Generates the store icons for the extension:
 *
 *   store-icon-128.png  Chrome Web Store icon: 96x96 artwork inside 16px of
 *                       transparent padding, the size the store asks for
 *   logo-300.png        Edge Add-ons logo, 1:1
 *
 * Why not reuse src/icons/icon128.png: that one is a toolbar icon on an opaque
 * two-tone tile that runs to the edge. A store renders its own card around the
 * icon and asks for transparent padding instead.
 *
 * The promo tiles come from gen-screenshots.mjs, since they show the same
 * window as the screenshots.
 *
 * Deps (global): @resvg/resvg-js.
 * Run: node extension/store/gen-store-assets.mjs
 */
import { readFileSync, writeFileSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { createRequire } from "node:module";
import { execSync } from "node:child_process";

const require = createRequire(import.meta.url);
const groot = execSync("npm root -g").toString().trim();
const { Resvg } = require(`${groot}/@resvg/resvg-js`);

const __dir = dirname(fileURLToPath(import.meta.url));
const LOGO = join(__dir, "..", "src", "logo.svg");

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

function emit(file, w, h, body) {
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${w}" height="${h}" viewBox="0 0 ${w} ${h}">${body}</svg>`;
  writeFileSync(join(__dir, file), new Resvg(svg, { fitTo: { mode: "width", value: w } }).render().asPng());
  console.log(`wrote ${file} (${w}x${h})`);
}

emit("store-icon-128.png", 128, 128, logo(16, 16, 96, 96));
emit("logo-300.png", 300, 300, logo(22, 22, 256, 256));
