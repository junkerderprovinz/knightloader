// Everything that is currently narrowing one list, kept in the document the
// rest of the interface's state already lives in.
//
// The search text, the quick filters and the collector's facets were three
// useState calls spread over two pages. Walking to Settings and back put the
// whole list straight back, which is mildly annoying with forty links and
// genuinely expensive with four thousand: the eight rows somebody had cut the
// list down to were gone, and the way back was to type the query again. The
// column layout and the sort order already survive that walk through
// lib/uistate.ts (`list.columns.${profile}`, `list.sort.${profile}`), so this
// is a third field in a document that already exists: no new route, no new
// table, no migration.
//
// ONE field for all three, and that is the load-bearing decision here.
// Applying a saved view is then a single write and a single render. Three
// fields would be three writes, and the list would render through two mixed
// states that were never true, each of which is published to the shell's
// overview strip (lib/listview.ts), so the header's "Visible" figure would
// count through two numbers nobody asked about on the way to the right one.
//
// Everything read back out of the document goes through sanitiseNarrowing()
// first. That is not defensive padding. The document is server-held, is shared
// between browsers, outlives the build that wrote it, and anything that can PUT
// /api/uistate can put anything at all in it. Two of the ways a raw value gets
// out of hand take the whole page down or strand it. See that function's own
// traps.
import { useCallback, useMemo } from 'react';
import { useUIState } from './uistate';
import { SEARCH_FIELDS, type SearchCategory, type SearchQuery } from './searchQuery';
import type { QuickFilterId } from '../components/ListToolbar';
import type { FacetSelection } from '../components/CollectorFacets';

/**
 * Which list a stored narrowing belongs to: the same profile key
 * `list.columns.${profile}` and `list.sort.${profile}` already use.
 *
 * Spelled out here rather than imported from components/columns.tsx so that a
 * module about stored state does not have to reach into the table's own
 * definitions for one string union. The two are deliberately the same words.
 */
export type ListProfileKey = 'downloads' | 'collector';

/**
 * Everything that narrows a list, in a shape JSON can hold.
 *
 * Sets become arrays, because a Set survives neither JSON.stringify on the way
 * out (it serialises as `{}`) nor JSON.parse on the way back. The Sets the
 * existing filter code wants are rebuilt from these arrays by the hook below,
 * once per change, the same way useCollapsedPackages rebuilds its own
 * (components/TaskList.tsx).
 */
export interface Narrowing {
  search: SearchQuery;
  /** In the profile's own filter order, de-duplicated. See sanitiseNarrowing. */
  filters: QuickFilterId[];
  facets: { host: string[]; fileType: string[]; package: string[] };
}

/** One saved, named view. `id` is the identity, never the name. */
export interface SavedView {
  id: string;
  /** What the chip says. Renamed freely, which is exactly why it is not the id. */
  name: string;
  state: Narrowing;
}

const NOTHING: Narrowing = {
  search: { text: '', category: 'any' },
  filters: [],
  facets: { host: [], fileType: [], package: [] },
};

/**
 * The stable "nothing is narrowing this list" value.
 *
 * TRAP: it has to be a module-level constant. useUIState leaves its `fallback`
 * out of the effect's dependencies on purpose (lib/uistate.ts), so an inline
 * `{ search: EMPTY_SEARCH, filters: [], … }` would be a new object on every
 * render, the effect would resubscribe on every render, and the page would
 * re-render for ever. components/TaskList.tsx's NO_COLLAPSED and
 * lib/dialogmute.ts's NONE both carry the same warning. Frozen so that a caller
 * that ever tried to edit it in place fails loudly instead of poisoning every
 * list at once.
 */
export const NO_NARROWING: Narrowing = Object.freeze(NOTHING);

/** The same rule for the saved-view list. Never mutated in place. */
export const NO_VIEWS: SavedView[] = [];

/** True when this value is one of the categories the search parser knows. */
function asCategory(raw: unknown): SearchCategory {
  if (raw === 'any') return 'any';
  // TRAP: lib/searchQuery.ts's fieldOf is a switch over the non-'any'
  // categories with no default branch, so an unknown category makes it return
  // undefined, and `holds` then calls .toLowerCase() on it once per row. That
  // is a TypeError raised inside the filter memo, which takes the whole page
  // down rather than one row. It could not happen while the only writer was
  // SearchField's own <select>; the moment the value comes back out of a
  // server-held document it can be any string at all.
  return (SEARCH_FIELDS as readonly string[]).includes(raw as string) ? (raw as SearchCategory) : 'any';
}

