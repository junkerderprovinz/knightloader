// The extension's port of the relay frame format, checked against the same
// fixed vector internal/relay's Go tests pin.
//
// Three implementations speak this protocol - internal/relay in Go, the phone
// in TypeScript (mobile/src/api/relayFrame.ts), and this extension in plain
// JavaScript - and only the first two had any test at all. A change to the
// domain string, the \x00 separator, the JSON field names or the nonce
// framing keeps every Go test green and ships as "the browser joins the group
// and every instance shows it with no name", or worse, as an extension whose
// announce no sibling can open.
//
// The vector below is byte-identical to the one in
// internal/relay/announce_seal_test.go, produced with the phone's own cipher
// library. Its nonce is a fixed run of 0x07 because a vector has to be
// reproducible; nothing in production seals with a fixed nonce.
//
// It loads src/relay.js as a script rather than reimplementing anything, so
// what is checked is the code that ships. Run by CI beside check-locales.mjs.

import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';
import { createHash, webcrypto } from 'node:crypto';

const here = dirname(fileURLToPath(import.meta.url));
const src = readFileSync(join(here, 'src', 'relay.js'), 'utf8');

// The globals relay.js reaches for. The WebSocket is a fake that opens at
// once and records what is written to it, so the last check below can read
// the REAL hello frame the shipped code produces rather than a reconstruction
// of it. Nothing here touches the network.
const sent = [];
class FakeSocket {
  constructor() {
    this.readyState = 1;
    setTimeout(() => this.onopen?.(), 0);
  }
  send(raw) {
    sent.push(raw);
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

// 2. What it seals opens again, with every field intact - the client flag
//    included, because losing that one lists the browser as somewhere to send
//    a download and it answers 501 to everything.
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

// 4. The check that matters most, and the only one above that reads the code
//    rather than the format: what does the shipped hello frame ACTUALLY put
//    on the wire?
//
//    Everything above would stay green if relaySession went on announcing the
//    browser's name in the clear beside a correctly sealed blob - which is
//    exactly the shape a half-finished version of this change has, and
//    exactly the shape that reads as fixed while fixing nothing. So this
//    drives the real session against a fake socket and inspects the frame.
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
  // And the name really is inside the seal rather than merely missing.
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

if (failures.length) {
  console.error('The extension’s relay frame format no longer matches the Go and mobile ports:\n');
  for (const f of failures) console.error(`  - ${f}`);
  console.error('\nAll three have to agree byte for byte. See internal/relay/protocol.go.');
  process.exit(1);
}

console.log('relay seal: the extension port agrees with the Go and mobile vector');
