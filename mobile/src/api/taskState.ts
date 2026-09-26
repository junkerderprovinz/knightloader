// The word a row and a package header show for where they are, from the status
// the instance sends (core.Status). A package reads the way the web's header
// does (packageStatus in web/src/components/columns.tsx).
//
// It imports nothing at run time, so check-task-state.mjs can load it in node.

import type { Task } from './types';

/** The word a row or a package header shows, as its `status.*` key names it. */
export type StateWord =
  | 'collected'
  | 'queued'
  | 'running'
  | 'paused'
  | 'extracting'
  | 'finished'
  | 'failed'
  | 'notUnpacked'
  | 'disabled';

// Least settled first.
const RANK: Record<string, number> = {
  running: 0,
  extracting: 1,
  queued: 2,
  paused: 3,
  collected: 4,
  done: 5,
  error: 6,
};

// A status from a newer instance sorts with the settled ones.
const rank = (t: Task): number => RANK[t.status] ?? RANK.error;

const unpackFailed = (t: Task): boolean => t.status === 'done' && (t.unpack === 'error' || t.unpack === 'password');

/** rowWord is one task's word, or null for a status this build does not know. */
export function rowWord(t: Task): StateWord | null {
  // A disabled link that is waiting says so instead of its queue state, since
  // nothing starts it until it is enabled. One that runs or has settled says
  // what it is doing, as the web list does.
  if (t.enabled === false && (t.status === 'queued' || t.status === 'paused' || t.status === 'collected')) {
    return 'disabled';
  }
  switch (t.status) {
    case 'collected':
    case 'queued':
    case 'running':
    case 'paused':
    case 'extracting':
      return t.status;
    case 'done':
      return unpackFailed(t) ? 'notUnpacked' : 'finished';
    case 'error':
      return 'failed';
  }
  return null;
}

export interface PackageState {
  word: StateWord | null;
  /** Links that did not download, plus archives that did not unpack. */
  failed: number;
}

/**
 * packageState is a package header's word and what in it failed. While
 * anything in the package is still going on, that is the word, and the
 * failures are counted beside it. Once nothing is, a failure wins: a package
 * with one dead link among nine finished files is not finished.
 */
export function packageState(tasks: Task[]): PackageState {
  const failedDownloads = tasks.filter((t) => t.status === 'error').length;
  // The instance marks every part of a set, so a set counts once, by its first part.
  const failedArchives = tasks.filter((t) => unpackFailed(t) && (t.archivePart ?? 0) <= 1).length;
  const failed = failedDownloads + failedArchives;
  const least = tasks.reduce<Task | undefined>((a, t) => (!a || rank(t) < rank(a) ? t : a), undefined);
  if (!least) return { word: null, failed };
  if (rank(least) >= RANK.collected) {
    if (failedDownloads > 0) return { word: 'failed', failed };
    if (failedArchives > 0) return { word: 'notUnpacked', failed };
  }
  return { word: rowWord(least), failed };
}
