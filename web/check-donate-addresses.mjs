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
// The last check is the important one, and it is about the SHAPE of the list
// rather than about any single address. The first version of this list was
// grouped by coin and named "Tether" with the networks "BNB, Tron, Solana,
// Ethereum" above a single 0x… address. That address exists on EVM chains
// only. A donor picking Tron would have sent USDT into nothing. Grouping by
// chain is what makes the wrong choice unofferable, and that is what the last
// check here holds down.
//
// Run by hand or from CI: `node web/check-donate-addresses.mjs`.
import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';

const src = readFileSync(new URL('./src/lib/donate.ts', import.meta.url), 'utf8');

/** The list as data, read out of the source rather than imported: this file is
 *  plain node with no TypeScript loader, the same as every other check here. */
function chains() {
  const body = src.slice(src.indexOf('export const CRYPTO_CHAINS'));
  const out = [];
  for (const block of body.split(/\n  \{\n/).slice(1)) {
    const field = (name) => {
      const m = block.match(new RegExp(`${name}: '([^']*)'`));
      return m ? m[1] : undefined;
    };
    if (field('id')) out.push({ id: field('id'), name: field('name'), coins: field('coins'), networks: field('networks'), address: field('address') });
  }
  return out;
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

const list = chains();
if (list.length < 3) note(`only ${list.length} chains parsed out of donate.ts - the reader is broken, not the list`);

const by = (id) => list.find((c) => c.id === id);

// 1. Bitcoin, by its own bech32 checksum. And the check is not a rubber stamp:
//    one flipped character has to fail it.
const btc = by('btc');
if (!btc || !bech32Ok(btc.address)) note('the Bitcoin address fails its bech32 checksum');
if (btc && bech32Ok(btc.address.replace(/.$/, (c) => (c === 'a' ? 'q' : 'a')))) note('the bech32 check passes a broken address - it is not checking anything');

// 2. XRP, by base58check, plus the prefix that says this is an account rather
//    than some other XRPL object.
const xrp = by('xrp');
if (xrp) {
  const raw = b58Decode(xrp.address, XRP58);
  if (!raw || raw.length !== 25) note('the XRP address does not decode to 25 bytes');
  else {
    const want = createHash('sha256').update(createHash('sha256').update(raw.subarray(0, 21)).digest()).digest().subarray(0, 4);
    if (!raw.subarray(21).equals(want)) note('the XRP address fails its base58check checksum');
    if (raw[0] !== 0) note('the XRP address is not an account address');
  }
}

// 3. Solana: 32 bytes in the base58 alphabet.
const sol = by('sol');
if (sol) {
  const raw = b58Decode(sol.address, B58);
  if (!raw || raw.length !== 32) note('the Solana address is not a 32-byte key');
}

// 4. The two hex ones, by length.
const evm = by('evm');
if (evm && !/^0x[0-9a-fA-F]{40}$/.test(evm.address)) note('the EVM address is not 20 bytes of hex');
const sui = by('sui');
if (sui && !/^0x[0-9a-fA-F]{64}$/.test(sui.address)) note('the Sui address is not 32 bytes of hex');

// 5. The shape of the list: no chain an address cannot live on. An EVM address
//    is offered for EVM networks only, and nothing anywhere mentions Tron,
//    since there is no Tron address here.
if (evm && !evm.networks) note('the EVM entry names no networks, so a donor cannot tell where it is valid');
for (const bad of ['tron', 'solana']) {
  if (evm?.networks?.toLowerCase().includes(bad)) note(`the EVM entry offers ${bad}, where that address does not exist`);
}
if (JSON.stringify(list).toLowerCase().includes('tron')) note('the list mentions Tron, and there is no Tron address in it');

// 6. Every entry says what can be sent and where it goes.
for (const c of list) {
  if (!c.name || !c.coins || !c.address || c.address.length < 21) note(`the ${c.id} entry is incomplete`);
}
if (new Set(list.map((c) => c.id)).size !== list.length) note('two chains share an id');

if (!fail) console.log(`OK  ${list.length} donation addresses check out`);
process.exit(fail);