/** The stored strings of one facet dimension, de-duplicated and ordered. */
function facetValues(raw: unknown): string[] {
  if (!Array.isArray(raw)) return [];
  const out = new Set<string>();
  for (const v of raw) if (typeof v === 'string') out.add(v);
  // Sorted so that two narrowings that mean the same thing compare equal
  // however they were built: a facet ticked host-then-package must light the
  // same chip as one ticked package-then-host.
  return [...out].sort();
}

/**
 * sanitiseNarrowing reads a stored document back into something the page can
 * trust.
 *
 * `keepFacets` is false for the download list, which has no facet sidebar and
 * never calls matchesFacets: dropping them here means a document written by
 * some future build cannot leave values in that field that nothing on the page
 * can ever see or clear.
 */
export function sanitiseNarrowing(
  raw: unknown,
  allowed: readonly QuickFilterId[],
  keepFacets = true,
): Narrowing {
  if (raw === null || typeof raw !== 'object') return NO_NARROWING;
  const doc = raw as { search?: unknown; filters?: unknown; facets?: unknown };
  const search = (doc.search ?? {}) as { text?: unknown; category?: unknown };

  // TRAP: an unknown quick-filter id does not narrow the list, it EMPTIES it,
  // with no way back. matchesQuickFilters (components/ListToolbar.tsx) answers
  // false for every row once the active set is non-empty and nothing in it
  // matches, offeredQuickFilters only ever draws chips for the page's own
  // filter list, and the "Show everything" reset lives inside that chip strip's
  // `after` slot, which is not rendered when there are no chips. An id that
  // was renamed in an upgrade, or a download-list id that leaked into the
  // collector's document, would therefore leave an empty list, no chips and no
  // reset, recoverable only by editing the database.
  //
  // Walking `allowed` rather than the stored array does four jobs in one pass:
  // unknown ids are dropped, duplicates collapse, the result is in the
  // profile's own chip order, and that order is stable, which is what lets
  // sameNarrowing below compare two of these element by element.
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
 * sameNarrowing is "these two narrow the list to the same rows", which is what
 * decides whether a saved view's chip is lit.
 *
 * The text is compared trimmed because the parser tokenises on whitespace, so a
 * trailing space is genuinely the same question. The category is compared only
 * while there is text to aim: with an empty box the picker asks nothing, and
 * un-lighting a chip because somebody nudged a dropdown that changed no row
 * would be the chip telling a small lie.
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
 * useListNarrowing is the one owner of a list's narrowing.
 *
 * The PAGE calls this, once, and passes what comes out down as props. Two
 * READERS of the same field would be fine (the store notifies every subscriber
 * on write, which is exactly why useCollapsedPackages is deliberately read
 * twice), but two writers in one commit are not: the second `set` simply wins
 * and the first change is gone.
 */
export function useListNarrowing(profile: ListProfileKey, allowed: readonly QuickFilterId[]): ListNarrowing {
  // TRAP: `list.narrowing.${profile}`, not `list.view.${profile}`. The saved
  // views next door live under `list.views.${profile}`, and two field names one
  // letter apart is a typo that reads the wrong field, finds nothing, returns
  // the fallback and reports no error anywhere, which reaches the user as "my
  // views are gone".
  //
  // Typed `unknown` on the way out on purpose: what the document holds is
  // whatever some build or some browser last put there, and pretending it is
  // already a Narrowing is how the traps in sanitiseNarrowing get past the
  // compiler.
  const [stored, setStored] = useUIState<unknown>(`list.narrowing.${profile}`, NO_NARROWING);
  const keepFacets = profile === 'collector';

  const narrowing = useMemo(
    () => sanitiseNarrowing(stored, allowed, keepFacets),
    [stored, allowed, keepFacets],
  );
  // The same array-to-Set memo useCollapsedPackages uses: matchesQuickFilters
  // and matchesFacets are called once per row per repaint, and rebuilding these
  // inside the filter pass would rebuild them per row.
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
  // Sanitised on the way in as well as on the way out: a view saved by a build
  // that offered a filter this one does not must not put that filter back.
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
