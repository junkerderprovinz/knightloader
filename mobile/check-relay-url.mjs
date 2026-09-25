// The app dials a relay address the way the server does.
//
// src/api/relayClient.ts ports internal/relay's connectURL. When the two
// disagree, an address that works on the instances fails on the phone with
// nothing but "not connected". So the port is run against every row of the Go
// test's table, TestConnectURL in internal/relay/client_test.go. A row wanting
// "" is an address the server refuses, which the app answers with null.
//
// Run by hand and by CI, from mobile/: `node check-relay-url.mjs`
import { readFileSync } from 'node:fs';
import { stripTypeScriptTypes } from 'node:module';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const source = readFileSync(join(here, 'src', 'api', 'relayClient.ts'), 'utf8');
const goTest = readFileSync(join(here, '..', 'internal', 'relay', 'client_test.go'), 'utf8');

const fn = source.match(/^export function connectURL\([\s\S]*?^\}$/m);
const table = goTest.match(/^func TestConnectURL\([\s\S]*?^\}$/m);
if (!fn || !table) {
  console.error('connectURL or TestConnectURL was not found; the patterns in this check no longer match the sources');
  process.exit(1);
}
const connectURL = new Function(`${stripTypeScriptTypes(fn[0].replace(/^export /, ''))}\nreturn connectURL;`)();

const rows = [...table[0].matchAll(/\{"([^"]*)", "([^"]*)", "([^"]*)"\}/g)];
const problems = [];
for (const [, name, input, want] of rows) {
  const got = connectURL(input);
  if (got !== (want === '' ? null : want)) {
    problems.push(`${name}: connectURL(${JSON.stringify(input)}) = ${JSON.stringify(got)}, the server dials ${JSON.stringify(want || null)}`);
  }
}
if (rows.length === 0) problems.push('TestConnectURL has no rows this check can read');

if (problems.length) {
  console.error('The app and the server dial different relay addresses:\n' + problems.map((p) => '  ' + p).join('\n'));
  process.exit(1);
}
console.log(`Relay addresses: ${rows.length} cases dialled as the server dials them.`);
