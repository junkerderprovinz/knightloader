// Generates the mobile app icons from .github/assets/kl_app_logo.svg, the
// square variant of the logo.
//
// Run: node .github/assets/gen-mobile-icon.mjs
//
// icon.png and favicon.png are the source full-bleed with its backdrop, which
// suits iOS and a browser tab. Android's adaptive icon is two layers that the
// launcher composites and masks: the foreground holds only the glyph, and the
// background layer carries the source's backdrop, so the result matches the
// artwork elsewhere.
//
// Only the central 72dp of the 108dp layers survives every mask. Scaling the
// whole source canvas by 72/108 makes that window show exactly what the source
// shows edge to edge.
const SAFE_FRACTION = 72 / 108;

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
const LOGO_RAW = readFileSync(join(REPO, ".github/assets/kl_app_logo.svg"), "utf8");
const LOGO_FULL = LOGO_RAW.replace(/<\?xml[^>]*\?>\s*/, "");
const vbMatch = LOGO_RAW.match(/viewBox="[\d.\-]+\s+[\d.\-]+\s+([\d.]+)\s+([\d.]+)"/);
if (!vbMatch) throw new Error("kl_app_logo.svg: no viewBox found");
const VB_W = parseFloat(vbMatch[1]), VB_H = parseFloat(vbMatch[2]);

// The backdrop is a full rounded rect plus a panel over the right half. Their
// colours and the split are read from the source so the Android background
// follows any change to the SVG.
const bgRect = LOGO_FULL.match(/<rect id="background" class="(cls-\d+)"[^/]*\/>/);
if (!bgRect) throw new Error("kl_app_logo.svg: no <rect id=\"background\"> found");
const panel = LOGO_FULL.match(/<path class="(cls-\d+)" d="M(\d+(?:\.\d+)?),0h-(\d+(?:\.\d+)?)v/);
if (!panel) throw new Error("kl_app_logo.svg: right-hand backdrop panel not found");

function fillOf(cls) {
  const m = LOGO_FULL.match(new RegExp(`\\.${cls}\\s*\\{[^}]*fill:\\s*([^;\\s}]+)`));
  if (!m) throw new Error(`kl_app_logo.svg: no fill for .${cls}`);
  return m[1];
}
const GROUND = fillOf(bgRect[1]);
const PANEL = fillOf(panel[1]);
// "M932,0h-432v..." starts at the panel's right edge and draws left, so the
// split is at 932 - 432 = 500 of the 1000 wide viewBox.
const SPLIT = (parseFloat(panel[2]) - parseFloat(panel[3])) / VB_W;

// The foreground layer: the source without the two backdrop shapes.
const LOGO_GLYPH_ONLY = LOGO_FULL
  .replace(bgRect[0], "")
  .replace(panel[0].replace(/v$/, ""), "")
  .replace(/^\s*[\r\n]/gm, "");
if (LOGO_GLYPH_ONLY === LOGO_FULL) {
  throw new Error("kl_app_logo.svg: backdrop strip did not change anything");
}

const SIZE = 1024;

function embedAt(logo, x, y, w, h) {
  return logo.replace(
    /<svg\b[^>]*>/,
    `<svg x="${x.toFixed(1)}" y="${y.toFixed(1)}" width="${w.toFixed(1)}" height="${h.toFixed(1)}" viewBox="0 0 ${VB_W} ${VB_H}" xmlns="http://www.w3.org/2000/svg">`,
  );
}

function write(name, svg) {
  const png = new Resvg(svg, { fitTo: { mode: "width", value: SIZE } }).render().asPng();
  writeFileSync(join(REPO, "mobile/assets", name), png);
  console.log(`wrote mobile/assets/${name} (${png.length} bytes)`);
}

/** fraction 1 = the logo fills the canvas edge to edge. */
function renderLogo(name, logo, fraction) {
  const avail = SIZE * fraction;
  const scale = Math.min(avail / VB_W, avail / VB_H);
  const w = VB_W * scale, h = VB_H * scale;
  write(
    name,
    `<svg xmlns="http://www.w3.org/2000/svg" width="${SIZE}" height="${SIZE}" viewBox="0 0 ${SIZE} ${SIZE}">
  ${embedAt(logo, (SIZE - w) / 2, (SIZE - h) / 2, w, h)}
</svg>
`,
  );
}

renderLogo("icon.png", LOGO_FULL, 1);
renderLogo("favicon.png", LOGO_FULL, 1);
renderLogo("android-icon-foreground.png", LOGO_GLYPH_ONLY, SAFE_FRACTION);

// The background is square and edge to edge rather than the source's rounded
// rect, so the launcher's mask alone decides the silhouette.
write(
  "android-icon-background.png",
  `<svg xmlns="http://www.w3.org/2000/svg" width="${SIZE}" height="${SIZE}" viewBox="0 0 ${SIZE} ${SIZE}">
  <rect width="${SIZE}" height="${SIZE}" fill="${GROUND}"/>
  <rect x="${(SIZE * SPLIT).toFixed(1)}" width="${(SIZE * (1 - SPLIT)).toFixed(1)}" height="${SIZE}" fill="${PANEL}"/>
</svg>
`,
);

console.log(`ground=${GROUND} panel=${PANEL} split=${(SPLIT * 100).toFixed(1)}% safe=${(SAFE_FRACTION * 100).toFixed(1)}%`);
