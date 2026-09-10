// The donation addresses, checked as far as each format allows.
//
// This is the one list in the app where a typo costs a stranger real money and
// nobody ever finds out: the person it happens to is not a user, they are
// somebody who tried to give something away and got nothing back. There is no
// error state, no retry and no support ticket, because they never write.
//
// Two of the five formats carry a real checksum, so those are verified rather
// than eyeballed; the rest are pinned by length and alphabet.
//
// The check that matters most is the last kind, and it is about the SHAPE of
// the list rather than any single address: every network a donor can pick must
// point at the wallet that actually lives on that chain. The list was first
// written grouped by coin, naming "Tether" with the networks "BNB, Tron,
// Solana, Ethereum" above a single 0x… address that exists on EVM chains only.
// A donor picking Tron would have sent USDT into nothing. donate.ts carries a
// table written out by hand saying which wallet each chain must resolve to,
// and this file holds the list to it — derived from the list it guards, such a
// check would agree with any mistake in it.
//
// Run by hand or from CI: `node web/check-donate-addresses.mjs`.
import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';

const src = readFileSync(new URL('./src/lib/donate.ts', import.meta.url), 'utf8');

/** The list as data, read out of the source rather than imported: this file is
 *  plain node with no TypeScript loader, the same as every other check here. */
function parse() {
  const wallets = {};
  for (const m of src.matchAll(/^const (BTC|EVM|SOL|SUI|XRP) = '([^']+)';/gm)) wallets[m[1]] = m[2];

  // The named chain constants (ETHEREUM, BASE, …) and the ones written inline.
  const chains = {};
  for (const m of src.matchAll(/^const [A-Z]+ = \{ id: '([^']+)', name: '([^']+)', address: (\w+) \};/gm)) {
    chains[m[1]] = { id: m[1], name: m[2], address: wallets[m[3]] };
  }
  for (const m of src.matchAll(/\{ id: '([^']+)', name: '([^']+)', address: (\w+) \}/g)) {
    if (!chains[m[1]]) chains[m[1]] = { id: m[1], name: m[2], address: wallets[m[3]] };
  }
  for (const m of src.matchAll(/id: '([^']+)',\s*\n\s*name: '([^']+)',\s*\n\s*address: (\w+),/g)) {
    if (!chains[m[1]]) chains[m[1]] = { id: m[1], name: m[2], address: wallets[m[3]] };
  }

  // The hand-written table this list is measured against.
  const table = {};
  const tableBody = src.slice(src.indexOf('ADDRESS_BY_CHAIN'));
  for (const m of tableBody.matchAll(/^\s{2}(\w+): (BTC|EVM|SOL|SUI|XRP),$/gm)) table[m[1]] = wallets[m[2]];

  // The coins, each with the chain ids it offers.
  const coins = [];
  const listBody = src.slice(src.indexOf('CRYPTO_COINS'), src.indexOf('ADDRESS_BY_CHAIN'));
  for (const block of listBody.split(/\n  \{\n/).slice(1)) {
    const id = /id: '([^']+)'/.exec(block)?.[1];
    const symbol = /symbol: '([^']+)'/.exec(block)?.[1];
    if (!id || !symbol) continue;
    const nets = [];
    const netBlock = block.slice(block.indexOf('networks:'));
    for (const name of netBlock.matchAll(/\b(ETHEREUM|BASE|OPTIMISM|BSC|SOLANA)\b/g)) {
      const byConst = { ETHEREUM: 'ethereum', BASE: 'base', OPTIMISM: 'optimism', BSC: 'bsc', SOLANA: 'solana' };
      nets.push(byConst[name[1]]);
    }
    for (const m of netBlock.matchAll(/id: '([^']+)'/g)) if (chains[m[1]]) nets.push(m[1]);
    coins.push({ id, symbol, networks: [...new Set(nets)] });
  }
  return { wallets, chains, table, coins };
}

const BECH32 = 'qpzry9x8gf2tvdw0s3jn54khce6mua7l';
const B58 = '123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz';
const XRP58 = 'rpshnaf39wBUDNEGHJKLM4PQRST7VWXYZ2bcdeCg65jkm8oFqi1tuvAxyz';

function bech32Polymod(values) {
  const gen = [0x3b6a57b2, 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3];
  let chk = 1;
  for (const v of values) {
    const b = chk >> 25;
    chk = ((chk & 0x1ffffff) << 5) ^ v;
    for (let i = 0; i < 5; i++) if ((b >> i) & 1) chk ^= gen[i];
  }
  return chk;
}

