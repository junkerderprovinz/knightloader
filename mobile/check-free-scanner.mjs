// The app stays free of Google's ML Kit and Play services.
//
// modules/qr-scanner reads QR codes with ZXing. expo-camera must not come back
// for the camera: with its barcode scanner switched off it still compiles
// against ML Kit, and F-Droid rejects an APK whose code merely names those
// classes. The release build checks the finished APK as well; this check
// catches the dependency before anything is built.
//
// Run by hand and by CI, from mobile/: `node check-free-scanner.mjs`
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const lock = JSON.parse(readFileSync(join(here, 'package-lock.json'), 'utf8'));
const banned = /(^|\/)(expo-camera|expo-barcode-scanner|[^/]*ml-?kit[^/]*)$/i;

const problems = Object.keys(lock.packages ?? {})
  .map((path) => path.replace(/^.*node_modules\//, ''))
  .filter((name) => banned.test(name));

if (problems.length) {
  console.error('The app would link Google ML Kit through:\n' + [...new Set(problems)].map((p) => '  ' + p).join('\n'));
  process.exit(1);
}
