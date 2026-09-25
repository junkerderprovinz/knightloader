// The crypto window's addresses match the web UI's, and the QR library is the
// published one.
//
// A donation address that drifts on one surface fails nobody's test: nothing in
// the app ever reads it back, and the person it fails is a stranger who tried to
// give something and never writes. So the extension's list (src/donate.js) is
// held to the web UI's (web/src/lib/donate.ts) coin by coin and chain by chain,
// and both to the hand-written table of which wallet each chain resolves to.
// The web's own check-donate-addresses.mjs checks each address's format.
//
// src/vendor/qrcode.js is qrcode-generator 2.0.4 as npm publishes it, the
// version the web UI uses. Store reviewers compare a bundled library with its
// release, so the file is pinned by hash: an edit to it fails here rather than
// in review.
//
// Run by CI and by hand: `node extension/check-donate.mjs`.
import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const problems = [];
const fail = (why) => problems.push(why);

// donate.js is a plain script, so it is evaluated rather than imported.
const src = readFileSync(join(here, 'src', 'donate.js'), 'utf8');
const ext = new Function(`${src}\nreturn { CRYPTO_COINS, ADDRESS_BY_CHAIN, COIN_MARKS, BTC_LETTER };`)();
const web = await import(pathToFileURL(join(here, '..', 'web', 'src', 'lib', 'donate.ts')).href);

const shape = (coins) =>
  coins.map((c) => ({
    id: c.id,
    symbol: c.symbol,
    name: c.name,
    tile: c.tile,
    networks: c.networks.map((n) => ({ id: n.id, name: n.name, address: n.address, note: Boolean(n.noteKey) })),
  }));
if (JSON.stringify(shape(ext.CRYPTO_COINS)) !== JSON.stringify(shape(web.CRYPTO_COINS))) {
  fail('src/donate.js: CRYPTO_COINS differs from web/src/lib/donate.ts in its coins, chains, names, addresses or tile colours');
}
if (JSON.stringify(ext.ADDRESS_BY_CHAIN) !== JSON.stringify(web.ADDRESS_BY_CHAIN)) {
  fail('src/donate.js: ADDRESS_BY_CHAIN differs from web/src/lib/donate.ts');
}
for (const coin of ext.CRYPTO_COINS) {
  if (coin.networks.length === 0) fail(`src/donate.js: ${coin.id} offers no network`);
  for (const n of coin.networks) {
    const want = ext.ADDRESS_BY_CHAIN[n.id];
    if (!want) fail(`src/donate.js: ${coin.id} offers ${n.id}, which has no wallet in ADDRESS_BY_CHAIN`);
    else if (n.address !== want) fail(`src/donate.js: ${coin.id} on ${n.id} sends to ${n.address}, not ${want}`);
  }
  if (!ext.COIN_MARKS[coin.id]) fail(`src/donate.js: ${coin.id} has no mark for its tile`);
}
if (!ext.BTC_LETTER.d.startsWith('M17.288 10.291')) {
  fail('src/donate.js: BTC_LETTER no longer starts at the letterform - the disc is back, or the letter is gone');
}

const QR_SHA256 = '79ec86f82856005b1c887905cfccfcfbec3821ca61c7fd5a952faa5f778f791c';
const qrSrc = readFileSync(join(here, 'src', 'vendor', 'qrcode.js'));
const got = createHash('sha256').update(qrSrc).digest('hex');
if (got !== QR_SHA256) {
  fail(`src/vendor/qrcode.js: sha256 ${got} is not qrcode-generator 2.0.4's dist/qrcode.js - bundle the published file unmodified`);
} else {
  // The library the window calls, run on every address it can show.
  const qrcode = new Function(`${qrSrc.toString('utf8')}\nreturn qrcode;`)();
  for (const address of new Set(Object.values(ext.ADDRESS_BY_CHAIN))) {
    const qr = qrcode(0, 'M');
    qr.addData(address);
    qr.make();
    if (!(qr.getModuleCount() >= 21)) fail(`src/vendor/qrcode.js: no code for ${address}`);
  }
}

if (problems.length) {
  console.error(`check-donate: ${problems.length} problem(s).`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}
console.log('ok: every coin and chain matches the web UI, each chain sends to its own wallet, and the QR library is the published one');
