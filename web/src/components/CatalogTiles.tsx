// The tiles of a picker drawn from one of GlimStone's provider catalogs
// (lib/catalogs): a brand wears the catalog's mark on a tile that lights up in
// its colour, and a provider without one wears its glyph on a tile that lights
// up in the accent.
import { useId, type ReactNode } from 'react';
import { catalogPicker, type Catalog, type CatalogPick, type TileMark } from '../lib/catalogs/catalog';
import { SubHead } from '../pages/settings/controls';
import { BrandPicker, type Brand } from './BrandPicker';

const isTileMark = (mark: unknown): mark is TileMark =>
  typeof mark === 'object' && mark !== null && typeof (mark as TileMark).svg === 'string';

/** withOwnIds gives a mark's gradients and masks ids of their own, because
 *  one mark can stand on a tile, a row and in a window at once, and a hidden
 *  copy would take its drawing's gradients with it. */
function withOwnIds(svg: string, suffix: string): string {
  return svg
    .replace(/id="([^"]+)"/g, `id="$1-${suffix}"`)
    .replace(/url\(#([^)]+)\)/g, `url(#$1-${suffix})`)
    .replace(/href="#([^"]+)"/g, `href="#$1-${suffix}"`);
}

/** TileMarkView draws a catalog's mark, with the drawing a lit tile shows instead where it has one. */
export function TileMarkView({ mark }: { mark: TileMark }) {
  const suffix = useId().replace(/[^a-zA-Z0-9]/g, '');
  const rest = withOwnIds(mark.svg, suffix);
  const html = mark.hover
    ? rest.replace('<svg ', '<svg class="glim-mark-rest" ') +
      withOwnIds(mark.hover, `${suffix}h`).replace('<svg ', '<svg class="glim-mark-hover" ')
    : rest;
  return <span className={`contents ${mark.className ?? ''}`} dangerouslySetInnerHTML={{ __html: html }} />;
}

/** GlyphMark is one of the app's glyphs as a mark, in the accent like every house mark. */
export function GlyphMark({ children }: { children: ReactNode }) {
  return <span className="glim-house-mark flex h-full w-full items-center justify-center">{children}</span>;
}

/**
 * ProviderMark is the mark a provider's tile wears, beside its row or at the
 * head of its form: the catalog's mark, or the app's glyph for it.
 */
export function ProviderMark({ mark, glyph, size }: { mark?: TileMark; glyph: ReactNode; size?: number }) {
  return (
    <span aria-hidden className="glim-client-mark" style={size ? { width: size, height: size } : undefined}>
      {mark ? <TileMarkView mark={mark} /> : glyph}
    </span>
  );
}

export interface BrandGroup {
  id: string;
  title: string;
  hint?: string;
  brands: Brand[];
}

/**
 * catalogBrands turns the providers an app offers from a catalog into its
 * picker's groups of brands, the tile that takes anything last in its group.
 * `more` adds what only the app knows about a provider, such as that it is
 * connected already, and `own` gives a tile the app's own logo where the
 * catalog has no mark, or corrects a mark (OwnLogos.tsx).
 */
export function catalogBrands<G extends string, I extends string>(
  catalog: Catalog<G, I>,
  pick: CatalogPick<G, I, ReactNode>,
  more?: (id: I) => Partial<Brand> | undefined,
  own?: (id: I, mark: TileMark | undefined) => Partial<Brand> | undefined,
): BrandGroup[] {
  const { items, groups } = catalogPicker(catalog, pick);
  return groups.map((g) => ({
    ...g,
    brands: items
      .filter((item) => item.group === g.id)
      .sort((a, b) => Number(!!a.last) - Number(!!b.last))
      .map((item): Brand => {
        const mark = item.mark;
        const lit = isTileMark(mark) && mark.tile && mark.ink ? { tile: mark.tile, ink: mark.ink } : undefined;
        return {
          id: item.id,
          name: item.label,
          sub: item.status ?? item.sub,
          terms: item.terms,
          mark: isTileMark(mark) ? <TileMarkView mark={mark} /> : <GlyphMark>{mark}</GlyphMark>,
          paint: lit,
          tile: lit ? undefined : 'glim-tile-house',
          unavailable: item.status ? (item.hint ?? item.status) : undefined,
          ...own?.(item.id as I, isTileMark(mark) ? mark : undefined),
          ...more?.(item.id as I),
        };
      }),
  }));
}

/** The groups a search leaves a tile in, each with only the tiles it found. */
export function narrowGroups(groups: BrandGroup[], keep: (b: Brand) => boolean): BrandGroup[] {
  return groups.map((g) => ({ ...g, brands: g.brands.filter(keep) })).filter((g) => g.brands.length > 0);
}

/**
 * PickerGroups is a picker's tiles in their groups, each under its eyebrow
 * where the group has one. The arrow keys cross from one group to the next.
 */
export function PickerGroups({
  groups,
  onPick,
  none,
  selected,
}: {
  groups: BrandGroup[];
  onPick: (id: string) => void;
  /** What to say when the search leaves no tile. */
  none: string;
  selected?: string;
}) {
  if (groups.length === 0) return <BrandPicker brands={[]} none={none} onPick={onPick} />;
  return (
    <div data-new="picker-look" className="flex flex-col gap-4">
      {groups.map((g) => (
        <div key={g.id} className="flex flex-col gap-2">
          {g.title && <SubHead label={g.title} hint={g.hint} />}
          <BrandPicker brands={g.brands} none="" selected={selected} onPick={onPick} />
        </div>
      ))}
    </div>
  );
}
