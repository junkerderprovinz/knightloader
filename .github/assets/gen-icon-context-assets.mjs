// Renders every small-icon-context asset from the two master logos: the
// bookmarklet button and the browser-tab/bookmark-bar favicon from the bare
// knight-helm logo.svg, the browser extension's own icon and the PWA
// install icon (manifest.webmanifest's icon-192/512/512-maskable) from the
// pre-composed square kl_app_logo.svg.
//
// Split (2026-08-23/24, two rounds of jdp feedback):
// - "Lesezeichen und Browsererweiterung sollen das normale Logo haben. Das
//   Quadratische Logo nehmen wir dann nur für die App." -> extension icon
//   moved to logo.svg, kl_app_logo.svg reserved for icon-192/512/512-maskable.
// - "Für die browsererweiterung nehmen wir das app logo." -> reversed again,
//   extension icon is back on kl_app_logo.svg. Bookmarklet/favicon stay on
//   logo.svg either way - only the extension's own icon moved.
//
// This script is now the single source for every size derived from either
// master - gen-pwa-icons.mjs (logo.svg-based tile() renders of 192/512/
// 512-maskable) is superseded and removed; icon-512/512-maskable have been
// identical-size kl_app_logo.svg renders since that split, which
// gen-pwa-icons.mjs's own tile() padding could never reproduce.
// Run: node .github/assets/gen-icon-context-assets.mjs
import { readFileSync, writeFileSync, mkdtempSync, rmSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { tmpdir } from "node:os";
import { createRequire } from "node:module";
import { execSync } from "node:child_process";

const require = createRequire(import.meta.url);
const groot = execSync("npm root -g").toString().trim();
const { Resvg } = require(`${groot}/@resvg/resvg-js`);

const __dir = dirname(fileURLToPath(import.meta.url));
const REPO = join(__dir, "../..");

// ---- bare logo.svg: needs a transparent, padded tile() - it has no
// background of its own. ----
const LOGO_RAW = readFileSync(join(REPO, ".github/assets/logo.svg"), "utf8");
const logoVb = LOGO_RAW.match(/viewBox="[\d.\-]+\s+[\d.\-]+\s+([\d.]+)\s+([\d.]+)"/);
if (!logoVb) throw new Error("logo.svg: no viewBox found");
const LOGO_VB_W = parseFloat(logoVb[1]), LOGO_VB_H = parseFloat(logoVb[2]);
const LOGO = LOGO_RAW.replace(/<\?xml[^>]*\?>\s*/, "");

function logoTile(size, pad) {
  const avail = size * (1 - pad * 2);
  const scale = Math.min(avail / LOGO_VB_W, avail / LOGO_VB_H);
  const w = LOGO_VB_W * scale, h = LOGO_VB_H * scale;
  const x = (size - w) / 2, y = (size - h) / 2;
  const embedded = LOGO.replace(
    /<svg\b[^>]*>/,
    `<svg x="${x.toFixed(2)}" y="${y.toFixed(2)}" width="${w.toFixed(2)}" height="${h.toFixed(2)}" viewBox="0 0 ${LOGO_VB_W} ${LOGO_VB_H}" xmlns="http://www.w3.org/2000/svg">`,
  );
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}" viewBox="0 0 ${size} ${size}">${embedded}</svg>`;
}

function renderLogo(size, pad) {
  return new Resvg(logoTile(size, pad), { background: "rgba(0,0,0,0)", fitTo: { mode: "width", value: size } })
    .render()
    .asPng();
}

// ---- kl_app_logo.svg: already a full-bleed square tile with its own
// rounded-corner background - a direct render at each size, no tile()
// wrapping needed. ----
const APP_SRC = readFileSync(join(REPO, ".github/assets/kl_app_logo.svg"), "utf8");

function renderApp(size) {
  return new Resvg(APP_SRC, { fitTo: { mode: "width", value: size } }).render().asPng();
}

// Browser extension's own icon (toolbar + chrome://extensions list) - the
// square app logo.
const EXT_ICONS = join(REPO, "extension/src/icons");
for (const size of [16, 32, 48, 128]) {
  writeFileSync(join(EXT_ICONS, `icon${size}.png`), renderApp(size));
}

// Browser-tab / bookmark-bar favicon set (web/index.html's <link rel="icon">
// and the bookmarklet button's own <img>) - the normal logo.
const WEB_ICONS = join(REPO, "web/public/icons");
for (const size of [16, 32, 48]) {
  writeFileSync(join(WEB_ICONS, `icon-${size}.png`), renderLogo(size, 0.06));
}

// manifest.webmanifest's PWA install icon - the square app logo. Maskable
// reuses the plain 512 render: the source is already a full-bleed tile with
// its own safe-zone padding baked in, there is no separate maskable
// composition to do here.
for (const size of [192, 512]) {
  writeFileSync(join(WEB_ICONS, `icon-${size}.png`), renderApp(size));
}
writeFileSync(join(WEB_ICONS, "icon-512-maskable.png"), renderApp(512));

// favicon.ico (16/32/48, from the normal logo) - Pillow packs it, same
// approach gen-tray.mjs uses for its own .ico, since Node has no ICO
// encoder among the global packages.
const tmp = mkdtempSync(join(tmpdir(), "kl-favicon-"));
const icoSizes = [48, 32, 16];
const pngPaths = icoSizes.map((s) => {
  const p = join(tmp, `${s}.png`);
  writeFileSync(p, renderLogo(s, 0.06));
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

console.log("wrote extension icons (16/32/48/128, app logo)");
console.log("wrote web favicon set (16/32/48/favicon.ico, normal logo)");
console.log("wrote web PWA icons (192/512/512-maskable, app logo)");
