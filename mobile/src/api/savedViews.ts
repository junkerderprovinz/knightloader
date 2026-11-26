// A list's saved views: a search and a set of filters kept under a name, the
// ones the web shows as chips over the same list. They live in the web's
// interface state, so this reads and writes them in the web's shape and by the
// web's rules (web/src/lib/savedViews.ts and web/src/lib/listNarrowing.ts),
// and a view saved on either side lights its chip on the other.
//
// It imports nothing at run time, so check-saved-views.mjs can load it in node.

import type { Facets, Narrowing, QuickFilterId, SearchCategory } from './narrowing';

const CATEGORIES: SearchCategory[] = ['any', 'name', 'host', 'package', 'comment', 'url'];

// An unknown category would aim the search at no field at all.
const categoryOf = (raw: unknown): SearchCategory =>
  CATEGORIES.includes(raw as SearchCategory) ? (raw as SearchCategory) : 'any';

/** A narrowing as the web stores it, arrays where the app keeps sets. */
export interface StoredNarrowing {
  search: { text: string; category: string };
  filters: QuickFilterId[];
  facets: { host: string[]; fileType: string[]; package: string[] };
}

export interface SavedView {
  id: string;
  name: string;
  state: StoredNarrowing;
}

/** Both lists' views, as GET /api/uistate/views answers. */
export interface SavedViewLists {
  downloads: SavedView[];
  collector: SavedView[];
}

/** The web's bounds, which the instance holds a member to as well. */
export const MAX_VIEWS = 12;
export const MAX_VIEW_BYTES = 4096;
export const MAX_VIEW_NAME = 60;

const sorted = (s: ReadonlySet<string>) => [...s].sort();

/** toStored is the app's narrowing in the web's shape, filters in the list's own order. */
export function toStored(n: Narrowing, allowed: readonly QuickFilterId[]): StoredNarrowing {
  return {
    search: { text: n.search, category: n.category },
    filters: allowed.filter((id) => n.filters.has(id)),
    facets: { host: sorted(n.facets.host), fileType: sorted(n.facets.fileType), package: sorted(n.facets.package) },
  };
}

function strings(raw: unknown): string[] {
  return Array.isArray(raw) ? [...new Set(raw.filter((v): v is string => typeof v === 'string'))].sort() : [];
}

/**
 * fromStored reads a stored narrowing back as the web's sanitiseNarrowing
 * does: an unknown filter is dropped rather than emptying the list with no
 * chip to undo it, the old "held" filter is the disabled one, and the download
 * list keeps no facets, which it has no chips for.
 */
export function fromStored(raw: unknown, allowed: readonly QuickFilterId[], keepFacets: boolean): Narrowing {
  const doc = (raw !== null && typeof raw === 'object' ? raw : {}) as {
    search?: { text?: unknown; category?: unknown };
    filters?: unknown;
    facets?: { host?: unknown; fileType?: unknown; package?: unknown } | null;
  };
  const stored = (Array.isArray(doc.filters) ? doc.filters : []).map((id) => (id === 'held' ? 'disabled' : id));
  const f = keepFacets ? doc.facets : null;
  const facets: Facets = {
    host: new Set(strings(f?.host)),
    fileType: new Set(strings(f?.fileType)),
    package: new Set(strings(f?.package)),
  };
  return {
    search: typeof doc.search?.text === 'string' ? doc.search.text : '',
    category: categoryOf(doc.search?.category),
    filters: new Set(allowed.filter((id) => stored.includes(id))),
    facets,
  };
}

/** sanitiseViews keeps the stored views that have an id of their own and a name. */
export function sanitiseViews(raw: unknown): SavedView[] {
  if (!Array.isArray(raw)) return [];
  const out: SavedView[] = [];
  const seen = new Set<string>();
  for (const item of raw) {
    const v = (item ?? {}) as { id?: unknown; name?: unknown; state?: unknown };
    if (typeof v.id !== 'string' || !v.id || seen.has(v.id) || typeof v.name !== 'string') continue;
    seen.add(v.id);
    out.push({ id: v.id, name: v.name, state: v.state as StoredNarrowing });
    if (out.length >= MAX_VIEWS) break;
  }
  return out;
}

const same = (a: readonly string[], b: readonly string[]) => a.length === b.length && a.every((x, i) => x === b[i]);

/**
 * sameNarrowing reports whether two narrowings pick the same rows, which is
 * what lights a view's chip. The text is compared trimmed, and the category
 * only where there is text for it to aim.
 */
export function sameNarrowing(a: Narrowing, b: Narrowing): boolean {
  const ta = a.search.trim();
  const tb = b.search.trim();
  if (ta !== tb || (ta !== '' && a.category !== b.category)) return false;
  return (
    same(sorted(a.filters), sorted(b.filters)) &&
    same(sorted(a.facets.host), sorted(b.facets.host)) &&
    same(sorted(a.facets.fileType), sorted(b.facets.fileType)) &&
    same(sorted(a.facets.package), sorted(b.facets.package))
  );
}

/** matching is the view the list equals right now, or null. */
export function matching(
  views: SavedView[],
  now: Narrowing,
  allowed: readonly QuickFilterId[],
  keepFacets: boolean,
): SavedView | null {
  return views.find((v) => sameNarrowing(fromStored(v.state, allowed, keepFacets), now)) ?? null;
}

const cleanName = (name: string) => name.trim().slice(0, MAX_VIEW_NAME);

/** taken is whether another view already has this name, compared without case. */
export function taken(views: SavedView[], name: string, except?: string): boolean {
  const clean = cleanName(name).toLowerCase();
  return clean !== '' && views.some((v) => v.id !== except && v.name.toLowerCase() === clean);
}

export type SaveResult = { views: SavedView[] } | { refused: 'full' | 'tooBig' };

/**
 * saveView keeps state under a name. A taken name overwrites that view rather
 * than making two chips that read alike.
 */
export function saveView(views: SavedView[], name: string, state: StoredNarrowing, newId: () => string): SaveResult {
  const clean = cleanName(name);
  if (!clean) return { views };
  const at = views.findIndex((v) => v.name.toLowerCase() === clean.toLowerCase());
  if (at < 0 && views.length >= MAX_VIEWS) return { refused: 'full' };
  const view: SavedView = { id: at >= 0 ? views[at].id : newId(), name: clean, state };
  if (JSON.stringify(view).length > MAX_VIEW_BYTES) return { refused: 'tooBig' };
  return { views: at >= 0 ? views.map((v, i) => (i === at ? view : v)) : [...views, view] };
}

export function renameView(views: SavedView[], id: string, name: string): SavedView[] {
  const clean = cleanName(name);
  return clean ? views.map((v) => (v.id === id ? { ...v, name: clean } : v)) : views;
}

export function removeView(views: SavedView[], id: string): SavedView[] {
  return views.filter((v) => v.id !== id);
}
