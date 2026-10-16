// The live side of lib/whatsNew.ts: the record as the stored interface state
// holds it, and whether the window with the release cards is open.
// Module-level like lib/commandPaletteOpen.ts, so the Version card and the
// event list open the window that app/Layout.tsx mounts without a prop chain.
import { useMemo, useSyncExternalStore } from 'react';
import { fetchHealth } from './api';
import { peekUIState, readUIState, useUIState, writeUIState } from './uistate';
import { NEWS_FIELD, changesOf, recordFor, releasesShown, type Change, type NewsRecord } from './whatsNew';

interface Shown {
  /** The running version, '' until the server has said it. */
  running: string;
  open: boolean;
}

let shown: Shown = { running: '', open: false };
const listeners = new Set<() => void>();

function set(next: Partial<Shown>): void {
  shown = { ...shown, ...next };
  for (const fn of listeners) fn();
}

function subscribe(fn: () => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

export function openWhatsNew(): void {
  set({ open: true });
}

export function closeWhatsNew(): void {
  set({ open: false });
}

export function useWhatsNewOpen(): boolean {
  return useSyncExternalStore(
    subscribe,
    () => shown.open,
    () => false,
  );
}

/**
 * startWhatsNew settles the record for the version the server runs and opens
 * the window once when that version arrived by an update. The first-run tour's
 * flag tells an instance in use from a fresh one where no record was kept yet,
 * which is every instance updating from a release without one.
 */
export async function startWhatsNew(): Promise<void> {
  const [state, health] = await Promise.all([readUIState(), fetchHealth()]);
  const running = health.version.split('+')[0].trim();
  if (!running) return;
  const stored = state[NEWS_FIELD];
  const record = recordFor(stored, running, state['onboarding.done'] === true);
  // Written as shown before the window opens, so a second browser loading in
  // the same minute does not open it again.
  if (record !== stored || !record.notes) writeUIState(NEWS_FIELD, { ...record, notes: true });
  set({ running, open: shown.open || !record.notes });
}

export interface WhatsNew {
  version: string;
  /** The release the instance ran before the last update, '' when not known. */
  from: string;
  /** The releases the window shows a card for, newest first. */
  releases: string[];
  /** Every change of those releases, seen or not. */
  changes: Change[];
  seen: ReadonlySet<string>;
  unseen: Change[];
  markSeen: (ids: string[], seen?: boolean) => void;
}

/** useWhatsNew is the record of the running version, or null until startWhatsNew has settled it. */
export function useWhatsNew(): WhatsNew | null {
  const [record] = useUIState<NewsRecord | null>(NEWS_FIELD, null);
  const running = useSyncExternalStore(
    subscribe,
    () => shown.running,
    () => '',
  );
  return useMemo(() => {
    if (!record || record.version !== running) return null;
    const seen = new Set(record.seen);
    const unseen = changesOf(record).filter((c) => !seen.has(c.id));
    const releases = releasesShown(record, unseen);
    return {
      version: record.version,
      from: record.from,
      releases,
      changes: changesOf(record).filter((c) => releases.includes(c.since)),
      seen,
      unseen,
      markSeen: (ids, on = true) => {
        // Read at the moment of the click, so two clicks in one frame both count.
        const now = peekUIState<NewsRecord>(NEWS_FIELD, record);
        const next = new Set(now.seen);
        for (const id of ids) {
          if (on) next.add(id);
          else next.delete(id);
        }
        writeUIState(NEWS_FIELD, { ...now, seen: [...next] });
      },
    };
  }, [record, running]);
}
