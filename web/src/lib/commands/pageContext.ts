// The verbs the mounted list page offers the command registry: change the
// selection, remove rows, run the clean-up flow. lib/listview.ts reports only
// what is visible and selected, for the shell's overview strip; this is the
// write side. A page publishes only the fields it has, and useCommandContext
// fills the rest with no-ops.

import { useEffect, useSyncExternalStore } from 'react';
import type { CleanupState, Removal } from '../../components/ListToolbar';

export interface CommandPageContext {
  setSelection?: (next: Set<string>) => void;
  removal?: Removal;
  cleanup?: CleanupState;
  /** Opens the Collector's file picker, the same one AddLinksForm's folder
   *  badge opens, so a shortcut can reach it. */
  openFilePicker?: () => void;
  /** Toggles the Downloads page's search panel, the same call as its search
   *  badge. The Collector has no such panel. */
  toggleSearch?: () => void;
}

let current: CommandPageContext | null = null;
const listeners = new Set<() => void>();

function publish(next: CommandPageContext | null): void {
  current = next;
  for (const l of listeners) l();
}

function subscribe(l: () => void): () => void {
  listeners.add(l);
  return () => {
    listeners.delete(l);
  };
}

/** Read by useCommandContext (./types.ts). Null when no page has published one. */
export function useCommandPageContext(): CommandPageContext | null {
  return useSyncExternalStore(
    subscribe,
    () => current,
    () => null,
  );
}

/**
 * usePublishCommandPageContext publishes a list page's verbs. Pass the same
 * removal, cleanup and setSelection the page uses for its own UI. Clearing on
 * unmount is a separate effect, as in useReportListView, so a re-publish does
 * not blank the store first.
 */
export function usePublishCommandPageContext(ctx: CommandPageContext): void {
  useEffect(() => {
    publish(ctx);
  }, [ctx]);
  useEffect(() => () => publish(null), []);
}
