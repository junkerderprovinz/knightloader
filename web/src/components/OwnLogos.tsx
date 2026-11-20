// Logos for the debrid services, captcha solvers and hosters GlimStone's
// catalogs give no mark. A catalog's own mark always wins. One without wears
// the picture KnightLoader bundles for its site (lib/serviceMarks.ts), and one
// without that GlimStone's glyph.
import type { TileMark } from '../lib/catalogs/catalog';
import { serviceMark } from '../lib/serviceMarks';
import { siteOf, type Brand } from './BrandPicker';

/** hasOwnLogo reports whether KnightLoader bundles a picture for the site at `address`, a bare host or a full URL. */
export function hasOwnLogo(address: string): boolean {
  return !!serviceMark(siteOf(address) || address);
}

/**
 * ownTile is what a picker's tile wears in place of the catalog's glyph: the
 * bundled picture for `host`, the site the provider lives at, which
 * BrandPicker draws through HosterIcon.
 */
export function ownTile(mark: TileMark | undefined, host?: string): Partial<Brand> | undefined {
  if (mark || !host || !hasOwnLogo(host)) return undefined;
  return { mark: undefined, paint: undefined, tile: undefined, host };
}
