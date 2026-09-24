// Every sentence a module row sends has words in this interface.
//
// /api/features sends each row's reason and live detail twice: as an English
// sentence for curl and the diagnostics bundle, and as a code with values that
// pages/settings/tx.ts turns into the reader's language. A code without a key
// falls back to the English sentence, so a German page shows English and
// nothing fails. The Go side has a test that every sentence carries a code;
// this is the other half, that every code has a key.
//
// Codes are read from the Go source: `code: "..."` in a line literal, and the
// code argument of countDetail, which also needs the key with "One" appended.
// A key under the two prefixes that no code produces is reported too, so a
// sentence the server stopped sending does not linger in the locale files.
//
// Run: `node web/check-module-lines.mjs`
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const read = (...p) => readFileSync(join(here, ...p), 'utf8');

const goFiles = ['routes_features.go', 'routes_health.go'].map((f) => read('..', 'internal', 'api', f));

const codes = new Set();
for (const src of goFiles) {
  for (const m of src.matchAll(/\bcode:\s*"(\w+)"/g)) codes.add(m[1]);
  for (const m of src.matchAll(/\bcountDetail\([^,]+,\s*"(\w+)"/g)) {
    codes.add(m[1]);
    codes.add(m[1] + 'One');
  }
}

const problems = [];
if (codes.size === 0) problems.push('no module line codes found in internal/api; the patterns above do not match the Go source');

const PREFIXES = ['settings.modules.detail.', 'settings.modules.reason.'];

for (const locale of ['en', 'de']) {
  const text = read('src', 'lib', 'locales', `${locale}.ts`);
  const keys = new Set([...text.matchAll(/^\s*'(settings\.modules\.(?:detail|reason)\.\w+)':/gm)].map((m) => m[1]));
  for (const code of codes) {
    if (!PREFIXES.some((p) => keys.has(p + code))) {
      problems.push(`${locale}.ts has no settings.modules.detail.${code} or settings.modules.reason.${code}`);
    }
  }
  for (const key of keys) {
    const code = key.slice(key.lastIndexOf('.') + 1);
    if (!codes.has(code)) problems.push(`${locale}.ts has ${key}, which no module row sends`);
  }
}

if (problems.length) {
  console.error('Module lines without words:\n' + problems.map((p) => '  ' + p).join('\n'));
  process.exit(1);
}
console.log(`Module lines: ${codes.size} codes, each with an English and a German key.`);
