import { useMemo } from 'react';
import type { Task } from './api';

/**
 * Which of the selected rows the list is actually DRAWING, and which it is not.
 *
 * The question this answers is the one lib/listview.ts's own doc comment
 * already names as a hazard and then leaves standing: "The selection, which is
 * NOT a subset of `visible`: turning a filter on hides rows without deselecting
 * them, and a strip that resolved the selection against the visible set would
 * silently shrink it." That decision is right - a selection that quietly shrank
 * when somebody typed in the search box would be worse - but it leaves a person
 * holding eighteen rows while six are on screen, and nothing anywhere says so.
 * Then Del takes all eighteen.
 *
 * WHY THIS IS NOT `visible` FROM lib/listview.ts, which is the trap that decides
 * the whole file. listview.ts's `visible` means "survived the search and the
 * quick filters". A row inside a COLLAPSED package survives both and is drawn
 * nowhere: components/TaskList.tsx emits a package's task rows only when the
 * package is not folded. And a folded package gives no sign of holding a partial
 * selection either, because PackageRow paints the selected background only when
 * every one of its items is selected - so three of five selected inside a folded
 * folder looks exactly like nothing selected. A Shift-range across a folded
 * package selects the whole package in one step, which makes that state easy to
 * reach by accident. A count taken from `visible` would under-report precisely
 * the case the gesture creates most often.
 *
 * WHY IT IS NOT PUBLISHED THROUGH lib/listview.ts EITHER: that store replaces its
 * held object on every publish and its own note explains that the snapshot must
 * BE the held object, because a fresh one per call renders for ever. A third
 * argument that changed identity every render would fire that effect, and every
 * useSyncExternalStore subscriber under it, on every frame. The drawn set stays
 * in the page that owns the grouping.
 */

export interface SelectionReach {
  /** The selected ids the list is drawing right now. */
  shown: string[];
  /**
   * The selected ids it is not: hidden by the search, by a quick filter, or by
   * the fold on the package they sit in. These are the ones a removal takes
   * with it and nobody looked at.
   */
  hidden: string[];
}

/**
 * The split. Pure, and deliberately order-preserving on `selected` so a caller
 * that shows the ids anywhere shows them in the order the selection holds them.
 */
export function selectionReach(selected: ReadonlySet<string>, drawn: ReadonlySet<string>): SelectionReach {
  const shown: string[] = [];
  const hidden: string[] = [];
  for (const id of selected) (drawn.has(id) ? shown : hidden).push(id);
  return { shown, hidden };
}

/**
 * The drawn set, built from exactly what the list card renders from: the
 * post-narrowing grouping, minus everything inside a folded package.
 *
 * `groups` is already the narrowed list on both pages, so a row the search or a
 * quick filter dropped never reaches here. It must NOT be built from the page's
 * own `all`, which holds every task on the instance, collector rows included -
 * the collector's held and skipped rows would start counting as "selected but
 * not visible" on a page that never offered them.
 *
 * Memoised, and that is not tidiness: it becomes a hook dependency downstream,
 * and this repo has been bitten three times by a fresh collection identity in a
 * dependency list (see NO_NARROWING in lib/listNarrowing.ts, NO_COLLAPSED in
 * components/TaskList.tsx, NONE in lib/dialogmute.ts).
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
