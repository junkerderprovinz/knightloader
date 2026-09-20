// Everything narrowing a list (the search, the quick filters, the collector's
// facets), kept in lib/uistate.ts beside the column layout and sort order, so
// leaving the page and coming back keeps a list cut down to eight rows.
//
// One field for all three, so applying a saved view is one write and one
// render, without intermediate states reaching the overview strip.
//
// What comes back out of the document goes through sanitiseNarrowing(): it is
// shared between browsers, outlives the build that wrote it, and anything can
// PUT into it.
import { useCallback, useMemo } from 'react';
import { useUIState } from './uistate';
import { SEARCH_FIELDS, type SearchCategory, type SearchQuery } from './searchQuery';
import type { QuickFilterId } from '../components/ListToolbar';
import type { FacetSelection } from '../components/CollectorFacets';

/** Which list a stored narrowing belongs to, the same profile key the column
 *  and sort fields use. */
export type ListProfileKey = 'downloads' | 'collector';

/** Everything that narrows a list, as JSON can hold it: arrays, since a Set
 *  serialises as `{}`. The hook below rebuilds the Sets once per change. */
export interface Narrowing {
  search: SearchQuery;
  /** In the profile's own filter order, de-duplicated. See sanitiseNarrowing. */
  filters: QuickFilterId[];
  facets: { host: string[]; fileType: string[]; package: string[] };
}

/** One saved, named view. `id` is the identity, never the name. */
export interface SavedView {
  id: string;
  /** What the chip says; it can be renamed, so it is not the id. */
  name: string;
  state: Narrowing;
}

const NOTHING: Narrowing = {
  search: { text: '', category: 'any' },
  filters: [],
  facets: { host: [], fileType: [], package: [] },
};

/**
 * The stable "nothing is narrowing this list" value. A module constant, since
 * an inline fallback to useUIState would be a new object each render and
 * re-render forever. Frozen, so editing it in place fails loudly.
 */
export const NO_NARROWING: Narrowing = Object.freeze(NOTHING);

/** The same rule for the saved-view list. Never mutated in place. */
export const NO_VIEWS: SavedView[] = [];

/** True when this value is one of the categories the search parser knows. */
function asCategory(raw: unknown): SearchCategory {
  if (raw === 'any') return 'any';
  // An unknown category would make searchQuery.ts's fieldOf return undefined
  // and crash the filter on every row.
  return (SEARCH_FIELDS as readonly string[]).includes(raw as string) ? (raw as SearchCategory) : 'any';
}

/** The stored strings of one facet dimension, de-duplicated and ordered. */
function facetValues(raw: unknown): string[] {
  if (!Array.isArray(raw)) return [];
  const out = new Set<string>();
  for (const v of raw) if (typeof v === 'string') out.add(v);
  // Sorted, so equal selections compare equal whatever order they were ticked in.
  return [...out].sort();
}

/**
 * sanitiseNarrowing reads a stored document back into something the page can
 * trust. `keepFacets` is false for the download list, which has no facet
 * sidebar and so could never show or clear stored facets.
 */
export function sanitiseNarrowing(
  raw: unknown,
  allowed: readonly QuickFilterId[],
  keepFacets = true,
): Narrowing {
  if (raw === null || typeof raw !== 'object') return NO_NARROWING;
  const doc = raw as { search?: unknown; filters?: unknown; facets?: unknown };
  const search = (doc.search ?? {}) as { text?: unknown; category?: unknown };

  // An unknown filter id would empty the list with no chip and no reset shown
  // to undo it. Walking `allowed` drops unknown ids and duplicates and keeps
  // the chip order stable, which sameNarrowing relies on.
  const storedFilters = Array.isArray(doc.filters) ? (doc.filters as unknown[]) : [];
  const filters = allowed.filter((id) => storedFilters.includes(id));

  const facets = doc.facets as { host?: unknown; fileType?: unknown; package?: unknown } | null | undefined;
  return {
    search: {
      text: typeof search.text === 'string' ? search.text : '',
      category: asCategory(search.category),
    },
    filters,
    facets: keepFacets
      ? {
          host: facetValues(facets?.host),
          fileType: facetValues(facets?.fileType),
          package: facetValues(facets?.package),
        }
      : { host: [], fileType: [], package: [] },
  };
}

function sameList(a: readonly string[], b: readonly string[]): boolean {
  return a.length === b.length && a.every((x, i) => x === b[i]);
}

/**
 * sameNarrowing reports whether two narrowings select the same rows, which
 * decides whether a saved view's chip is lit. The text is compared trimmed,
 * since the parser splits on whitespace, and the category only when there is
 * text for it to apply to.
 */
