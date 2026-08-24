// What the currently-mounted list page hands to the command registry beyond
// what lib/listview.ts already reports (which rows, which are selected):
// the actual verbs a downloads/collector command needs to run for real —
// change the selection, remove rows, run the clean-up flow.
//
// Same publish/subscribe shape as lib/listview.ts (one external store, read
// with useSyncExternalStore), kept as its own file rather than folded into
// that one: listview.ts's job is read-only visibility reporting for the
// shell's overview strip (Counters.tsx, QuickSettings.tsx already consume
// it for exactly that and nothing else), while this is specifically the
// command surface's own bridge — a page that needs one and not the other
// only has to publish one, and Counters/QuickSettings never have to know
// this file exists.
//
// Every field is optional: a page publishes only what it actually has
// (Collector.tsx has no useCleanup() instance today and commands/
// collector.ts never asks for one), and useCommandContext (./types.ts)
// substitutes a harmless no-op for whatever a page did not publish — so a
// command visible only on a surface with no page mounted yet (there is
// none today, but nothing here assumes there never will be) still gets a
// CommandContext whose removeSelected/cleanup/setSelection are safe to call.

import { useEffect, useSyncExternalStore } from 'react';
import type { CleanupState, Removal } from '../../components/ListToolbar';

export interface CommandPageContext {
  setSelection?: (next: Set<string>) => void;
  removal?: Removal;
  cleanup?: CleanupState;
  /** Opens the Collector's own file picker (FileDrop.tsx's ref handle) - the
   *  same trigger AddLinksForm's own folder-icon badge calls. Published so a
   *  keyboard shortcut can reach it too (jdp, 2026-08-24: "im sammler fehlt
   *  mir der tastenkürzel um eine datei zu öffnen"). */
  openFilePicker?: () => void;
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
 * usePublishCommandPageContext is the one call a downloads/collector-style
 * page adds — pass the same `removal`/`cleanup` instances and `setSelection`
 * setter the page already holds for its own UI, never a second copy built
 * just for this.
 *
 * The clear-on-unmount effect is deliberately its own, with no dependency on
 * `ctx`, mirroring useReportListView's identical split (lib/listview.ts) and
 * for the identical reason: folded into the first effect, its cleanup would
 * fire on every re-publish, blanking the store and repopulating it inside
 * the same commit that changed nothing a command actually reads.
 */
export function usePublishCommandPageContext(ctx: CommandPageContext): void {
  useEffect(() => {
    publish(ctx);
  }, [ctx]);
  useEffect(() => () => publish(null), []);
}
