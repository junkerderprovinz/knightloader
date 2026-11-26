// The extension dials the relay its instances use. An empty field is the
// project relay; an address is taken the way the instances take it, so the one
// entered under Settings, Pairing on an instance works here as it is.
//
//   relayDialURL  ports kit/relay's connectURL and is run against every row of
//                 TestConnectURL in internal/federation/wire_test.go. A row the
//                 server dials over TLS is dialled the same; any other is
//                 refused, as the phone refuses cleartext.
//   group.js      stores the address, refuses one it cannot dial, keeps it when
//                 the browser leaves the group, and withGroup dials it.
//
// Run by CI and by hand: `node extension/check-relay-address.mjs`
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';

const here = dirname(fileURLToPath(import.meta.url));
const read = (...p) => readFileSync(join(here, ...p), 'utf8');
const problems = [];
const expect = (what, got, want) => {
  const g = JSON.stringify(got);
  const w = JSON.stringify(want);
  if (g !== w) problems.push(`${what}: got ${g}, want ${w}`);
};

const stored = {};
const ctx = vm.createContext({
  chrome: {
    storage: {
      local: {
        async get(keys) {
          const out = {};
          for (const k of [keys].flat()) if (k in stored) out[k] = stored[k];
          return out;
        },
        async set(items) {
          Object.assign(stored, items);
        },
        async remove(keys) {
          for (const k of [keys].flat()) delete stored[k];
        },
      },
    },
  },
  crypto: globalThis.crypto,
  TextEncoder,
  TextDecoder,
});
for (const file of ['wordlist.js', 'phrase.js', 'relay.js', 'group.js']) vm.runInContext(read('src', file), ctx, { filename: file });

const relayDialURL = ctx.relayDialURL;
if (typeof relayDialURL !== 'function') {
  console.error('relayDialURL was not found in src/relay.js');
  process.exit(1);
}

const goTest = read('..', 'internal', 'federation', 'wire_test.go');
const table = goTest.match(/^func TestConnectURL\([\s\S]*?^\}$/m);
const rows = table ? [...table[0].matchAll(/\{"([^"]*)", "([^"]*)", "([^"]*)"\}/g)] : [];
if (rows.length === 0) problems.push('TestConnectURL has no rows this check can read');
for (const [, name, input, want] of rows) {
  expect(`${name} (${JSON.stringify(input)})`, relayDialURL(input), want.startsWith('wss://') ? want : null);
}
for (const [what, input, want] of [
  ['a wss address without a path', 'wss://relay.example.com', 'wss://relay.example.com/relay/connect'],
  ['the scheme in capitals', 'WSS://relay.example.com', 'wss://relay.example.com/relay/connect'],
  ['an instance with a port', 'https://kl.example.org:8443', 'wss://kl.example.org:8443/relay/connect'],
  ['a plain ws address', 'ws://relay.example.com/relay/connect', null],
  ['a plain http address', 'http://192.168.20.30:8760', null],
  ['a name and password in the address', 'wss://user:pass@relay.example.com', null],
  ['a query', 'wss://relay.example.com/relay/connect?x=1', null],
  ['a space inside', 'wss://relay example.com', null],
]) {
  expect(what, relayDialURL(input), want);
}

// withGroup runs for real; the relay session and the keys are stand-ins.
let dialled = null;
vm.runInContext(
  `readGroupKeys = async () => ({ key: 'k', frameKey: new Uint8Array(32), phrase: 'p', movedKey: '' });
   readMember = async () => null;
   relaySession = async (opts, work) => { globalThis.dialledURL = opts.url; return work({ siblings: [], call: async () => null }); };
   followMove = async () => {};`,
  ctx,
);
const dial = async () => {
  await ctx.withGroup(async () => null);
  dialled = ctx.dialledURL;
  return dialled;
};
const projectRelay = vm.runInContext('DEFAULT_RELAY_URL', ctx);

expect('with nothing stored the project relay is dialled', await dial(), projectRelay);
expect('an address with a path is stored', await ctx.writeRelayAddress('  https://kl.example.org/kl  '), true);
expect('the address is kept as entered, trimmed', await ctx.readRelayAddress(), 'https://kl.example.org/kl');
expect('the stored address is dialled', await dial(), 'wss://kl.example.org/kl/relay/connect');
expect('a cleartext address is refused', await ctx.writeRelayAddress('ws://kl.example.org'), false);
expect('a refused address leaves the stored one alone', await ctx.readRelayAddress(), 'https://kl.example.org/kl');
await ctx.forgetGroup();
expect('leaving the group keeps the relay', await ctx.readRelayAddress(), 'https://kl.example.org/kl');
expect('an emptied field goes back to the project relay', await ctx.writeRelayAddress(''), true);
expect('nothing is stored for the project relay', 'relayAddress' in stored, false);
expect('and the project relay is dialled again', await dial(), projectRelay);

if (problems.length) {
  console.error('Relay address:\n' + problems.map((p) => '  ' + p).join('\n'));
  process.exit(1);
}
console.log(`Relay address: ${rows.length} server rows and the stored address dialled as the instances do.`);
