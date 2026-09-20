// A list's named views: a search and set of filters saved under a name, shown
// as a chip. One array field per list in the shared UI state, not a field per
// view.
import { useCallback, useMemo } from 'react';
import { flushUIState, useUIState } from './uistate';
import {
  NO_VIEWS,
  sameNarrowing,
  sanitiseNarrowing,
  type ListProfileKey,
  type Narrowing,
  type SavedView,
} from './listNarrowing';
import type { QuickFilterId } from '../components/ListToolbar';

/** How many views one list keeps; it bounds the chip row and the shared
 *  document. */
export const MAX_VIEWS = 12;

/**
 * The largest view, in JSON bytes. A view's size grows with every ticked
 * facet, and a UI state document over its cap stops saving anything, column
 * widths included. An oversized view is refused at save, where the dialog can
 * explain it.
 */
export const MAX_VIEW_BYTES = 4096;

/** As long a name as the chip row can carry without becoming a paragraph. */
export const MAX_VIEW_NAME = 60;

/**
 * newViewId is the identity a view keeps for life. crypto.randomUUID only
 * exists in a secure context, so plain-HTTP installs use getRandomValues. The
 * id is only compared for equality.
 */
function newViewId(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') return crypto.randomUUID();
  const bytes = new Uint8Array(16);
  crypto.getRandomValues(bytes);
  return [...bytes].map((b) => b.toString(16).padStart(2, '0')).join('');
}

/** Why a view could not be saved. */
export type SaveRefusal = 'full' | 'tooBig';
export type SaveResult = { ok: true } | { ok: false; reason: SaveRefusal };

export interface SavedViews {
  views: SavedView[];
  save: (name: string, state: Narrowing) => SaveResult;
  rename: (id: string, name: string) => void;
  remove: (id: string) => void;
  /** The id of the view the list currently equals, or null. What lights a chip. */
  matching: (now: Narrowing) => string | null;
}

/**
 * sanitiseViews reads the stored array back. Each view's state is sanitised
 * like the live narrowing, so a view with a filter this build lacks still
 * compares equal after it is applied and lights its chip.
 */
function sanitiseViews(raw: unknown, allowed: readonly QuickFilterId[], keepFacets: boolean): SavedView[] {
  if (!Array.isArray(raw)) return NO_VIEWS;
  const out: SavedView[] = [];
  const seen = new Set<string>();
  for (const item of raw) {
    if (item === null || typeof item !== 'object') continue;
    const v = item as { id?: unknown; name?: unknown; state?: unknown };
    if (typeof v.id !== 'string' || !v.id || seen.has(v.id)) continue;
    if (typeof v.name !== 'string') continue;
    seen.add(v.id);
    out.push({ id: v.id, name: v.name, state: sanitiseNarrowing(v.state, allowed, keepFacets) });
    if (out.length >= MAX_VIEWS) break;
  }
  return out;
}

/** useSavedViews is the named views of one list. `allowed` is the list's
 *  quick filters, as passed to useListNarrowing. */
export function useSavedViews(profile: ListProfileKey, allowed: readonly QuickFilterId[]): SavedViews {
  const [stored, setStored] = useUIState<unknown>(`list.views.${profile}`, NO_VIEWS);
  const keepFacets = profile === 'collector';
  const views = useMemo(() => sanitiseViews(stored, allowed, keepFacets), [stored, allowed, keepFacets]);

  // uistate writes the whole document, so another tab's next write can drop a
  // new view. Flushing at once instead of after the debounce narrows that
  // window; closing it would need a bucket of its own or a merging store.
  const write = useCallback(
    (next: SavedView[]) => {
      setStored(next);
      void flushUIState();
    },
    [setStored],
  );

  const save = useCallback(
    (name: string, state: Narrowing): SaveResult => {
      const clean = name.trim().slice(0, MAX_VIEW_NAME);
      // The dialog disables saving without a name, so this asks for nothing.
      if (!clean) return { ok: true };
      // A taken name overwrites that view instead of making two identical
      // chips; the dialog's button says so beforehand.
      const at = views.findIndex((v) => v.name.toLowerCase() === clean.toLowerCase());
      if (at < 0 && views.length >= MAX_VIEWS) return { ok: false, reason: 'full' };
      const view: SavedView = { id: at >= 0 ? views[at].id : newViewId(), name: clean, state };
      if (JSON.stringify(view).length > MAX_VIEW_BYTES) return { ok: false, reason: 'tooBig' };
      write(at >= 0 ? views.map((v, i) => (i === at ? view : v)) : [...views, view]);
      return { ok: true };
    },
    [views, write],
  );

  const rename = useCallback(
    (id: string, name: string) => {
      const clean = name.trim().slice(0, MAX_VIEW_NAME);
      if (!clean) return;
      write(views.map((v) => (v.id === id ? { ...v, name: clean } : v)));
    },
    [views, write],
  );

  const remove = useCallback((id: string) => write(views.filter((v) => v.id !== id)), [views, write]);

  // Computed rather than stored, so it cannot go stale when a filter changes.
  const matching = useCallback(
    (now: Narrowing) => views.find((v) => sameNarrowing(v.state, now))?.id ?? null,
    [views],
  );

  return { views, save, rename, remove, matching };
}
