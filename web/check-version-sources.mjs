// The two version numbers on the Browser & App page come from where the
// downloads beside them come from, never from a number typed into the page.
//
// The browser tiles download the copy of extension/src the server embeds, so
// the extension's number is read at runtime from that same embedded
// manifest.json (GET /api/browser-extension/version, whose Go test compares it
// with the manifest inside the served zip). The APK tile downloads the app
// release that mobile/app.json names when the page is built, so the card's
// number and the file's address are both made from the one constant
// vite.config.ts reads out of app.json.
//
// A typed number drifts without a sound: nothing fails when app.json or the
// manifest moves on, and the card keeps promising a version the tile does not
// give.
//
// Checks:
//   define    vite.config.ts reads expo.version from ../mobile/app.json into
//             __MOBILE_VERSION__.
//   page      BrowserTools.tsx holds no version literal outside its logos; the
//             app's number and the APK address are made from the constant
//             that reads __MOBILE_VERSION__; the extension's number is the
//             one fetchExtensionVersion answers, and that asks the route that
//             reads the embedded manifest.
//   release   the APK address names the tag and the file release-mobile.yml
//             publishes, and that workflow refuses a tag app.json disagrees
//             with.
//   words     no translation of a settings.browsertools string carries a
//             version.
//
// Not checked: whether the release for the current app.json exists yet, since
// app.json is raised before its tag is pushed, and web/dist, which CI rebuilds
// and compares with the sources.
//
// Run: `node web/check-version-sources.mjs`.
import { readdirSync, readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const web = dirname(fileURLToPath(import.meta.url));
const root = join(web, '..');
const read = (...parts) => readFileSync(join(...parts), 'utf8');

const VERSION = /(?<![\w.])v?\d+\.\d+\.\d+(?![\w.])/;
const problems = [];
const fail = (message) => {
  console.error(`check-version-sources: ${message}`);
  process.exit(1);
};

/** Comments blanked one character for one, so offsets and line numbers stay put. */
function blankComments(text) {
  const out = text.split('');
  let i = 0;
  let quote = null;
  while (i < text.length) {
    const c = text[i];
    if (quote) {
      if (c === '\\') { i += 2; continue; }
      if (c === quote) quote = null;
      i += 1;
      continue;
    }
    if (c === '"' || c === "'" || c === '`') { quote = c; i += 1; continue; }
    if (c === '/' && text[i + 1] === '/') {
      while (i < text.length && text[i] !== '\n') { out[i] = ' '; i += 1; }
      continue;
    }
    if (c === '/' && text[i + 1] === '*') {
      const close = text.indexOf('*/', i + 2);
      const stop = close === -1 ? text.length : close + 2;
      for (let k = i; k < stop; k += 1) if (out[k] !== '\n') out[k] = ' ';
      i = stop;
      continue;
    }
    i += 1;
  }
  return out.join('');
}

const lineOf = (text, at) => text.slice(0, at).split('\n').length;

// define
const vite = blankComments(read(web, 'vite.config.ts'));
const reader = /const\s+(\w+)\s*=\s*\(?\s*JSON\.parse\(\s*readFileSync\(\s*new URL\(\s*'\.\.\/mobile\/app\.json'[\s\S]*?\.expo\.version\s*;/.exec(vite);
if (!reader) {
  problems.push('vite.config.ts: no constant reads expo.version out of ../mobile/app.json');
} else if (!new RegExp(`__MOBILE_VERSION__\\s*:\\s*JSON\\.stringify\\(\\s*${reader[1]}\\s*\\)`).test(vite)) {
  problems.push(`vite.config.ts: __MOBILE_VERSION__ is not defined as JSON.stringify(${reader[1]}), the value read from app.json`);
}
const appVersion = JSON.parse(read(root, 'mobile', 'app.json')).expo?.version;
if (!appVersion || !VERSION.test(appVersion)) fail(`mobile/app.json has no expo.version to read (found ${JSON.stringify(appVersion)}).`);

// page
const pagePath = join(web, 'src', 'pages', 'settings', 'BrowserTools.tsx');
const page = blankComments(read(pagePath));
if (!/const \w+_SVG =/.test(page)) fail('BrowserTools.tsx declares no *_SVG logos - the page moved or the reader went blind.');
// The logos' path data is full of dotted number runs such as 4.1.9.
const code = page.replace(/(const \w+_SVG =\s*)'[^']*'/g, (whole, head) => head + "''".padEnd(whole.length - head.length, ' '));
const literal = VERSION.exec(code);
if (literal) {
  problems.push(`BrowserTools.tsx:${lineOf(code, literal.index)}: "${literal[0]}" is a version typed into the page`);
}

const appConst = /const\s+(\w+)\s*=\s*__MOBILE_VERSION__\s*;/.exec(code)?.[1];
if (!appConst) {
  problems.push('BrowserTools.tsx: no constant takes __MOBILE_VERSION__, so the app card has no number from app.json');
} else {
  if (!new RegExp(`<ReleaseVersion\\s+version=\\{${appConst}\\}\\s+tagPrefix="mobile/v"`).test(code)) {
    problems.push(`BrowserTools.tsx: the app card's number is not <ReleaseVersion version={${appConst}} tagPrefix="mobile/v" />`);
  }
}

const extState = /const \[(\w+), (\w+)\] = useState<string \| null>\(null\);/.exec(code);
const extFetch = extState && new RegExp(`fetchExtensionVersion\\(\\)\\s*\\.then\\(\\((\\w+)\\)\\s*=>\\s*${extState[2]}\\(\\1\\.version\\)\\)`).test(code);
if (!extFetch) {
  problems.push('BrowserTools.tsx: the extension card\'s number is not the version fetchExtensionVersion answers');
} else if (!new RegExp(`<ReleaseVersion\\s+version=\\{${extState[1]}\\}\\s+tagPrefix="extension/v"`).test(code)) {
  problems.push(`BrowserTools.tsx: the extension card's number is not <ReleaseVersion version={${extState[1]}} tagPrefix="extension/v" />`);
}

const api = read(web, 'src', 'lib', 'api.ts');
const fetcher = /export async function fetchExtensionVersion\(\)[\s\S]*?\n\}/.exec(api)?.[0] ?? '';
if (!/fetch\('\/api\/browser-extension\/version'\)/.test(fetcher)) {
  problems.push("lib/api.ts: fetchExtensionVersion does not ask GET /api/browser-extension/version");
}
const route = read(root, 'internal', 'api', 'routes_browsertools.go');
const handler = /func extensionVersion\([\s\S]*?\n\}/.exec(route)?.[0] ?? '';
if (!/fs\.ReadFile\(extension\.Dist, "src\/manifest\.json"\)/.test(handler)) {
  problems.push('internal/api/routes_browsertools.go: extensionVersion does not read src/manifest.json out of extension.Dist');
}

// release
const workflow = read(root, '.github', 'workflows', 'release-mobile.yml');
if (!/require\('\.\/app\.json'\)\.expo\.version/.test(workflow)) {
  problems.push('release-mobile.yml: the tag is no longer checked against app.json, so a tag no longer names the app.json version');
}
const tagPrefix = /v="\$\{GITHUB_REF_NAME#([^}]+)\}"\s*\n\s*out="([^"$]*)\$v([^"]*)"/.exec(workflow);
if (!tagPrefix) fail('release-mobile.yml: the step that names the APK after its version was not found.');
const [, prefix, fileHead, fileTail] = tagPrefix;
const apk = /apk:\s*`([^`]*)`/.exec(code)?.[1];
if (!apk) {
  problems.push('BrowserTools.tsx: APP_URLS has no apk address');
} else if (appConst) {
  const want = `/releases/download/${prefix}\${${appConst}}/${fileHead}\${${appConst}}${fileTail}`;
  if (!apk.endsWith(want)) {
    problems.push(`BrowserTools.tsx: the APK address ends in "${apk.replace(/^\$\{\w+\}/, '')}", but release-mobile.yml publishes "${want}"`);
  }
}

// words
const localeDir = join(web, 'src', 'lib', 'locales');
const locales = readdirSync(localeDir).filter((f) => /^[a-z]{2}\.ts$/.test(f));
if (locales.length < 2) fail(`only ${locales.length} locale files found - wrong directory?`);
for (const file of locales) {
  const text = read(localeDir, file);
  for (const m of text.matchAll(/^\s*'(settings\.browsertools\.[\w.]+)':\s*('(?:[^'\\]|\\.)*')/gm)) {
    const found = VERSION.exec(m[2]);
    if (found) problems.push(`locales/${file}: ${m[1]} carries "${found[0]}", a version the card already shows`);
  }
}

if (problems.length) {
  console.error(`check-version-sources: ${problems.length} problem(s).`);
  for (const p of problems.sort()) console.error(`  ${p}`);
  process.exit(1);
}
console.log(
  `ok: the app card shows and downloads app.json's ${appVersion} through one constant, the extension card shows the embedded manifest's number, and ${locales.length} locales carry no version.`,
);
