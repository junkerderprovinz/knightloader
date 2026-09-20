// Composes the systray icon from the logo on a transparent ground, since the
// taskbar or menu bar behind it can be light or dark.
//
// desktop/assets/tray.png is the 64x64 PNG used on Linux and macOS;
// desktop/assets/tray.ico is a multi-size Windows icon, packed by Pillow
// because Node has no ICO encoder.
// Run: node .github/assets/gen-tray.mjs
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
const LOGO_RAW = readFileSync(join(REPO, ".github/assets/logo.svg"), "utf8");
const vbMatch = LOGO_RAW.match(/viewBox="[\d.\-]+\s+[\d.\-]+\s+([\d.]+)\s+([\d.]+)"/);
if (!vbMatch) throw new Error("logo.svg: no viewBox found");
const VB_W = parseFloat(vbMatch[1]), VB_H = parseFloat(vbMatch[2]);
const LOGO = LOGO_RAW.replace(/<\?xml[^>]*\?>\s*/, "");

// Every frame needs a square canvas: rendering the portrait logo directly
// gives 10x16 at size 16, which breaks the ICO packer.
const PAD = 0.08;
function squareSvg(size) {
  const avail = size * (1 - PAD * 2);
  const scale = Math.min(avail / VB_W, avail / VB_H);
  const w = VB_W * scale, h = VB_H * scale;
  const x = (size - w) / 2, y = (size - h) / 2;
  const embedded = LOGO.replace(
    /<svg\b[^>]*>/,
    `<svg x="${x.toFixed(2)}" y="${y.toFixed(2)}" width="${w.toFixed(2)}" height="${h.toFixed(2)}" viewBox="0 0 ${VB_W} ${VB_H}" xmlns="http://www.w3.org/2000/svg">`,
  );
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}" viewBox="0 0 ${size} ${size}">${embedded}</svg>`;
}

function renderPng(size) {
  return new Resvg(squareSvg(size), { fitTo: { mode: "width", value: size } }).render().asPng();
}

// Largest first: Pillow's ICO writer drops every size larger than the base
// image passed to save().
const ICO_SIZES = [256, 48, 32, 24, 16];
const tmp = mkdtempSync(join(tmpdir(), "kl-tray-"));
const pngPaths = ICO_SIZES.map((s) => {
  const p = join(tmp, `${s}.png`);
  writeFileSync(p, renderPng(s));
  return p;
});

const trayPng = renderPng(64);
writeFileSync(join(REPO, "desktop/assets/tray.png"), trayPng);

// A temp .py file rather than python -c, because execSync goes through cmd.exe
// on Windows and its quoting would mangle the Python string.
const icoOut = join(REPO, "desktop/assets/tray.ico");
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
console.log("wrote desktop/assets/tray.png (64x64) + tray.ico (16/24/32/48/256)");
