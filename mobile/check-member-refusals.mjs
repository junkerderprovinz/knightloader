// Every refusal an instance can answer this phone with when it will not take
// its key, or when a member may not make the call, has a sentence in every
// catalogue, so the app never shows the server's English for something the
// person could fix.
//
// The codes are read from the server (internal/federation/members.go,
// internal/relay/member.go and internal/api/group_routes.go), so a code added
// there fails this check until it is mapped in src/api/client.ts or listed
// below as one no phone can meet.
//
// Run by CI and by hand, from mobile/: `node check-member-refusals.mjs`
import { readFileSync, readdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const read = (...p) => readFileSync(join(here, ...p), 'utf8');

// Codes the phone cannot meet, and why.
const UNREACHABLE = {
  // The app names its relay id in every call it signs.
  memberUnnamed: 'every signed call names its member',
  // The app signs as a phone under the "phone-" id it mints.
  memberKind: 'the member id has the shape its kind mints',
  // The app's signature is held to the server's vector by check-member-key.mjs.
  memberSignature: 'the signature is checked against the server vector',
};

const server =
  read('..', 'internal', 'federation', 'members.go') +
  read('..', 'internal', 'relay', 'member.go') +
  read('..', 'internal', 'api', 'group_routes.go');
const codes = new Set(
  [...server.matchAll(/(?:refuse\([^,]+,|RefusalBody\(|writeRefusal\([^,]+,[^,]+,)\s*"([A-Za-z]+)"/g)].map((m) => m[1]),
);
const problems = [];
if (codes.size === 0) problems.push('no refusal codes found in the server; the pattern in this check no longer matches it');

const client = read('src', 'api', 'client.ts');
const map = client.match(/const REFUSALS[^=]*= \{([\s\S]*?)\n\};/);
if (!map) problems.push('REFUSALS was not found in src/api/client.ts');
const mapped = Object.fromEntries([...(map?.[1] ?? '').matchAll(/(\w+): '([^']+)'/g)].map((m) => [m[1], m[2]]));

const dir = join(here, 'src', 'i18n');
const catalogues = readdirSync(dir).filter((f) => /^[a-z]{2}\.ts$/.test(f));

for (const code of codes) {
  if (code in UNREACHABLE) continue;
  const key = mapped[code];
  if (!key) {
    problems.push(`${code} is a refusal the server sends and src/api/client.ts does not word`);
    continue;
  }
  for (const f of catalogues) {
    if (!new RegExp(`\\n  '${key.replace('.', '\\.')}': ['"][^\\n]+,`).test(readFileSync(join(dir, f), 'utf8'))) {
      problems.push(`${f}: ${key} is missing`);
    }
  }
}
for (const code of Object.keys(UNREACHABLE)) {
  if (!codes.has(code)) problems.push(`${code} is listed as unreachable but the server never sends it`);
}

if (problems.length) {
  console.error('Member refusals:\n' + problems.map((p) => '  ' + p).join('\n'));
  process.exit(1);
}
console.log(`Member refusals: ${codes.size - Object.keys(UNREACHABLE).length} codes worded in ${catalogues.length} languages.`);
