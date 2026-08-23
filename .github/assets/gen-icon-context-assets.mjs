// Renders the small-icon-context logo (jdp's own kl_app_logo.svg - a
// pre-composed square tile, unlike the bare knight-helm logo.svg the other
// gen-*.mjs scripts wrap themselves) for the three surfaces jdp named this
// is for and ONLY this is for: the bookmarklet button, the browser
// extension's own icon, and the favicon/PWA icon set. No tile()/background
// composition needed here - the source SVG already carries its own
// rounded-square background, so every size is a direct resvg render.
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
const SRC = readFileSync(join(REPO, ".github/assets/kl_app_logo.svg"), "utf8");

function renderPng(size) {
  return new Resvg(SRC, { fitTo: { mode: "width", value: size } }).render().asPng();
}

// Browser extension's own icon (toolbar + chrome://extensions list) -
// replaces the generic yellow download-arrow-in-a-circle placeholder.
const EXT_ICONS = join(REPO, "extension/src/icons");
for (const size of [16, 32, 48, 128]) {
  writeFileSync(join(EXT_ICONS, `icon${size}.png`), renderPng(size));
}

// Favicon/PWA icon set - web/index.html's <link rel="icon">/apple-touch-icon
// and manifest.webmanifest's icons[]. icon-512-maskable reuses the same
// render: the source already IS a full-bleed tile with its own background,
// so there is no separate maskable-safe-zone composition to do here (unlike
// gen-pwa-icons.mjs's tile() for the bare logo.svg).
const WEB_ICONS = join(REPO, "web/public/icons");
for (const size of [16, 32, 48, 192, 512]) {
  writeFileSync(join(WEB_ICONS, `icon-${size}.png`), renderPng(size));
}
writeFileSync(join(WEB_ICONS, "icon-512-maskable.png"), renderPng(512));

// favicon.ico (16/32/48) - Pillow packs it, same approach gen-tray.mjs uses
// for its own .ico, since Node has no ICO encoder among the global packages.
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
const tmp = mkdtempSync(join(tmpdir(), "kl-favicon-"));
const icoSizes = [48, 32, 16];
const pngPaths = icoSizes.map((s) => {
  const p = join(tmp, `${s}.png`);
  writeFileSync(p, renderPng(s));
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

console.log("wrote extension icons (16/32/48/128), web icons (16/32/48/192/512/512-maskable), favicon.ico");
