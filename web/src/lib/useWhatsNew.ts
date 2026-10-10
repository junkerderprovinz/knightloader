// The live side of lib/whatsNew.ts: the record of the running version as the
// stored interface state holds it, and whether the window with the release
// notes and the list of changes is open. Module-level like
// lib/commandPaletteOpen.ts, so the Version card and the event list open the
// window that app/Layout.tsx mounts without a prop chain.
import { useMemo, useSyncExternalStore } from 'react';
import { fetchHealth } from './api';
import { peekUIState, readUIState, useUIState, writeUIState } from './uistate';
import { NEWS_FIELD, changesOf, recordFor, type Change, type NewsRecord } from './whatsNew';

export type NewsTab = 'notes' | 'changes';

interface Shown {
  /** The running version, '' until the server has said it. */
  running: string;
  open: NewsTab | null;
}

let shown: Shown = { running: '', open: null };
const listeners = new Set<() => void>();

function set(next: Partial<Shown>): void {
  shown = { ...shown, ...next };
  for (const fn of listeners) fn();
}

function subscribe(fn: () => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

export function openWhatsNew(tab: NewsTab): void {
  set({ open: tab });
}

export function closeWhatsNew(): void {
  set({ open: null });
}

/** The tab the window is open on, or null while it is closed. */
export function useWhatsNewOpen(): NewsTab | null {
  return useSyncExternalStore(
    subscribe,
    () => shown.open,
    () => null,
  );
}

/**
 * startWhatsNew settles the record for the version the server runs and opens
 * the release notes once when that version arrived by an update. The first-run
 * tour's flag tells an instance in use from a fresh one where no record was
 * kept yet, which is every instance updating from a release without one.
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
  set({ running, open: record.notes ? shown.open : 'notes' });
}

export interface WhatsNew {
  version: string;
  /** Every change the update brought, seen or not. */
  changes: Change[];
  seen: ReadonlySet<string>;
  unseen: Change[];
  /** Whether the dots are drawn at all. */
  dots: boolean;
  markSeen: (ids: string[], seen?: boolean) => void;
  showDots: (on: boolean) => void;
}

/** useWhatsNew is the running version's record, or null until startWhatsNew has settled it. */
export function useWhatsNew(): WhatsNew | null {
  const [record] = useUIState<NewsRecord | null>(NEWS_FIELD, null);
  const running = useSyncExternalStore(
    subscribe,
    () => shown.running,
    () => '',
  );
  return useMemo(() => {
    if (!record || record.version !== running) return null;
    const changes = changesOf(record);
    const seen = new Set(record.seen);
    // Read at the moment of the click, so two clicks in one frame both count.
    const write = (fields: Partial<NewsRecord>) =>
      writeUIState(NEWS_FIELD, { ...peekUIState<NewsRecord>(NEWS_FIELD, record), ...fields });
    return {
      version: record.version,
      changes,
      seen,
      unseen: changes.filter((c) => !seen.has(c.id)),
      dots: record.dots !== false,
      markSeen: (ids, on = true) => {
        const next = new Set(peekUIState<NewsRecord>(NEWS_FIELD, record).seen);
        for (const id of ids) {
          if (on) next.add(id);
          else next.delete(id);
        }
        write({ seen: [...next] });
      },
      showDots: (on) => write({ dots: on }),
    };
  }, [record, running]);
}
