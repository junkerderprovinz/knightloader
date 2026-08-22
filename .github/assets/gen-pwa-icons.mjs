// Composes the web app's PWA/browser-tab icon set from the same knight-helm
// logo, replacing the generic download-arrow-in-a-circle placeholder in
// web/public/icons/ (referenced by web/index.html's <link rel="icon"> /
// apple-touch-icon and by manifest.webmanifest's icons[] array).
//
// icon-192 / icon-512 (purpose "any"): transparent ground, no OS-side
// cropping guarantee, so the logo is shown near its natural shape.
//
// icon-512-maskable (purpose "maskable"): a full-bleed #161616 tile (the
// app's own GlimStone --carbon-bg / theme_color) with NO transparency - the
// OS masks this into a circle/squircle/rounded-square at its own discretion,
// so content must stay inside the maskable "safe zone" (content within an
// ~80%-diameter centred circle; a 66%-inscribed-square margin is the more
// conservative, commonly-used target) rather than reaching the tile edges.
// Run: node .github/assets/gen-pwa-icons.mjs
import { readFileSync, writeFileSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { createRequire } from "node:module";
import { execSync } from "node:child_process";

const require = createRequire(import.meta.url);
const groot = execSync("npm root -g").toString().trim();
const { Resvg } = require(`${groot}/@resvg/resvg-js`);

const __dir = dirname(fileURLToPath(import.meta.url));
const REPO = join(__dir, "../..");
const LOGO_RAW = readFileSync(join(REPO, ".github/assets/logo.svg"), "utf8");
const vbMatch = LOGO_RAW.match(/viewBox="[\d.\-]+\s+[\d.\-]+\s+([\d.]+)\s+([\d.]+)"/);
if (!vbMatch) throw new Error("logo.svg: no viewBox found");
const VB_W = parseFloat(vbMatch[1]), VB_H = parseFloat(vbMatch[2]);
const LOGO = LOGO_RAW.replace(/<\?xml[^>]*\?>\s*/, "");

function tile(size, pad, bg) {
  const avail = size * (1 - pad * 2);
  const scale = Math.min(avail / VB_W, avail / VB_H);
  const w = VB_W * scale, h = VB_H * scale;
  const x = (size - w) / 2, y = (size - h) / 2;
  const embedded = LOGO.replace(
    /<svg\b[^>]*>/,
    `<svg x="${x.toFixed(2)}" y="${y.toFixed(2)}" width="${w.toFixed(2)}" height="${h.toFixed(2)}" viewBox="0 0 ${VB_W} ${VB_H}" xmlns="http://www.w3.org/2000/svg">`,
  );
  const rect = bg ? `<rect width="${size}" height="${size}" fill="${bg}"/>` : "";
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}" viewBox="0 0 ${size} ${size}">${rect}${embedded}</svg>`;
}

function render(svg, size, bg, outFile) {
  const png = new Resvg(svg, { background: bg, fitTo: { mode: "width", value: size } }).render().asPng();
  writeFileSync(join(REPO, "web/public/icons", outFile), png);
  console.log(`wrote ${outFile} (${size}x${size})`);
}

const OUT = join(REPO, "web/public/icons");
render(tile(192, 0.1, null), 192, "rgba(0,0,0,0)", "icon-192.png");
render(tile(512, 0.1, null), 512, "rgba(0,0,0,0)", "icon-512.png");
render(tile(512, 0.17, "#161616"), 512, "#161616", "icon-512-maskable.png");
