// The app dials a relay address the way the server does, and names the relay
// in its hello the way the relay checks it.
//
// src/api/relayClient.ts ports internal/relay's connectURL and RelayHost. When
// connectURL disagrees, an address that works on the instances fails on the
// phone with nothing but "not connected"; when relayHost does, the relay never
// lets the phone take back its place after a dropped connection. So both ports
// are run against every row of the Go tests' tables, TestConnectURL and
// TestRelayHost in internal/relay/client_test.go. A connectURL row wanting ""
// is an address the server refuses, which the app answers with null.
//
// Run by hand and by CI, from mobile/: `node check-relay-url.mjs`
import { readFileSync } from 'node:fs';
import { stripTypeScriptTypes } from 'node:module';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const source = readFileSync(join(here, 'src', 'api', 'relayClient.ts'), 'utf8');
const goTest = readFileSync(join(here, '..', 'internal', 'relay', 'client_test.go'), 'utf8');

const port = (name) => {
  const fn = source.match(new RegExp(`^export function ${name}\\([\\s\\S]*?^\\}$`, 'm'));
  return fn && new Function(`${stripTypeScriptTypes(fn[0].replace(/^export /, ''))}\nreturn ${name};`)();
};
const rowsOf = (test) => {
  const table = goTest.match(new RegExp(`^func ${test}\\([\\s\\S]*?^\\}$`, 'm'));
  return table && [...table[0].matchAll(/\{"([^"]*)", "([^"]*)", "([^"]*)"\}/g)];
};
const connectURL = port('connectURL');
const relayHost = port('relayHost');
const connectRows = rowsOf('TestConnectURL');
const hostRows = rowsOf('TestRelayHost');
if (!connectURL || !relayHost || !connectRows || !hostRows) {
  console.error('connectURL, relayHost, TestConnectURL or TestRelayHost was not found; the patterns in this check no longer match the sources');
  process.exit(1);
}

const problems = [];
for (const [, name, input, want] of connectRows) {
  const got = connectURL(input);
  if (got !== (want === '' ? null : want)) {
    problems.push(`${name}: connectURL(${JSON.stringify(input)}) = ${JSON.stringify(got)}, the server dials ${JSON.stringify(want || null)}`);
  }
}
for (const [, name, input, want] of hostRows) {
  const got = relayHost(input);
  if (got !== want) problems.push(`${name}: relayHost(${JSON.stringify(input)}) = ${JSON.stringify(got)}, the relay checks for ${JSON.stringify(want)}`);
}
if (connectRows.length === 0) problems.push('TestConnectURL has no rows this check can read');
if (hostRows.length === 0) problems.push('TestRelayHost has no rows this check can read');

if (problems.length) {
  console.error('The app and the server dial or name relays differently:\n' + problems.map((p) => '  ' + p).join('\n'));
  process.exit(1);
}
console.log(`Relay addresses: ${connectRows.length} cases dialled and ${hostRows.length} named as the server does.`);
