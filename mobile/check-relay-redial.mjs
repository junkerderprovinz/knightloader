// A relay call that times out redials, and the call after it is answered.
//
// The phone sends no pings, so a socket the relay or the network dropped
// without a close stays open on this side: every call through it waits out its
// timeout, and the downloads screen says "connecting" until the app is started
// again. src/api/relayClient.ts runs here on a stand-in WebSocket and a clock
// this check moves by hand.
//
// Run by hand and by CI, from mobile/: `node check-relay-redial.mjs`
import { randomBytes } from 'node:crypto';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { compile } from './stand-in-react.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const problems = [];
const expect = (what, got, want) => {
  const g = JSON.stringify(got);
  const w = JSON.stringify(want);
  if (g !== w) problems.push(`${what}: got ${g}, want ${w}`);
};

let now = 0;
const timers = new Map();
let nextTimer = 0;
const realSetTimeout = globalThis.setTimeout;
globalThis.setTimeout = (f, ms = 0) => {
  timers.set(++nextTimer, { at: now + ms, f });
  return nextTimer;
};
globalThis.clearTimeout = (id) => timers.delete(id);
Date.now = () => now;
const flush = () => new Promise((r) => setImmediate(r));
const advance = async (ms) => {
  const end = now + ms;
  for (;;) {
    const due = [...timers].filter(([, t]) => t.at <= end).sort((a, b) => a[1].at - b[1].at)[0];
    if (!due) break;
    timers.delete(due[0]);
    now = due[1].at;
    due[1].f();
    await flush();
  }
  now = end;
  await flush();
};

const sockets = [];
globalThis.WebSocket = class {
  constructor(url) {
    this.url = url;
    this.sent = [];
    this.closed = false;
    sockets.push(this);
  }
  send(data) {
    this.sent.push(JSON.parse(data));
  }
  close() {
    this.closed = true;
  }
};

const b64 = (bytes) => Buffer.from(bytes).toString('base64');
const { RelayClient } = compile(join(here, 'src', 'api', 'relayClient.ts'), {
  './base64': {
    base64Decode: (s) => [...Buffer.from(s, 'base64')],
    base64Encode: b64,
    bytesToUtf8: (b) => Buffer.from(b).toString('utf8'),
    encodeBody: (s) => Buffer.from(s).toString('base64'),
  },
  'expo-crypto': { getRandomBytes: (n) => new Uint8Array(randomBytes(n)) },
  './memberKey': { memberPublicKey: () => new Uint8Array(32), proveId: () => new Uint8Array(64), verifyResult: () => true },
  './relayFrame': {
    sealCall: () => b64([1]),
    sealSignedCall: () => b64([1]),
    sealIdentity: () => b64([2]),
    sealResult: () => b64([3]),
    openCall: () => null,
    openIdentity: () => null,
    openResult: (_key, _id, sealed) => JSON.parse(Buffer.from(sealed, 'base64').toString('utf8')),
  },
});

const client = new RelayClient({
  url: 'wss://relay.example/relay/connect',
  key: 'k'.repeat(32),
  frameKey: new Uint8Array(32),
  selfId: 'phone-1',
  memberSeed: null,
  selfName: 'Phone',
});
const joinRelay = async (socket) => {
  socket.onopen();
  socket.onmessage({ data: JSON.stringify({ type: 'challenge', data: { nonce: b64(new Uint8Array(32)) } }) });
  await flush();
};
const answer = (socket, status, body) => {
  const req = socket.sent.findLast((f) => f.type === 'proxy-request');
  const sealed = b64(Buffer.from(JSON.stringify({ status, body: b64(Buffer.from(body)) })));
  socket.onmessage({ data: JSON.stringify({ type: 'proxy-response', data: { requestId: req.data.requestId, sealed } }) });
};

client.start();
await joinRelay(sockets[0]);
expect('the client is connected', client.isConnected(), true);

let failed = null;
client.proxy('instance-1', 'GET', '/api/tasks').catch((e) => (failed = e.message));
await flush();
await advance(15_000);
expect('a call nobody answers fails', typeof failed, 'string');
expect('and the socket it went out on is closed', sockets[0].closed, true);
expect('and no longer counts as open', client.isConnected(), false);

await advance(1_000);
expect('the client dials again', sockets.length, 2);
if (sockets.length < 2) {
  console.error('A relay call that timed out leaves the client on a dead socket:\n' + problems.map((p) => '  ' + p).join('\n'));
  process.exit(1);
}
await joinRelay(sockets[1]);
sockets[0].onclose?.();
await advance(60_000);
expect('the dead socket closing late dials nothing more', sockets.length, 2);

const later = client.proxy('instance-1', 'GET', '/api/tasks');
await flush();
answer(sockets[1], 200, '[]');
const result = await later;
expect('the next call is answered', [result.status, result.body], [200, '[]']);

client.close();
globalThis.setTimeout = realSetTimeout;

if (problems.length) {
  console.error('A relay call that timed out leaves the client on a dead socket:\n' + problems.map((p) => '  ' + p).join('\n'));
  process.exit(1);
}
console.log('Relay redial: a call that times out closes its socket, the client dials again and the next call is answered.');
