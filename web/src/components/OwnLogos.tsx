// Logos for the providers GlimStone's catalogs give only a glyph. A catalog's
// own mark always wins.
//
// The VPN logos are the PNGs in public/vpn. Debrid services, captcha solvers
// and hosters use what HosterIcon draws for their site. Jan wears BombVault's
// mark, the vendor's raster icon wrapped in SVG.
import type { ReactNode } from 'react';
import type { TileMark } from '../lib/catalogs/catalog';
import { withBase } from '../lib/basePath';
import type { LogoTile } from '../lib/logoTile';
import type { Brand } from './BrandPicker';
import { TileMarkView } from './CatalogTiles';
import { Logo } from './HosterIcon';

export type OwnCatalog = 'vpn' | 'mcp' | 'download' | 'captcha';

const VPN_LOGOS = new Map<string, LogoTile>([
  ['anonine', { tile: '#000000', ink: '#ffffff', rest: 'ink' }],
  ['astrill', { tile: '#000000', ink: '#ffffff', rest: 'ink' }],
  ['cryptostorm', { tile: '#286666', ink: '#ffffff' }],
  ['fastestvpn', { tile: '#f9c02a', ink: '#161616' }],
  ['ivacy', { tile: '#24aaf6', ink: '#ffffff' }],
  ['torguard', { tile: '#000000', ink: '#ffffff', rest: 'ink' }],
  ['vpnac', { tile: '#13314c', ink: '#ffffff', lit: true }],
  ['xeovo', { tile: '#06da69', ink: '#161616' }],
  ['tunnelbear', { tile: '#dc9a2c', ink: '#ffffff' }],
  ['turbovpn', { tile: '#fa7a48', ink: '#ffffff' }],
]);

// Jan's hand is a picture, which the lit tile shows as its outline in the ink.
const JAN =
  "<svg viewBox=\"0 0 96 96\" xmlns=\"http://www.w3.org/2000/svg\" xmlns:xlink=\"http://www.w3.org/1999/xlink\"><image width=\"96\" height=\"96\" xlink:href=\"data:image/png;base64,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\"/></svg>";

const MCP_MARKS: Record<string, TileMark> = {
  jan: {
    svg: JAN,
    hover: JAN.replace('<image ', '<filter id="ink"><feFlood flood-color="currentColor"/><feComposite in2="SourceAlpha" operator="in"/></filter><image filter="url(#ink)" '),
    tile: '#f17455',
    ink: '#ffffff',
  },
};

function vpnLogo(id: string, size: number, lit: boolean): ReactNode {
  const logo = VPN_LOGOS.get(id);
  if (!logo) return undefined;
  const src = withBase(`/vpn/${id}.png`);
  const mask = logo.lit ? withBase(`/vpn/${id}.lit.png`) : src;
  return <Logo src={src} mask={mask} info={logo} box={{ width: size, height: size }} lit={lit} />;
}

/**
 * ownTile is what a picker's tile wears in place of the catalog's glyph: a
 * logo of KnightLoader's. `host` is the site a debrid service, captcha solver
 * or hoster lives at.
 */
export function ownTile(
  catalog: OwnCatalog,
  id: string,
  mark: TileMark | undefined,
  host?: string,
): Partial<Brand> | undefined {
  if (mark) return undefined;
  if (catalog === 'vpn') {
    const logo = VPN_LOGOS.get(id);
    return logo && { mark: vpnLogo(id, 25, true), paint: logo, tile: undefined };
  }
  if (catalog === 'mcp') {
    const own = MCP_MARKS[id];
    return own && { mark: <TileMarkView mark={own} />, paint: { tile: own.tile!, ink: own.ink! }, tile: undefined };
  }
  // Without a mark of its own, BrandPicker draws the logo HosterIcon has for the host.
  return host ? { mark: undefined, paint: undefined, tile: undefined, host } : undefined;
}

/** ownRowMark is the mark a row or a form head shows for a provider whose tile wears one from here. */
export function ownRowMark(catalog: 'vpn' | 'mcp', id: string, mark: TileMark | undefined, size: number): ReactNode {
  if (mark) return undefined;
  if (catalog === 'vpn') return vpnLogo(id, size, false);
  const own = MCP_MARKS[id];
  return own && <TileMarkView mark={own} />;
}
