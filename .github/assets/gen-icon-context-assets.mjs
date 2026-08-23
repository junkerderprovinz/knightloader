// Renders the small-icon-context assets for the bookmarklet button, the
// browser extension's own icon, and the browser-tab/bookmark-bar favicon -
// all from the bare knight-helm logo.svg (transparent, padded tile(), same
// composition gen-pwa-icons.mjs already uses for icon-192/512), NOT from
// jdp's pre-composed kl_app_logo.svg.
//
// Revert (2026-08-23, jdp: "Ich nehm das mit dem neuen Icon zurück.
// Lesezeichen und Browsererweiterung sollen das normale Logo haben. Das
// Quadratische Logo nehmen wir dann nur für die App."): kl_app_logo.svg is
// reserved for "die App" ONLY - manifest.webmanifest's icon-192/icon-512/
// icon-512-maskable (the PWA "installed app" icon), which this script does
// NOT touch and gen-pwa-icons.mjs... no wait, gen-pwa-icons.mjs also renders
// logo.svg - see its own header comment for why icon-192/512/512-maskable
// still use kl_app_logo.svg: those three files are written directly here
// too (last block below), so gen-pwa-icons.mjs's logo.svg render is the one
// that is now dead/superseded for those three sizes - run this script, not
// that one, for the current split.
// Run: node .github/assets/gen-icon-context-assets.mjs
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

// Same tile() shape as gen-pwa-icons.mjs: transparent ground, logo centred
// with `pad` fractional margin on each side.
function tile(size, pad) {
  const avail = size * (1 - pad * 2);
  const scale = Math.min(avail / VB_W, avail / VB_H);
  const w = VB_W * scale, h = VB_H * scale;
  const x = (size - w) / 2, y = (size - h) / 2;
  const embedded = LOGO.replace(
    /<svg\b[^>]*>/,
    `<svg x="${x.toFixed(2)}" y="${y.toFixed(2)}" width="${w.toFixed(2)}" height="${h.toFixed(2)}" viewBox="0 0 ${VB_W} ${VB_H}" xmlns="http://www.w3.org/2000/svg">`,
  );
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}" viewBox="0 0 ${size} ${size}">${embedded}</svg>`;
}

function renderPng(size, pad) {
  return new Resvg(tile(size, pad), { background: "rgba(0,0,0,0)", fitTo: { mode: "width", value: size } })
    .render()
    .asPng();
}

// Browser extension's own icon (toolbar + chrome://extensions list) - the
// normal logo, not the square app tile.
const EXT_ICONS = join(REPO, "extension/src/icons");
for (const size of [16, 32, 48, 128]) {
  writeFileSync(join(EXT_ICONS, `icon${size}.png`), renderPng(size, 0.06));
}

// Browser-tab / bookmark-bar favicon set (web/index.html's <link rel="icon">
// and the bookmarklet button's own <img>) - also the normal logo. Distinct
// from manifest.webmanifest's icon-192/512/512-maskable, which stay on
// kl_app_logo.svg for "die App" (PWA install icon) and are untouched here.
const WEB_ICONS = join(REPO, "web/public/icons");
for (const size of [16, 32, 48]) {
  writeFileSync(join(WEB_ICONS, `icon-${size}.png`), renderPng(size, 0.06));
}

// favicon.ico (16/32/48) - Pillow packs it, same approach gen-tray.mjs uses
// for its own .ico, since Node has no ICO encoder among the global packages.
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
const tmp = mkdtempSync(join(tmpdir(), "kl-favicon-"));
const icoSizes = [48, 32, 16];
const pngPaths = icoSizes.map((s) => {
  const p = join(tmp, `${s}.png`);
  writeFileSync(p, renderPng(s, 0.06));
  return p;
});
const icoOut = join(REPO, "web/public/favicon.ico");
const pyScript = join(tmp, "pack_ico.py");
writeFileSync(
  pyScript,
  `from PIL import Image
imgs = [Image.open(p) for p in ${JSON.stringify(pngPaths)}]
imgs[0].save(${JSON.stringify(icoOut)}, format="ICO", sizes=[im.size for im in imgs], append_images=imgs[1:])
`,
);
execSync(`python3 "${pyScript}"`, { stdio: "inherit" });
rmSync(tmp, { recursive: true, force: true });

console.log("wrote extension icons (16/32/48/128) + web favicon set (16/32/48/favicon.ico) from logo.svg");
console.log("icon-192/512/512-maskable untouched - those stay on kl_app_logo.svg (the app/PWA icon)");
