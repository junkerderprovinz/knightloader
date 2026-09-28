// The QR scanner stays free software.
//
// expo-camera links Google's ML Kit and Play services for its barcode scanner
// unless the plugin is told otherwise, and either keeps the app out of
// F-Droid. src/components/QRScanner.tsx decodes photographed frames with jsQR
// instead, so the plugin must keep the scanner off and no source may bring
// back onBarcodeScanned, which needs it. Expo ships expo-camera precompiled,
// with ML Kit in its published dependencies, so the switch only takes effect
// when package.json has the module built from source.
//
// Run by hand and by CI, from mobile/: `node check-free-scanner.mjs`
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const app = JSON.parse(readFileSync(join(here, 'app.json'), 'utf8'));
const problems = [];

const camera = (app.expo.plugins ?? []).find((p) => (Array.isArray(p) ? p[0] : p) === 'expo-camera');
if (!Array.isArray(camera) || camera[1]?.barcodeScannerEnabled !== false) {
  problems.push('app.json: the expo-camera plugin needs "barcodeScannerEnabled": false');
}

const pkg = JSON.parse(readFileSync(join(here, 'package.json'), 'utf8'));
if (!(pkg.expo?.autolinking?.android?.buildFromSource ?? []).includes('expo-camera')) {
  problems.push('package.json: expo.autolinking.android.buildFromSource needs "expo-camera"');
}

function walk(dir) {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) walk(path);
    else if (/\.(ts|tsx)$/.test(name) && /onBarcodeScanned|barcodeScannerSettings/.test(readFileSync(path, 'utf8'))) {
      problems.push(`${relative(here, path)}: uses expo-camera's barcode scanner, which is ML Kit`);
    }
  }
}
walk(join(here, 'src'));

if (problems.length) {
  console.error('The app would ship Google ML Kit:\n' + problems.map((p) => '  ' + p).join('\n'));
  process.exit(1);
}