export function sameNarrowing(a: Narrowing, b: Narrowing): boolean {
  const ta = a.search.text.trim();
  const tb = b.search.text.trim();
  if (ta !== tb) return false;
  if (ta !== '' && a.search.category !== b.search.category) return false;
  return (
    sameList(a.filters, b.filters) &&
    sameList(a.facets.host, b.facets.host) &&
    sameList(a.facets.fileType, b.facets.fileType) &&
    sameList(a.facets.package, b.facets.package)
  );
}

/** How many facet values are ticked, across all three dimensions. */
export function facetCount(n: Narrowing): number {
  return n.facets.host.length + n.facets.fileType.length + n.facets.package.length;
}

/** The Sets components/CollectorFacets.tsx wants, out of the arrays JSON holds. */
export function toFacetSelection(n: Narrowing): FacetSelection {
  return {
    host: new Set(n.facets.host),
    fileType: new Set(n.facets.fileType),
    package: new Set(n.facets.package),
  };
}

/** And back again, ordered the way facetValues orders everything else. */
export function fromFacetSelection(f: FacetSelection): Narrowing['facets'] {
  return {
    host: [...f.host].sort(),
    fileType: [...f.fileType].sort(),
    package: [...f.package].sort(),
  };
}

/** What useListNarrowing hands a page. */
export interface ListNarrowing {
  /** The stored shape, for saving a view out of it and comparing chips against it. */
  narrowing: Narrowing;
  search: SearchQuery;
  filters: Set<QuickFilterId>;
  facets: FacetSelection;
  setSearch: (q: SearchQuery) => void;
  toggleFilter: (id: QuickFilterId) => void;
  clearFilters: () => void;
  setFacets: (f: FacetSelection) => void;
  /** Applying a saved view: one write, one render. */
  apply: (n: Narrowing) => void;
  /** The search, the quick filters and the facets, all back to nothing. */
  clearAll: () => void;
  /** Anything at all is narrowing this list right now. */
  active: boolean;
}

/**
 * useListNarrowing owns a list's narrowing. The page calls it once and passes
 * the result down: several readers are fine, but two writers in one commit
 * would lose the first change.
 */
export function useListNarrowing(profile: ListProfileKey, allowed: readonly QuickFilterId[]): ListNarrowing {
  // Not `list.views.${profile}`, where the saved views live. Typed unknown,
  // since the document may hold anything; sanitiseNarrowing makes it a
  // Narrowing.
  const [stored, setStored] = useUIState<unknown>(`list.narrowing.${profile}`, NO_NARROWING);
  const keepFacets = profile === 'collector';

  const narrowing = useMemo(
    () => sanitiseNarrowing(stored, allowed, keepFacets),
    [stored, allowed, keepFacets],
  );
  // Built once per change rather than per row in the filter pass.
  const filters = useMemo(() => new Set(narrowing.filters), [narrowing.filters]);
  const facets = useMemo(() => toFacetSelection(narrowing), [narrowing]);

  const setSearch = useCallback(
    (q: SearchQuery) => setStored({ ...narrowing, search: { text: q.text, category: q.category } }),
    [narrowing, setStored],
  );
  const toggleFilter = useCallback(
    (id: QuickFilterId) =>
      setStored({
        ...narrowing,
        filters: narrowing.filters.includes(id)
          ? narrowing.filters.filter((x) => x !== id)
          : allowed.filter((x) => x === id || narrowing.filters.includes(x)),
      }),
    [narrowing, allowed, setStored],
  );
  const clearFilters = useCallback(
    () => setStored({ ...narrowing, filters: [] }),
    [narrowing, setStored],
  );
  const setFacets = useCallback(
    (f: FacetSelection) => setStored({ ...narrowing, facets: fromFacetSelection(f) }),
    [narrowing, setStored],
  );
  // Sanitised on the way in too, since a saved view may name a filter this
  // build lacks.
  const apply = useCallback(
    (n: Narrowing) => setStored(sanitiseNarrowing(n, allowed, keepFacets)),
    [allowed, keepFacets, setStored],
  );
  const clearAll = useCallback(() => setStored(NO_NARROWING), [setStored]);

  const active =
    narrowing.search.text.trim() !== '' || narrowing.filters.length > 0 || facetCount(narrowing) > 0;

  return {
    narrowing,
    search: narrowing.search,
    filters,
    facets,
    setSearch,
    toggleFilter,
    clearFilters,
    setFacets,
    apply,
    clearAll,
    active,
  };
}
