// What the list on screen is showing, published so the shell's overview strip
// can offer Total / Visible / Selected that agree with it. The page publishes
// only which rows, by id; the strip computes the figures from its own task
// stream. Nothing is published when no list is mounted, so no stale "Visible"
// count outlives its page.

import { useEffect, useSyncExternalStore } from 'react';
import type { Task } from './api';

/** The rows a list is showing and the rows the user picked out of them. */
export interface ListView {
  /** In view order, after the list's search and quick filters. */
  visible: readonly string[];
  /** The selection, which is not a subset of `visible`: a filter hides rows
   *  without deselecting them. */
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
 * useListView is what the shell reads; null means no list is mounted. The
 * snapshot is the held object, since a fresh one per call would re-render
 * forever.
 */
export function useListView(): ListView | null {
  return useSyncExternalStore(
    subscribe,
    () => current,
    () => null,
  );
}

/**
 * useReportListView publishes a list page's filtered rows and selection.
 * Clearing on unmount is a separate effect, or every filter change would
 * publish null first and make the strip blink.
 */
export function useReportListView(visible: readonly Task[], selected: ReadonlySet<string>): void {
  useEffect(() => {
    publish({ visible: visible.map((x) => x.id), selected });
  }, [visible, selected]);
  useEffect(() => () => publish(null), []);
}
