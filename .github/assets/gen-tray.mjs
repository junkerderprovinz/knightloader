// Composes the systray icon from the logo on a transparent ground, since the
// taskbar or menu bar behind it can be light or dark.
//
// desktop/assets/tray.png is the 64x64 PNG used on Linux and macOS;
// desktop/assets/tray.ico holds a frame for each size Windows draws a
// notification icon at: 16 px at 100 % scaling, 20, 24, 32 and 40 up to 250 %.
// Run: node .github/assets/gen-tray.mjs
import { writeFileSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { frame, ico } from "./icon-frames.mjs";

const REPO = join(dirname(fileURLToPath(import.meta.url)), "../..");

const SIZES = [16, 20, 24, 32, 40, 48, 64];
writeFileSync(join(REPO, "desktop/assets/tray.ico"), ico(SIZES));
writeFileSync(join(REPO, "desktop/assets/tray.png"), frame(64));
console.log(`wrote desktop/assets/tray.png (64x64) + tray.ico (${SIZES.join("/")})`);
