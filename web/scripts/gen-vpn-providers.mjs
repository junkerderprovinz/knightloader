// Rebuilds src/lib/vpnProviders.ts and the logos in public/vpn from the VPN
// provider list the engine is built on (providers.json: id, name,
// wireguard.method and .fields, portForwarding.supported, logo.url or
// logo.file for a mark cut out by hand, and tile and tileOrder where the list
// picks the tiles).
//
//   npm i --no-save sharp
//   node scripts/gen-vpn-providers.mjs <providers.json>
//
// The list words each provider's fields for a person, so the form fields are
// read out of those words. Each logo becomes a 64-pixel PNG lifted off its
// plate, with its colours on a brand tile and a lit mask where it needs one,
// as gen-hoster-logos.mjs does. A provider without a logo URL, or one that
// does not load, gets the letter tile. Look at public/vpn afterwards: a logo
// of the wrong brand passes here.
import { mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { entry, prepare } from './logo-pipeline.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const out = join(here, '..', 'public', 'vpn');
const list = join(here, '..', 'src', 'lib', 'vpnProviders.ts');

// WireGuard's dragon stands on a red disc in its icon, which the own
// configuration's tile leaves off like any other plate.
const ON_DISC = new Set(['custom']);
// Some sites refuse a script's own user agent.
const BROWSER = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Safari/537.36';

const source = process.argv[2];
if (!source) {
  console.error('usage: node scripts/gen-vpn-providers.mjs <providers.json>');
  process.exit(2);
}

let sharp;
try {
  sharp = (await import('sharp')).default;
} catch {
  console.error('sharp is missing: npm i --no-save sharp');
  process.exit(2);
}

/**
 * fieldsOf turns a provider's way in into the form's fields, in the order the
 * form asks for them. A .conf carries everything, so it stands alone.
 */
function fieldsOf(wg) {
  const words = [wg.method ?? '', ...(wg.fields ?? [])].join(' | ').toLowerCase();
  if (/\.conf|conf-upload|config file/.test(words)) return ['config'];
  const fields = [];
  if (/private key/.test(words)) fields.push('privateKey', 'address');
  if (/account (number|id)|api-account/.test(words)) fields.push('accountNumber');
  if (/token/.test(words) && !/username|password/.test(words)) fields.push('token');
  if (/username|user name|login/.test(words)) fields.push('username', 'password');
  if (/server|location|region|country/.test(words)) fields.push('location');
  return fields.length > 0 ? fields : ['config'];
}

/** logo writes a provider's logo to public/vpn and is its colours, or null without one. */
async function logo(id, l) {
  const from = l?.file ?? l?.url;
  if (!from) return null;
  try {
    let input;
    if (l.file) input = await readFile(l.file);
    else {
      const r = await fetch(l.url, { headers: { 'user-agent': BROWSER }, signal: AbortSignal.timeout(15000) });
      if (!r.ok) throw new Error(`HTTP ${r.status}`);
      input = Buffer.from(await r.arrayBuffer());
    }
    const made = await prepare(sharp, input, { disc: ON_DISC.has(id) });
    await writeFile(join(out, `${id}.png`), made.png);
    if (made.lit) await writeFile(join(out, `${id}.lit.png`), made.lit);
    return made.info;
  } catch (err) {
    console.log(`no logo from ${from}: ${err.message}`);
    return null;
  }
}

// A list that marks its tiles shows those alone, in their order, with the own
// configuration last. A tile whose provider keeps WireGuard to its own app is
// shown after them, greyed out, so nobody looks for it in vain.
const all = JSON.parse(await readFile(source, 'utf8'));
const tiled = all.some((p) => 'tile' in p);
const order = (a, b) => (a.id === 'custom') - (b.id === 'custom') || (a.tileOrder ?? 0) - (b.tileOrder ?? 0);
const providers = tiled ? all.filter((p) => p.tile === true && p.wireguard?.supported !== false).sort(order) : all;
const appOnly = tiled ? all.filter((p) => p.tile === true && p.wireguard?.supported === false).sort(order) : [];
await rm(out, { recursive: true, force: true });
await mkdir(out, { recursive: true });

const entries = [];
for (const p of providers) {
  entries.push({
    id: p.id,
    name: p.name,
    fields: fieldsOf(p.wireguard ?? {}),
    portForwarding: p.portForwarding?.supported === true,
    logo: await logo(p.id, p.logo),
  });
}

const shut = [];
for (const p of appOnly) {
  shut.push({ id: p.id, name: p.name, logo: await logo(p.id, p.logo) });
}

const quote = (s) => `'${s.replace(/\\/g, '\\\\').replace(/'/g, "\\'")}'`;
const logoOf = (info) => (info ? entry(info) : 'null');

const body = entries
  .map(
    (e) =>
      `  {\n    id: '${e.id}',\n    name: ${quote(e.name)},\n` +
      `    fields: [${e.fields.map((f) => `'${f}'`).join(', ')}],\n    portForwarding: ${e.portForwarding},\n    logo: ${logoOf(e.logo)},\n  },\n`,
  )
  .join('');
await writeFile(
  list,
  `// Generated by scripts/gen-vpn-providers.mjs, do not edit by hand: the VPN
// providers KnightLoader can connect to, and the colours of each one's logo in
// public/vpn.
import type { VpnField } from './api';
import type { LogoTile } from './logoTile';

export interface VpnCatalogEntry {
  id: string;
  name: string;
  fields: VpnField[];
  portForwarding: boolean;
  logo: LogoTile | null;
}

export const VPN_PROVIDERS: readonly VpnCatalogEntry[] = [
${body}];

/** Providers that keep WireGuard to their own app, shown greyed out after the rest. */
export const VPN_APP_ONLY: readonly { id: string; name: string; logo: LogoTile | null }[] = [
${shut.map((e) => `  { id: '${e.id}', name: ${quote(e.name)}, logo: ${logoOf(e.logo)} },\n`).join('')}];
`,
);
console.log(`${entries.length} providers, ${entries.filter((e) => e.logo).length} with a logo, ${shut.length} app-only`);
