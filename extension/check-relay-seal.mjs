// Checks the extension's port of the relay frame format against the fixed
// vector internal/relay's Go tests pin. The Go server, the phone
// (mobile/src/api/relayFrame.ts) and the extension have to agree byte for byte.
//
// The vector matches internal/relay/announce_seal_test.go. Its nonce is a fixed
// run of 0x07 so it is reproducible; nothing in production uses a fixed nonce.
// src/relay.js is loaded as is, so the shipped code is what gets checked. The
// last two checks hold a session to the order the relay sent its frames in and
// to taking a peer's identity only from a seal that opens.

import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';
import { createHash, webcrypto } from 'node:crypto';

const here = dirname(fileURLToPath(import.meta.url));
const src = readFileSync(join(here, 'src', 'relay.js'), 'utf8');

// The globals relay.js uses. The fake WebSocket opens at once and records what
// is written, so the last check reads the real hello frame.
const sent = [];
class FakeSocket {
  constructor() {
    this.readyState = 1;
    setTimeout(() => this.onopen?.(), 0);
  }
  send(raw) {
    sent.push(raw);
    FakeSocket.answer?.(this, JSON.parse(raw));
  }
  close() {
    this.onclose?.();
  }
}
const ctx = vm.createContext({
  crypto: webcrypto,
  TextEncoder,
  TextDecoder,
  btoa: (s) => Buffer.from(s, 'binary').toString('base64'),
  atob: (s) => Buffer.from(s, 'base64').toString('binary'),
  console,
  setTimeout,
  clearTimeout,
  WebSocket: FakeSocket,
});
vm.runInContext(src, ctx);

// relay.DeriveFrameKey, over the same secret the Go vector uses.
const frameKey = new Uint8Array(
  createHash('sha256')
    .update('knightloader/relay/frame-key/v1')
    .update(new TextEncoder().encode('cross-implementation vector'))
    .digest(),
);

const VECTOR =
  'BwcHBwcHBwcHBwcHKowzi1tX9hS/RbpFD36F1jz5pHjOvE8p9pXq7oULX/Xf0cCMQxbXtTdsUR7tCWHualBstwHbZzUY0vpg/urebU1me215Eg==';

const probe = vm.runInContext(
  `(async (key, vector) => {
     const opened = await relayOpen(key, relayAnnounceAAD('phone'), vector);
     const sealed = await relaySeal(key, relayAnnounceAAD('brave'),
       relayUtf8(JSON.stringify({ name: 'Chrome', deployment: 'extension', client: true })));
     const back = await relayOpen(key, relayAnnounceAAD('brave'), sealed);
     const moved = await relayOpen(key, relayAnnounceAAD('other'), sealed);
     return {
       opened: opened ? JSON.parse(relayFromUtf8(opened)) : null,
       roundTrip: back ? JSON.parse(relayFromUtf8(back)) : null,
       movedOpens: moved !== null,
     };
   })`,
  ctx,
);

const failures = [];
const r = await probe(frameKey, VECTOR);

// 1. It can open what the other two ports seal.
if (!r.opened) {
  failures.push('relayOpen could not open the shared vector at all');
} else if (r.opened.name !== 'Pixel 8' || r.opened.deployment !== 'mobile' || r.opened.client !== true) {
  failures.push(`the vector opened into ${JSON.stringify(r.opened)}, want the phone's own announce`);
}

// 2. What it seals opens again with every field intact. Without the client
//    flag the browser would be listed as a download target.
if (!r.roundTrip) {
  failures.push('an identity this port sealed could not be opened again');
} else if (
  r.roundTrip.name !== 'Chrome' ||
  r.roundTrip.deployment !== 'extension' ||
  r.roundTrip.client !== true
) {
  failures.push(`round trip produced ${JSON.stringify(r.roundTrip)}`);
}

// 3. The binding holds: a relay cannot reattach this announce to another id.
if (r.movedOpens) {
  failures.push('an identity opened under an instance id it was not bound to - the seal is not bound to its routing');
}

// 4. A call carries its request id and the time inside the seal, or every
//    instance refuses it as a possible replay (relay.OpenCall, ReplayGuard).
const call = await vm.runInContext(
  `(async (key) => {
     const sealed = await relaySealCall(key, 'req-9', 'alpha', 'POST', '/api/links', '{"urls":[]}');
     const plain = await relayOpen(key, relayRequestAAD('req-9', 'alpha'), sealed);
     return plain ? JSON.parse(relayFromUtf8(plain)) : null;
   })`,
  ctx,
)(frameKey);
if (!call) {
  failures.push('a call this port sealed could not be opened again');
} else {
  if (call.id !== 'req-9') failures.push(`a sealed call carries the id ${JSON.stringify(call.id)}, want its request id`);
  if (typeof call.sent !== 'number' || Math.abs(Date.now() / 1000 - call.sent) > 60) {
    failures.push(`a sealed call carries the time ${JSON.stringify(call.sent)}, want now in Unix seconds`);
  }
}