/** BIP-173/350: the checksum an address carries about itself. A single wrong
 *  character fails this, which is the whole point of the format. */
function bech32Ok(addr) {
  const lower = addr.toLowerCase();
  if (addr !== lower && addr !== addr.toUpperCase()) return false;
  const pos = lower.lastIndexOf('1');
  if (pos < 1 || pos + 7 > lower.length || lower.length > 90) return false;
  const hrp = lower.slice(0, pos);
  const data = [...lower.slice(pos + 1)].map((c) => BECH32.indexOf(c));
  if (data.some((v) => v < 0)) return false;
  const expand = [...hrp].map((c) => c.charCodeAt(0) >> 5).concat([0], [...hrp].map((c) => c.charCodeAt(0) & 31));
  const chk = bech32Polymod(expand.concat(data));
  return chk === 1 || chk === 0x2bc830a3;
}

function b58Decode(s, alphabet) {
  let num = 0n;
  for (const c of s) {
    const i = alphabet.indexOf(c);
    if (i < 0) return null;
    num = num * 58n + BigInt(i);
  }
  let hex = num.toString(16);
  if (hex.length % 2) hex = '0' + hex;
  const body = Buffer.from(hex, 'hex');
  let pad = 0;
  while (pad < s.length && s[pad] === alphabet[0]) pad++;
  return Buffer.concat([Buffer.alloc(pad), body]);
}

let fail = 0;
const note = (m) => {
  console.log('FAIL  ' + m);
  fail = 1;
};

const { wallets, chains, table, coins } = parse();

if (Object.keys(wallets).length !== 5) note(`parsed ${Object.keys(wallets).length} wallets, expected 5 - the reader is broken, not the list`);
if (coins.length < 4) note(`parsed only ${coins.length} coins - the reader is broken, not the list`);

// 1. Every offered chain points at the wallet the hand-written table names.
for (const coin of coins) {
  if (!coin.networks.length) note(`${coin.id} offers no network`);
  for (const id of coin.networks) {
    if (!table[id]) note(`${coin.id}/${id} is not a chain this app knows`);
    else if (chains[id]?.address !== table[id]) note(`${coin.id}/${id} points at the wrong wallet`);
  }
}

// 2. Bitcoin, by its own bech32 checksum, and the check is not a rubber stamp.
if (!bech32Ok(wallets.BTC)) note('the Bitcoin address fails its bech32 checksum');
if (bech32Ok(wallets.BTC.replace(/.$/, (c) => (c === 'a' ? 'q' : 'a')))) note('the bech32 check passes a broken address - it is not checking anything');

// 3. XRP, by base58check, plus the prefix saying this is an account.
const raw = b58Decode(wallets.XRP, XRP58);
if (!raw || raw.length !== 25) note('the XRP address does not decode to 25 bytes');
else {
  const want = createHash('sha256').update(createHash('sha256').update(raw.subarray(0, 21)).digest()).digest().subarray(0, 4);
  if (!raw.subarray(21).equals(want)) note('the XRP address fails its base58check checksum');
  if (raw[0] !== 0) note('the XRP address is not an account address');
}

// 4. Solana: 32 bytes in the base58 alphabet.
const sol = b58Decode(wallets.SOL, B58);
if (!sol || sol.length !== 32) note('the Solana address is not a 32-byte key');

// 5. The two hex ones, by length.
if (!/^0x[0-9a-fA-F]{40}$/.test(wallets.EVM)) note('the EVM address is not 20 bytes of hex');
if (!/^0x[0-9a-fA-F]{64}$/.test(wallets.SUI)) note('the Sui address is not 32 bytes of hex');

// 6. The EVM wallet is for EVM chains only, and Tron appears nowhere.
for (const id of ['solana', 'bitcoin', 'xrpl', 'sui']) {
  if (table[id] === wallets.EVM) note(`${id} must not share the EVM wallet`);
}
if (src.toLowerCase().includes('tron') && !src.toLowerCase().includes('picking tron')) note('the list mentions Tron, and there is no Tron address in it');

if (!fail) console.log(`OK  ${coins.length} coins over ${Object.keys(table).length} chains, ${Object.keys(wallets).length} wallets check out`);
process.exit(fail);
