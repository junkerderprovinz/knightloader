// Generates every mobile app-icon asset from ONE source of truth,
// .github/assets/kl_app_logo.svg - a dedicated 1000x1000 square variant of
// the repo logo, drawn for an app-icon slot (gen-appicon.mjs's desktop tile
// uses the tall banner-shaped logo.svg instead).
//
// Run: node .github/assets/gen-mobile-icon.mjs
//
// Three outputs, because the platforms want genuinely different shapes:
//
//   icon.png / favicon.png - the source verbatim, full-bleed, its own
//   white/grey backdrop included. Correct for iOS (Apple's guidance is a
//   full opaque square; the OS rounds the corners itself) and for a browser
//   tab, neither of which composites anything underneath.
//
//   android-icon-background.png + android-icon-foreground.png - Android's
//   adaptive icon, which is TWO layers the system composites and then masks
//   with a shape that varies by launcher (circle on stock/Pixel, squircle on
//   Samsung, rounded square, teardrop...).
//
// Two things about that Android pair have each already been a real, shipped
// bug, so both are spelled out here:
//
// 1. THE FOREGROUND NEEDS ITS OWN MARGIN. Both layers are 108x108dp but only
//    the central 72x72dp survives every mask - the outer 18dp per side is
//    reserved for masking and the launcher's parallax/zoom. An earlier
//    export of this file wrote the logo full-bleed (alpha channel confirmed
//    99.6% of the canvas fully opaque), so every mask except the loosest one
//    clipped its edges. Hence SAFE_FRACTION below.
//
// 2. THE BACKGROUND IS A LAYER, NOT A COLOR. The fix for (1) stripped the
//    source's own backdrop out of the foreground - correct, since a backdrop
//    baked into the foreground would render as a square-within-a-square -
//    but paired it with a flat dark adaptiveIcon.backgroundColor. That threw
//    the source's white/grey ground away and shipped a dark icon that no
//    longer matched the artwork anywhere else. The backdrop belongs in the
//    OTHER layer: this script renders it as a real backgroundImage, so the
//    composited result is the source artwork again, with the launcher's mask
//    supplying the rounding the source drew for itself.
//
// SAFE_FRACTION is 72/108 exactly, and that exactness is the point rather
// than a taste call: shrinking the full source canvas by 72/108 makes the
// guaranteed-visible 72dp window show precisely what the 1000x1000 source
// shows edge to edge. The glyph measures 56.0% x 89.8% of the source canvas,
// so it lands at 89.8% of the visible height - the artwork's own
// proportions, not a re-composition of them.
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

// The source draws its backdrop as two shapes: a rounded rect covering the
// whole canvas, and a second rounded panel over its right half (echoing the
// blade's own light/dark split). Both are located, and their colours and the
// split position READ OUT of the source rather than restated here, so a
// palette or layout change in the SVG carries into the Android background
// layer instead of silently drifting from it.
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
// "M932,0h-432v..." starts at the panel's right edge and draws left, so its
// left edge - the split - is 932-432 = 500 of the 1000-wide viewBox.
const SPLIT = (parseFloat(panel[2]) - parseFloat(panel[3])) / VB_W;

// The glyph alone, for the foreground layer: the same two backdrop shapes
// removed, matched by the patterns already validated above.
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

// The background layer is deliberately NOT the source's rounded rect: it has
// to bleed past the mask on every side, and its own rounding would fight the
// launcher's. Square, edge to edge, split where the source splits it - the
// mask alone decides the silhouette.
write(
  "android-icon-background.png",
  `<svg xmlns="http://www.w3.org/2000/svg" width="${SIZE}" height="${SIZE}" viewBox="0 0 ${SIZE} ${SIZE}">
  <rect width="${SIZE}" height="${SIZE}" fill="${GROUND}"/>
  <rect x="${(SIZE * SPLIT).toFixed(1)}" width="${(SIZE * (1 - SPLIT)).toFixed(1)}" height="${SIZE}" fill="${PANEL}"/>
</svg>
`,
);

console.log(`ground=${GROUND} panel=${PANEL} split=${(SPLIT * 100).toFixed(1)}% safe=${(SAFE_FRACTION * 100).toFixed(1)}%`);
