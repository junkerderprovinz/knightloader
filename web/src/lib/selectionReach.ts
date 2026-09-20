import { useMemo } from 'react';
import type { Task } from './api';

// Which selected rows the list is drawing and which it is not. A filter hides
// rows without deselecting them (see lib/listview.ts), so someone can hold
// eighteen rows with six on screen, and Del would take all eighteen.
//
// listview.ts's `visible` is not enough: a row inside a collapsed package
// passes the search and the filters but is drawn nowhere, and a folded package
// only shows as selected when all its rows are. The drawn set stays in the page
// that owns the grouping instead of going through listview's store, where a
// new identity every render would re-render all its subscribers.

export interface SelectionReach {
  /** The selected ids the list is drawing right now. */
  shown: string[];
  /** The selected ids it is not drawing, hidden by the search, a quick
   *  filter or a folded package. A removal takes these unseen. */
  hidden: string[];
}

/** selectionReach splits the selection, keeping its order. */
export function selectionReach(selected: ReadonlySet<string>, drawn: ReadonlySet<string>): SelectionReach {
  const shown: string[] = [];
  const hidden: string[] = [];
  for (const id of selected) (drawn.has(id) ? shown : hidden).push(id);
  return { shown, hidden };
}

/**
 * useDrawnRows is the set of rows the list card renders: the narrowed
 * grouping minus folded packages. Pass the narrowed groups, not the page's
 * `all`, which also holds the collector's rows. Memoised because callers use
 * it as a hook dependency.
 */
export function useDrawnRows(groups: [string, Task[]][], collapsed: ReadonlySet<string>): ReadonlySet<string> {
  return useMemo(() => {
    const out = new Set<string>();
    for (const [name, items] of groups) {
      if (collapsed.has(name)) continue;
      for (const x of items) out.add(x.id);
    }
    return out;
  }, [groups, collapsed]);
}
