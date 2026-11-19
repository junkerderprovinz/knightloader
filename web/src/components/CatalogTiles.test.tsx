// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { catalogPicker, type Catalog } from '../lib/catalogs/catalog';
import { VPN_PROVIDERS } from '../lib/catalogs/vpnProviders';
import { catalogBrands, narrowGroups } from './CatalogTiles';
import { brandMatches } from './BrandPicker';

const CATALOG: Catalog<'a' | 'b'> = {
  groups: ['a', 'b'],
  entries: [
    { id: 'zeta', name: 'Zeta', group: 'a', mark: { svg: '<svg></svg>', tile: '#123456', ink: '#ffffff' } },
    { id: 'any', name: 'Any address', group: 'a', glyph: 'IconLink', word: true },
    { id: 'alpha', name: 'Alpha', group: 'a', terms: ['first'], glyph: 'IconLink' },
    { id: 'beta', name: 'Beta', group: 'b', glyph: 'IconLink' },
  ],
};

describe('catalogPicker', () => {
  it('keeps the offered providers in the catalog group order, alphabetical within a group', () => {
    const { items, groups } = catalogPicker(CATALOG, {
      ids: ['beta', 'zeta', 'alpha', 'unknown'],
      titles: { a: 'A', b: { title: 'B', hint: 'Why' } },
      glyph: () => 'glyph',
      lang: 'en',
    });
    expect(items.map((i) => i.id)).toEqual(['alpha', 'zeta', 'beta']);
    expect(groups).toEqual([
      { id: 'a', title: 'A' },
      { id: 'b', title: 'B', hint: 'Why' },
    ]);
  });

  it('finds a translated name by the catalog name as well', () => {
    const { items } = catalogPicker(CATALOG, {
      ids: ['any'],
      titles: { a: '', b: '' },
      glyph: () => 'glyph',
      lang: 'de',
      own: () => ({ label: 'Jede Adresse' }),
    });
    expect(items[0]).toMatchObject({ label: 'Jede Adresse', terms: ['Any address'] });
  });
});

describe('catalogBrands', () => {
  const groups = catalogBrands(CATALOG, {
    ids: ['zeta', 'any', 'alpha', 'beta'],
    titles: { a: '', b: 'B' },
    glyph: () => <i />,
    lang: 'en',
    own: (e) => (e.id === 'any' ? { last: true } : e.id === 'beta' ? { status: 'Not yet', hint: 'Soon' } : undefined),
  });

  it('lights a brand in its own colours and a glyph in the accent', () => {
    const [zeta] = groups[0].brands.filter((b) => b.id === 'zeta');
    const [alpha] = groups[0].brands.filter((b) => b.id === 'alpha');
    expect(zeta).toMatchObject({ paint: { tile: '#123456', ink: '#ffffff' }, tile: undefined });
    expect(alpha).toMatchObject({ paint: undefined, tile: 'glim-tile-house' });
  });

  it('puts the tile that takes anything last in its group', () => {
    expect(groups[0].brands.map((b) => b.id)).toEqual(['alpha', 'zeta', 'any']);
  });

  it('keeps a provider that is not ready in view, greyed out with its reason', () => {
    expect(groups[1].brands[0]).toMatchObject({ id: 'beta', sub: 'Not yet', unavailable: 'Soon' });
  });

  it('finds a tile by its search words and drops the groups a search empties', () => {
    expect(narrowGroups(groups, (b) => brandMatches(b, 'first')).map((g) => g.brands.map((b) => b.id))).toEqual([['alpha']]);
  });
});

describe('the VPN catalog', () => {
  it('gives Proton VPN a drawing of its own for the lit tile', () => {
    expect(VPN_PROVIDERS.entries.find((e) => e.id === 'protonvpn')?.mark?.hover).toBeTruthy();
  });
});
