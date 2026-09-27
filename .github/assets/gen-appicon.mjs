// Composes the app icons from the logo. The desktop app's icon is the logo on
// a transparent ground, because the taskbar, the Dock and the installer draw
// their own behind it. The container icon, knightloader-appicon.png, puts it on
// GlimStone's --carbon-bg in a square tile, left unrounded because Unraid
// applies its own mask. Wails builds the platform .ico and .icns from
// desktop/build/appicon.png.
// Run: node .github/assets/gen-appicon.mjs
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
const LOGO = LOGO_RAW.replace(/<\?xml[^>]*\?>\s*/, "");
const vbMatch = LOGO_RAW.match(/viewBox="[\d.\-]+\s+[\d.\-]+\s+([\d.]+)\s+([\d.]+)"/);
if (!vbMatch) throw new Error("logo.svg: no viewBox found");
const VB_W = parseFloat(vbMatch[1]), VB_H = parseFloat(vbMatch[2]);

const SIZE = 1024;
const BG = "#161616";
const PAD = 0.07; // fraction of SIZE reserved as margin on each side
const availW = SIZE * (1 - PAD * 2);
const availH = SIZE * (1 - PAD * 2);
const scale = Math.min(availW / VB_W, availH / VB_H);
const logoW = VB_W * scale, logoH = VB_H * scale;
const x = (SIZE - logoW) / 2, y = (SIZE - logoH) / 2;

const embedded = LOGO.replace(
  /<svg\b[^>]*>/,
  `<svg x="${x.toFixed(1)}" y="${y.toFixed(1)}" width="${logoW.toFixed(1)}" height="${logoH.toFixed(1)}" viewBox="0 0 ${VB_W} ${VB_H}" xmlns="http://www.w3.org/2000/svg">`,
);

const tile = `<svg xmlns="http://www.w3.org/2000/svg" width="${SIZE}" height="${SIZE}" viewBox="0 0 ${SIZE} ${SIZE}">
  <rect width="${SIZE}" height="${SIZE}" fill="${BG}"/>
  ${embedded}
</svg>
`;
const bare = `<svg xmlns="http://www.w3.org/2000/svg" width="${SIZE}" height="${SIZE}" viewBox="0 0 ${SIZE} ${SIZE}">
  ${embedded}
</svg>
`;

const render = (svg) => new Resvg(svg, { fitTo: { mode: "width", value: SIZE } }).render().asPng();

writeFileSync(join(REPO, ".github/assets/knightloader-appicon.svg"), tile);
writeFileSync(join(REPO, ".github/assets/knightloader-appicon.png"), render(tile));
const desktop = render(bare);
writeFileSync(join(REPO, "desktop/build/appicon.png"), desktop);
console.log("wrote desktop/build/appicon.png (" + desktop.length + " bytes) + knightloader-appicon.svg/.png");
