// What the list on screen is actually showing, published so that the shell above
// it can agree with it.
//
// The overview strip in the shell bar offers Total / Visible / Selected, and
// "visible" is the only one of the three it cannot work out for itself.
// Object.values(tasks) is not visible: the download page holds a search query, a
// set of quick filters and a selection in its own state, and a strip that summed
// the whole stream would print a byte total the list directly underneath it
// contradicts. Which is worse than showing nothing, because it looks right.
//
// So the page says WHICH ROWS, by id, and nothing else. The figures are computed
// once, in the strip, from the strip's own task stream — two components summing
// two copies of the same stream is how the header and the list end up a tick
// apart on a number people read as one.
//
// Nothing is published when no list is mounted, and the strip then offers Total
// alone: on Settings or Accounts there is no list to be visible in, and a stale
// "Visible: 8" from a page somebody left is exactly the lie this file exists to
// prevent.

import { useEffect, useSyncExternalStore } from 'react';
import type { Task } from './api';

/** The rows a list is showing and the rows the user picked out of them. */
export interface ListView {
  /** In view order, after the list's search and quick filters. */
  visible: readonly string[];
  /**
   * The selection, which is NOT a subset of `visible`: turning a filter on
   * hides rows without deselecting them, and a strip that resolved the
   * selection against the visible set would silently shrink it.
   */
  selected: ReadonlySet<string>;
}

let current: ListView | null = null;
const listeners = new Set<() => void>();

function publish(next: ListView | null): void {
  current = next;
  for (const l of listeners) l();
}

function subscribe(l: () => void): () => void {
  listeners.add(l);
  return () => {
    listeners.delete(l);
  };
}

/**
 * useListView is what the shell reads. Null means no list is mounted.
 *
 * The snapshot is the held object itself rather than something built in the
 * getter, because useSyncExternalStore compares by reference and a fresh object
 * per call renders for ever.
 */
export function useListView(): ListView | null {
  return useSyncExternalStore(
    subscribe,
    () => current,
    () => null,
  );
}

/**
 * useReportListView is the one line a list page adds. `visible` is the filtered,
 * sorted array the page already renders; `selected` is the id set it already
 * holds.
 *
 * The clear is its own effect with no dependencies on purpose. Folded into the
 * first one, its cleanup would fire on every change of the filtered list —
 * publishing null and then the new value in the same commit, so the strip blinks
 * back to "no list" between two keystrokes in the search box.
 */
export function useReportListView(visible: readonly Task[], selected: ReadonlySet<string>): void {
  useEffect(() => {
    publish({ visible: visible.map((x) => x.id), selected });
  }, [visible, selected]);
  useEffect(() => () => publish(null), []);
}
