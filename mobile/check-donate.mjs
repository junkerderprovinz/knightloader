// The crypto window's addresses match the web UI's.
//
// A donation address that drifts on one surface fails nobody's test: nothing in
// the app ever reads it back, and the person it fails is a stranger who tried to
// give something and never writes. So the app's list (src/donate.ts) is held to
// the web UI's (web/src/lib/donate.ts) coin by coin and chain by chain, and both
// to the hand-written table of which wallet each chain resolves to. The web's
// own check-donate-addresses.mjs checks each address's format.
//
// Every coin also needs its mark in assets/, where the window's tiles look for
// it by id, or the tile is left with a ticker and a hole.
//
// Run by hand and by CI, from mobile/: `node check-donate.mjs`
import { existsSync, readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const problems = [];
const fail = (why) => problems.push(why);

const app = await import(pathToFileURL(join(here, 'src', 'donate.ts')).href);
const web = await import(pathToFileURL(join(here, '..', 'web', 'src', 'lib', 'donate.ts')).href);

const shape = (coins) =>
  coins.map((c) => ({
    id: c.id,
    symbol: c.symbol,
    name: c.name,
    networks: c.networks.map((n) => ({ id: n.id, name: n.name, address: n.address, note: Boolean(n.noteKey) })),
  }));
if (JSON.stringify(shape(app.CRYPTO_COINS)) !== JSON.stringify(shape(web.CRYPTO_COINS))) {
  fail('src/donate.ts: CRYPTO_COINS differs from web/src/lib/donate.ts - coins, chains, names or addresses no longer agree');
}
if (JSON.stringify(app.ADDRESS_BY_CHAIN) !== JSON.stringify(web.ADDRESS_BY_CHAIN)) {
  fail('src/donate.ts: ADDRESS_BY_CHAIN differs from web/src/lib/donate.ts');
}

const dialog = readFileSync(join(here, 'src', 'components', 'CryptoDonate.tsx'), 'utf8');
for (const coin of app.CRYPTO_COINS) {
  if (coin.networks.length === 0) fail(`src/donate.ts: ${coin.id} offers no network`);
  for (const n of coin.networks) {
    const want = app.ADDRESS_BY_CHAIN[n.id];
    if (!want) fail(`src/donate.ts: ${coin.id} offers ${n.id}, which has no wallet in ADDRESS_BY_CHAIN`);
    else if (n.address !== want) fail(`src/donate.ts: ${coin.id} on ${n.id} sends to ${n.address}, not ${want}`);
  }
  const asset = `assets/coin-${coin.id}.png`;
  if (!existsSync(join(here, asset))) fail(`${asset} is missing, so the ${coin.symbol} tile has no mark`);
  if (!dialog.includes(`../../${asset}`)) fail(`src/components/CryptoDonate.tsx never requires ${asset}`);
}

if (problems.length) {
  console.error(`check-donate: ${problems.length} problem(s).`);
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}
console.log('ok: every coin and chain matches the web UI, each chain sends to its own wallet, and every tile has its mark');
