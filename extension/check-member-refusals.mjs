// Every refusal an instance can answer this browser with when it will not take
// its key, or when a member may not make the call, has a sentence in every
// catalogue, so the badge never falls back to "The instance refused it" for
// something the browser could fix.
//
// The codes are read from the server (internal/federation/members.go,
// internal/relay/member.go and internal/api/group_routes.go), so a code added
// there fails this check until it is mapped in src/group.js or listed below as
// one no browser can meet.
//
// Run by CI and by hand: `node extension/check-member-refusals.mjs`.
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const read = (...p) => readFileSync(join(here, ...p), 'utf8');

// Codes the browser cannot meet, and why.
const UNREACHABLE = {
  // The browser names its member id in every call it signs.
  memberUnnamed: 'every signed call names its member',
  // The browser signs as an extension under the "browser-" id it mints.
  memberKind: 'the member id has the shape its kind mints',
  // The browser's signature is held to the server's vector by check-relay-seal.mjs.
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

const group = read('src', 'group.js');
const map = group.match(/const REFUSAL_KEYS = \{([\s\S]*?)\n\};/);
if (!map) problems.push('REFUSAL_KEYS was not found in src/group.js');
const mapped = Object.fromEntries([...(map?.[1] ?? '').matchAll(/(\w+): '([^']+)'/g)].map((m) => [m[1], m[2]]));

const src = read('src', 'i18n.js');
const start = src.indexOf('const MESSAGES');
const MESSAGES = new Function(`${src.slice(start, src.indexOf('\n};', start) + 3)}\nreturn MESSAGES;`)();

for (const code of codes) {
  if (code in UNREACHABLE) continue;
  const key = mapped[code];
  if (!key) {
    problems.push(`${code} is a refusal the server sends and src/group.js does not word`);
    continue;
  }
  for (const [lang, messages] of Object.entries(MESSAGES)) {
    if (typeof messages[key] !== 'string' || messages[key] === '') problems.push(`${lang}: ${key} is missing`);
  }
}
for (const code of Object.keys(mapped)) {
  if (!codes.has(code)) problems.push(`${code} is worded in src/group.js but the server never sends it`);
}
for (const code of Object.keys(UNREACHABLE)) {
  if (!codes.has(code)) problems.push(`${code} is listed as unreachable but the server never sends it`);
}

// An instance that was reset answers the old words with a plain 410 "reset"
// (internal/api/retired_group.go), which says to pair again rather than
// "refused".
const fnStart = group.indexOf('function refusalKey(res)');
const refusalKey = new Function(
  `${map?.[0] ?? ''}\n${group.slice(fnStart, group.indexOf('\n}\n', fnStart) + 2)}\nreturn refusalKey;`,
)();
const reset = refusalKey({ status: 410, body: 'reset' });
if (reset !== 'refusal.groupReset') problems.push(`an instance that was reset reads as ${reset}, want refusal.groupReset`);
for (const [lang, messages] of Object.entries(MESSAGES)) {
  if (typeof messages['refusal.groupReset'] !== 'string' || messages['refusal.groupReset'] === '') problems.push(`${lang}: refusal.groupReset is missing`);
}

if (problems.length) {
  console.error('Member refusals:\n' + problems.map((p) => '  ' + p).join('\n'));
  process.exit(1);
}
console.log(`Member refusals: ${codes.size - Object.keys(UNREACHABLE).length} codes worded in ${Object.keys(MESSAGES).length} languages.`);
