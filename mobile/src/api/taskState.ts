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
  | 'seeding'
  | 'finished'
  | 'failed'
  | 'notUnpacked'
  | 'disabled';

// Least settled first. Seeding comes after every state that still owes a
// download, as on the web.
const RANK: Record<string, number> = {
  running: 0,
  extracting: 1,
  queued: 2,
  paused: 3,
  seeding: 4,
  collected: 5,
  done: 6,
  error: 7,
};

const seeds = (t: Task): boolean => t.status === 'done' && t.seeding === true;

// A status from a newer instance sorts with the settled ones.
const rank = (t: Task): number => (seeds(t) ? RANK.seeding : (RANK[t.status] ?? RANK.error));

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
      // An archive that did not unpack is the thing to act on, seeding or not.
      if (unpackFailed(t)) return 'notUnpacked';
      return seeds(t) ? 'seeding' : 'finished';
    case 'error':
      return 'failed';
  }
  return null;
}

/**
 * isParked is whether a row, or a package header over `tasks`, reads as
 * switched off: every link in it disabled. Its words and figures then take the
 * muted ink, and its switch stays as it is, since that is how it comes back.
 */
export function isParked(tasks: Task[]): boolean {
  return tasks.length > 0 && tasks.every((t) => t.enabled === false);
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
  if (rank(least) >= RANK.collected && failedDownloads > 0) return { word: 'failed', failed };
  // An archive that failed speaks over a torrent still seeding, as it does in
  // the web's header, where only an unpacked archive gives way to the upload.
  if (rank(least) >= RANK.seeding && failedArchives > 0) return { word: 'notUnpacked', failed };
  return { word: rowWord(least), failed };
}