// 5. What the real hello frame puts on the wire. The checks above would pass
//    even if the session still sent the name in the clear beside the seal.
const session = vm.runInContext(
  `((opts) => relaySession(opts, async () => 'done'))`,
  ctx,
);
await session({
  url: 'ws://relay.invalid/relay/connect',
  key: 'a-key-long-enough-for-the-relay',
  frameKey,
  selfId: 'brave',
  selfName: 'jdp-workstation',
});

const hello = sent.map((raw) => JSON.parse(raw)).find((f) => f.type === 'hello');
if (!hello) {
  failures.push('the session sent no hello frame at all');
} else {
  const announce = hello.data?.announce ?? {};
  for (const leaked of ['name', 'deployment', 'client']) {
    if (leaked in announce) {
      failures.push(`the hello frame still announces "${leaked}" in the clear: ${JSON.stringify(announce)}`);
    }
  }
  if (!announce.sealed) failures.push('the hello frame carries no sealed identity');
  if (announce.instanceId !== 'brave') {
    failures.push(`the hello frame lost the id the relay routes on: ${JSON.stringify(announce)}`);
  }
  // The name is inside the seal, not just missing.
  if (announce.sealed) {
    const opened = await vm.runInContext(
      `((key, id, blob) => relayOpen(key, relayAnnounceAAD(id), blob).then(p => p && relayFromUtf8(p)))`,
      ctx,
    )(frameKey, 'brave', announce.sealed);
    const parsed = opened ? JSON.parse(opened) : null;
    if (parsed?.name !== 'jdp-workstation') {
      failures.push(`the hello's seal opened into ${JSON.stringify(parsed)}, want the browser's own name`);
    }
  }
}

// 6. Frames are handled in the order they came. A sibling that announces
//    itself and leaves straight away is gone from the roster, even though its
//    announce has a seal to open and its presence frame has none.
const gone = 'c'.repeat(40);
const goneSeal = await vm.runInContext(
  `((key, id) => relaySeal(key, relayAnnounceAAD(id), relayUtf8(JSON.stringify({ name: 'cellar', deployment: 'container' }))))`,
  ctx,
)(frameKey, gone);
FakeSocket.answer = (socket, frame) => {
  if (frame.type !== 'hello') return;
  const deliver = (type, data) => socket.onmessage?.({ data: JSON.stringify({ type, data }) });
  deliver('announce', { instanceId: gone, sealed: goneSeal });
  deliver('presence', { instanceId: gone, online: false });
};
const roster = await vm.runInContext(`((opts) => relaySession(opts, async ({ siblings }) => siblings))`, ctx)({
  url: 'ws://relay.invalid/relay/connect',
  key: 'a-key-long-enough-for-the-relay',
  frameKey,
  selfId: 'd'.repeat(40),
  selfName: 'Browser',
});
FakeSocket.answer = null;
if (roster.some((s) => s.instanceId === gone)) {
  failures.push('an instance that announced itself and left at once is still offered: its presence frame was undone by the announce');
}

// 7. The relay can write an announce's plaintext fields itself, so only a seal
//    that opens names a peer. An unsealed announce is a bare id: no name, and
//    no client flag that would take a real instance off the target list.
const forged = 'e'.repeat(40);
FakeSocket.answer = (socket, frame) => {
  if (frame.type !== 'hello') return;
  socket.onmessage?.({
    data: JSON.stringify({
      type: 'announce',
      data: { instanceId: forged, name: 'login-knightloader', deployment: 'mobile', client: true, address: 'https://login.evil' },
    }),
  });
};
const unsealed = await vm.runInContext(`((opts) => relaySession(opts, async ({ siblings }) => siblings))`, ctx)({
  url: 'ws://relay.invalid/relay/connect',
  key: 'a-key-long-enough-for-the-relay',
  frameKey,
  selfId: 'd'.repeat(40),
  selfName: 'Browser',
});
FakeSocket.answer = null;
const bare = unsealed.find((s) => s.instanceId === forged);
if (!bare) {
  failures.push('an unsealed announce took the peer off the list: its plaintext client flag was believed');
} else if (bare.name !== '' || bare.deployment !== '') {
  failures.push(`an unsealed announce was listed with the identity it claims in plaintext: ${JSON.stringify(bare)}`);
}

if (failures.length) {
  console.error('The extension’s relay port fails its checks:\n');
  for (const f of failures) console.error(`  - ${f}`);
  console.error('\nThe frame format has to agree with the Go and mobile ports byte for byte. See internal/relay/protocol.go.');
  process.exit(1);
}

console.log('relay seal: the extension port agrees with the Go and mobile vector');
