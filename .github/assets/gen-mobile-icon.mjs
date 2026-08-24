// Composes the mobile app's Android adaptive-icon foreground layer and
// refreshes its plain icon.png/favicon.png, from the same square app-icon
// source gen-appicon.mjs's desktop tile uses a differently-shaped logo for
// (kl_app_logo.svg, a dedicated 1000x1000 square variant of logo.svg made
// for exactly this - an app icon slot, not the tall banner shape).
//
// The plain icon.png/favicon.png stay full-bleed, no padding added: that is
// correct for iOS (Apple's own guidance is a full square, no rounding, no
// transparency - the OS masks corners itself) and for a favicon, the same
// "solid, edge to edge" shape this project's own Unraid CA icon already
// uses on purpose (see ca-icon-background in Claude's memory).
//
// The Android adaptive-icon foreground is a SEPARATE shape with its own
// rule, not an iOS convention at all: the system composites this layer
// under its OWN mask (circle on stock/Pixel, squircle on Samsung, rounded
// square, teardrop...), varying by launcher, and only the inner ~66% "safe
// zone" of the 108x108dp canvas is guaranteed visible on every one of them.
// A full-bleed logo - which is what an EARLIER, un-padded export of this
// same source into mobile/assets/android-icon-foreground.png actually was,
// confirmed by inspecting its alpha channel: 99.6% of the canvas fully
// opaque, transparency only in a few corner-rounding pixels - gets its
// edges clipped by every mask shape except the least aggressive one. This
// script fixes that by scaling the logo down to fit SAFE_FRACTION of the
// canvas and centering it on a FULLY TRANSPARENT background (never a solid
// fill here - the background color layer is app.json's own
// adaptiveIcon.backgroundColor, a second, separate image the OS composites
// underneath this one, not something this file needs to paint).
//
// Run: node .github/assets/gen-mobile-icon.mjs
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

// The source draws its own square backdrop as two shapes - a white rounded
// rect the full canvas, and a light-grey rounded panel over its right half
// (echoing the blade's own light/dark split) - which is exactly right for
// icon.png/favicon.png (a full square WITH its own background is the
// correct, expected shape for iOS and a browser tab, both of which mask or
// round it themselves and never composite a second background layer under
// it). Android's adaptive icon is the one consumer that DOES composite a
// separate background layer of its own (app.json's adaptiveIcon.backgroundColor,
// #161616) underneath this foreground, so keeping the source's white/grey
// backdrop here would produce a solid off-white square-within-a-square
// instead of the dark app background showing through - stripped for that
// one render only.
const LOGO_GLYPH_ONLY = LOGO_FULL
  .replace(/<rect id="background"[^/]*\/>\s*/, "")
  .replace(/<path class="cls-5" d="M932,0h-432v1000h432c37\.56,0,68-30\.44,68-68V68c0-37\.56-30\.44-68-68-68Z"\/>\s*/, "");
if (LOGO_GLYPH_ONLY === LOGO_FULL) {
  throw new Error("kl_app_logo.svg: expected background shapes not found - update the strip patterns above");
}

const SIZE = 1024;

function embedAt(logo, x, y, w, h) {
  return logo.replace(
    /<svg\b[^>]*>/,
    `<svg x="${x.toFixed(1)}" y="${y.toFixed(1)}" width="${w.toFixed(1)}" height="${h.toFixed(1)}" viewBox="0 0 ${VB_W} ${VB_H}" xmlns="http://www.w3.org/2000/svg">`,
  );
}

function renderSquare(name, { pad, logo }) {
  const avail = SIZE * (1 - pad * 2);
  const scale = Math.min(avail / VB_W, avail / VB_H);
  const logoW = VB_W * scale, logoH = VB_H * scale;
  const x = (SIZE - logoW) / 2, y = (SIZE - logoH) / 2;
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${SIZE}" height="${SIZE}" viewBox="0 0 ${SIZE} ${SIZE}">
  ${embedAt(logo, x, y, logoW, logoH)}
</svg>
`;
  const png = new Resvg(svg, { fitTo: { mode: "width", value: SIZE } }).render().asPng();
  writeFileSync(join(REPO, "mobile/assets", name), png);
  console.log(`wrote mobile/assets/${name} (${png.length} bytes, ${((1 - pad * 2) * 100).toFixed(0)}% fill)`);
}

// icon.png / favicon.png: full-bleed, own backdrop included - iOS and a
// browser tab both mask/round it themselves.
renderSquare("icon.png", { pad: 0, logo: LOGO_FULL });
renderSquare("favicon.png", { pad: 0, logo: LOGO_FULL });

// android-icon-foreground.png: glyph only, on true transparency. Android's
// own adaptive-icon safe zone is the inner 66dp circle of a 108dp canvas
// (~61%); this leaves real margin beyond that minimum so the logo survives
// even a tight squircle mask without hugging the guaranteed-safe edge.
renderSquare("android-icon-foreground.png", { pad: 0.22, logo: LOGO_GLYPH_ONLY });
