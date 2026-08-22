// Composes the systray icon from the same knight-helm logo, replacing the
// placeholder assets.go itself flagged: "Generated from a small standalone
// script using the app's own accent/contrast colors ... a placeholder until
// the project's own logo rollout replaces it." This is that rollout.
//
// Renders on a transparent ground (no background tile) - the tray icon sits
// directly on the OS taskbar/menu bar, which can be light or dark, so it must
// composite rather than carry its own fill like the appicon does.
//
// desktop/assets/tray.png stays the single 64x64 PNG SetIcon expects on
// unix/darwin; desktop/assets/tray.ico is a real multi-size Windows icon
// (16/24/32/48/256, matching the sizes already embedded in the file this
// replaces) - Node has no ICO encoder among this machine's global packages,
// so the individual PNG renders are packed into the .ico via Pillow (already
// installed), shelled out the same way gen-banner.mjs shells out to npm root -g.
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

// Every icon frame must be a genuinely SQUARE canvas - fitting the resvg
// output directly to the logo's own portrait viewBox (as gen-banner.mjs does
// for the wide banner) instead yields a non-square render (e.g. 10x16 at
// size=16), which silently breaks the ICO packer below (a bug caught by
// visually verifying the render - see the logo-rollout skill's own rule).
// So: build a square transparent SVG per size, centre-scale the logo inside
// it with a small margin, same shape as gen-appicon.mjs's tile math minus the
// background rect.
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

// Largest-first: Pillow's ICO writer checks every candidate size against the
// BASE image's own (width, height) as an upper bound and silently drops any
// size larger than it - the base image passed to .save() must be the biggest
// frame, with the rest as append_images, or every size above the smallest
// gets dropped (caught by inspecting the packed .ico's own directory header).
const ICO_SIZES = [256, 48, 32, 24, 16];
const tmp = mkdtempSync(join(tmpdir(), "kl-tray-"));
const pngPaths = ICO_SIZES.map((s) => {
  const p = join(tmp, `${s}.png`);
  writeFileSync(p, renderPng(s));
  return p;
});

// desktop/assets/tray.png: the single-size PNG used on Linux/macOS.
const trayPng = renderPng(64);
writeFileSync(join(REPO, "desktop/assets/tray.png"), trayPng);

// desktop/assets/tray.ico: pack the per-size PNGs Pillow already has on disk.
// Written to a temp .py file rather than passed as a -c string - node's
// execSync shells out through cmd.exe on Windows, whose quoting would mangle
// the embedded double-quoted Python string.
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
