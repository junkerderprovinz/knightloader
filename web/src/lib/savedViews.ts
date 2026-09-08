// The named views a list keeps: "the search and the filters I had set, under a
// name I chose".
//
// Nothing like this existed anywhere in the app, and its absence is recorded in
// JD's own words in docs/jd-feature-census.md: a download view is React
// component state there, so it is gone on reload. Now that a narrowing survives
// the page (lib/listNarrowing.ts), naming one costs a chip and gives back the
// three or four questions somebody asks of a long list over and over.
//
// One array field per profile in the same shared document, not a field per
// view. lib/dialogmute.ts already argues that case for muted dialogs and the
// argument is identical: a field per item is how this file would have to be
// edited for the thirteenth view.
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

/**
 * As many named views as one list keeps.
 *
 * It bounds two things at once: the chip row, which stops being a row anybody
 * can read somewhere around a dozen, and the shared 256 KiB document.
 */
export const MAX_VIEWS = 12;

/**
 * As much JSON as one view may weigh.
 *
 * TRAP: saved views are the first thing in this document whose size is decided
 * by how many values somebody ticks. A paste of two hundred links from eighty
 * hosts, all ticked in the facet sidebar, is one view carrying eighty hostnames
 * and every package name beside them. Cross the document's own cap and
 * lib/uistate.ts sets `refused`, stops writing entirely and says so in a single
 * console.warn, at which point the column widths, the folded packages, the
 * settings tab order and the muted dialogs all quietly stop being remembered
 * too, with nothing on screen to say why. Refusing an oversized view at save
 * time, in a window that explains itself, is the only version of this the user
 * ever finds out about.
 */
export const MAX_VIEW_BYTES = 4096;

/** As long a name as the chip row can carry without becoming a paragraph. */
export const MAX_VIEW_NAME = 60;

/**
 * newViewId is the identity a view keeps for life.
 *
 * TRAP: crypto.randomUUID exists only in a secure context, and a self-hosted
 * download manager is reached over plain http on a home network as often as
 * not, and calling it there is a TypeError thrown out of the save button. So it
 * is used when it is there and getRandomValues, which has no such restriction,
 * stands in when it is not. The value is never shown, never sent anywhere and
 * only ever compared for equality, so all it has to be is different from the
 * other eleven.
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
 * sanitiseViews reads the stored array back.
 *
 * Each view's own state goes through sanitiseNarrowing for the same reasons the
 * live narrowing does, and for one more: `matching` below compares the live
 * narrowing, which is always sanitised, against these. A view left holding a
 * filter id this build no longer offers would otherwise never compare equal to
 * anything, so applying it would light no chip: the chip would be telling a lie
 * about the click that had just happened.
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

/**
 * useSavedViews is the named views of one list.
 *
 * `allowed` is the profile's own quick-filter list, the same one
 * useListNarrowing takes, and it is here for sanitiseViews above rather than
 * for anything this hook does with it directly.
 */
export function useSavedViews(profile: ListProfileKey, allowed: readonly QuickFilterId[]): SavedViews {
  const [stored, setStored] = useUIState<unknown>(`list.views.${profile}`, NO_VIEWS);
  const keepFacets = profile === 'collector';
  const views = useMemo(() => sanitiseViews(stored, allowed, keepFacets), [stored, allowed, keepFacets]);

  /**
   * TRAP: lib/uistate.ts sends the WHOLE document on every write and never
   * reads back over the local copy, so a second tab that loaded before this
   * write and then drags a column edge writes a document with no such view in
   * it, and the view is gone from the server. Losing a column width that way is
   * annoying; losing something somebody typed a name for is data loss. Flushing
   * immediately instead of letting the 600 ms debounce run narrows the window
   * as far as this side can narrow it. Closing it entirely would mean a bucket
   * of its own (internal/app/app_feeds.go sets that precedent, and gives this
   * exact reason for it) or teaching the store to merge, and neither is worth
   * doing before anybody has two tabs open on the same list.
   */
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
      // A blank name never arrives here: the dialog's own confirm button is
      // disabled until there is something in the box. Treated as "nothing to
      // do" rather than as a refusal, because there is no refusal to explain:
      // nothing was asked for.
      if (!clean) return { ok: true };
      // A name that is already taken overwrites that view rather than making a
      // second chip with the same label, which nobody could tell apart. The
      // dialog says so before it happens: its primary button changes.
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

  // Computed, never stored. An "active view id" kept in the document would
  // start lying the moment somebody nudged one filter, and with at most twelve
  // views this comparison costs nothing worth measuring.
  const matching = useCallback(
    (now: Narrowing) => views.find((v) => sameNarrowing(v.state, now))?.id ?? null,
    [views],
  );

  return { views, save, rename, remove, matching };
}
