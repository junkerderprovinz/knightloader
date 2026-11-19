// GlimStone's provider catalogs ("Pickers and wizards"): the files beside this
// one are copied from GlimStone's core with only their imports changed, and
// the *-marks folders keep the sources and licences of the marks. Refresh them
// from GlimStone rather than editing them here, so every app shows a provider
// the same way. This file is catalog.ts with the picker types it needs, less
// the group badges no picker here uses, and with catalogEntry for a row that
// draws the mark its tile wore.
//
// A catalog is the shared list of one kind of provider: each with its name,
// its group, the words a search finds it by and its brand's mark. An app
// offers the providers it can reach, joins them with its own data by id, and
// leaves out the rest.

/** A mark kept as markup, with the colours its tile lights up in ("Brand tiles"). */
export interface TileMark {
  svg: string;
  tile?: string;
  ink?: string;
  /** The mark drawn on the lit tile instead, for a brand whose layers do not survive one ink. */
  hover?: string;
  /** The class on the mark's box, where the app paints a mark of its own at rest. */
  className?: string;
}

export interface PickGroup {
  id: string;
  /** The eyebrow over the group's tiles. */
  title: string;
  /** What the group's name cannot say, as the (i) in its eyebrow's line. */
  hint?: string;
}

/** One tile. `M` is the app's own kind of glyph, for a choice that has no brand. */
export interface PickItem<M = unknown> {
  id: string;
  label: string;
  /** What kind of thing it is, as the tile's second line. */
  sub?: string;
  mark?: M | TileMark;
  group?: string;
  /** What the name cannot say, in the tile's info bubble. */
  hint?: string;
  /** Further words the search finds it by. */
  terms?: readonly string[];
  /** The tile that takes anything, which stands last. */
  last?: boolean;
  /** Why the tile cannot be picked yet, as its second line. The tile stays in view. */
  status?: string;
}

export interface CatalogEntry<G extends string = string, I extends string = string> {
  /** Stable, and what the app stores. */
  id: I;
  /** The provider's own name. A brand's name is not translated. */
  name: string;
  /** What kind of thing it is, as the tile's second line, such as "Windows share" beside SMB. */
  kind?: string;
  group: G;
  /** Further words a search finds it by, such as the protocol it speaks or the company behind it. */
  terms?: readonly string[];
  mark?: TileMark;
  /** For a provider without a brand of its own, the glyph that means it (glyphs.md), which the app draws. */
  glyph?: string;
  /** The name and kind are words rather than a brand, which the app says in its own language. */
  word?: boolean;
}

export interface Catalog<G extends string = string, I extends string = string> {
  /** The groups in the order a picker shows them. */
  groups: readonly G[];
  entries: readonly CatalogEntry<G, I>[];
}

/** What an app hands over to make a picker of a catalog. `M` is its kind of glyph. */
export interface CatalogPick<G extends string, I extends string, M> {
  /** The providers the app offers. An id the catalog does not know is left out, for the app to add its own tile. */
  ids: readonly I[];
  /** Each group's eyebrow in the app's language, or the eyebrow with the (i) that stands in its line. */
  titles: Readonly<Record<G, string | Omit<PickGroup, 'id'>>>;
  /** The app's glyph for a provider without a mark. */
  glyph: (entry: CatalogEntry<G, I>) => M;
  /** The page's language, which the tiles are sorted in. */
  lang: string | undefined;
  /** The app's own words for a tile, which win over the catalog's: a translated name, a hint. */
  own?: (entry: CatalogEntry<G, I>) => Partial<PickItem<M>> | undefined;
}

/**
 * The tiles and groups of a picker for the providers an app offers: group by
 * group in the catalog's order, alphabetical within a group, and only the
 * groups that keep a tile.
 */
export function catalogPicker<G extends string, I extends string, M>(
  catalog: Catalog<G, I>,
  pick: CatalogPick<G, I, M>,
): { items: PickItem<M>[]; groups: PickGroup[] } {
  const asked = new Set<string>(pick.ids);
  const rank = (g: G) => catalog.groups.indexOf(g);
  const items = catalog.entries
    .filter((e) => asked.has(e.id))
    .map((e) => {
      const item: PickItem<M> = { id: e.id, label: e.name, group: e.group, ...pick.own?.(e) };
      if (e.kind && item.sub === undefined) item.sub = e.kind;
      // A translated name still finds the provider by the name its own documentation uses.
      const terms = [...(item.label !== e.name ? [e.name] : []), ...(e.terms ?? []), ...(item.terms ?? [])];
      if (terms.length > 0) item.terms = terms;
      item.mark ??= e.mark ?? pick.glyph(e);
      return { item, group: e.group };
    })
    .sort((a, b) => rank(a.group) - rank(b.group) || a.item.label.localeCompare(b.item.label, pick.lang, { sensitivity: 'base' }))
    .map(({ item }) => item);
  const groups = catalog.groups
    .filter((g) => items.some((i) => i.group === g))
    .map((g) => {
      const title: string | Omit<PickGroup, 'id'> = pick.titles[g];
      return typeof title === 'string' ? { id: g, title } : { id: g, ...title };
    });
  return { items, groups };
}

/** The entry of a catalog with this id, for a row that draws the mark its tile wore. */
export function catalogEntry<G extends string, I extends string>(catalog: Catalog<G, I>, id: string): CatalogEntry<G, I> | undefined {
  return catalog.entries.find((e) => e.id === id);
}
