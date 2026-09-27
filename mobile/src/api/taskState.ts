// The word a row and a package header show for where they are, from the status
// the instance sends (core.Status). A package reads the way the web's header
// does (packageStatus in web/src/components/columns.tsx).
//
// It imports nothing at run time, so check-task-state.mjs can load it in node.

import type { ExtractJob, Task } from './types';

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

/** Which part of the Downloads screen a package is listed in. */
export type ListCard = 'downloads' | 'finished' | 'torrents';

// A torrent a debrid service fetched comes down over HTTP and is not one.
const isTorrent = (t: Task): boolean => t.resolver === 'torrent';

// The web's twins are in web/src/lib/listCards.ts.
/** A finished torrent that seeds or is about to, which stopping the seeding ends. */
export const seedingOn = (t: Task): boolean => t.status === 'done' && isTorrent(t) && !t.seedingOver;

/** A finished torrent that seeds no more, which starting the seeding takes up again. */
export const seedingOff = (t: Task): boolean => t.status === 'done' && isTorrent(t) && !!t.seedingOver;

/**
 * packageCard is the part of the list a package belongs in, by the rule the
 * web's Downloads page uses (packageCard in web/src/lib/listCards.ts). It is
 * finished once every link has downloaded and nothing is left to unpack: none
 * waiting for or in the middle of unpacking, none that failed to. A failed link
 * keeps it in the download list, and a finished package holding a torrent goes
 * to Torrents, seeding or not. A disabled link that has not downloaded does not
 * hold it back unless it is still running, and a package switched off whole
 * stays in the download list, where it can be switched on again.
 */
export function packageCard(tasks: Task[]): ListCard {
  if (tasks.every((t) => t.enabled === false)) return 'downloads';
  let done = 0;
  let torrent = false;
  for (const t of tasks) {
    if (t.status === 'done' && !unpackFailed(t)) {
      done++;
      if (isTorrent(t)) torrent = true;
      continue;
    }
    if (t.enabled === false && t.status !== 'running' && t.status !== 'extracting') continue;
    return 'downloads';
  }
  if (done === 0) return 'downloads';
  return torrent ? 'torrents' : 'finished';
}

/** The two parts below the download list and whether each is switched on. */
export interface CardSwitches {
  finished: boolean;
  torrents: boolean;
}

/**
 * splitByCard sorts the tasks of the download list into its parts, keeping
 * their order. A package goes by all of its links; the loose links, which share
 * the unnamed group without belonging together, go one by one. A part that is
 * switched off hands its packages on: Torrents to Finished, Finished to the
 * download list.
 */
export function splitByCard(tasks: Task[], on: CardSwitches): Record<ListCard, Task[]> {
  const byPackage = new Map<string, Task[]>();
  for (const t of tasks) {
    const name = t.package || '';
    const list = byPackage.get(name);
    if (list) list.push(t);
    else byPackage.set(name, [t]);
  }
  const placed = (card: ListCard): ListCard => {
    if (card === 'torrents' && !on.torrents) card = 'finished';
    return card === 'finished' && !on.finished ? 'downloads' : card;
  };
  const cardOf = new Map<string, ListCard>();
  for (const [name, list] of byPackage) if (name !== '') cardOf.set(name, placed(packageCard(list)));
  const out: Record<ListCard, Task[]> = { downloads: [], finished: [], torrents: [] };
  for (const t of tasks) {
    const name = t.package || '';
    out[name === '' ? placed(packageCard([t])) : (cardOf.get(name) ?? 'downloads')].push(t);
  }
  return out;
}

/** How far an unpacking has got, as a row's bar draws it. */
export interface UnpackProgress {
  unpacked: number;
  /** 0 when the format does not say how much the archive holds. */
  size: number;
  failed: boolean;
}

/**
 * unpackingByTask maps every file of an archive to the latest unpacking of it,
 * as the web's list does (extractionsByTask in web/src/components/Archives.tsx).
 * The jobs arrive oldest first, so a retry replaces the failure before it, and
 * a cancelled job hands the rows back to their own status.
 */
export function unpackingByTask(jobs: ExtractJob[]): Map<string, ExtractJob> {
  const out = new Map<string, ExtractJob>();
  for (const j of jobs) {
    for (const id of j.parts ?? [j.taskId]) {
      if (j.status === 'cancelled') out.delete(id);
      else out.set(id, j);
    }
  }
  return out;
}

/**
 * unpackProgress is what a row's bar shows in place of the finished download,
 * by the rule of the web's progress column (barOf in
 * web/src/components/columns.tsx). A file downloading again has left its
 * archive's unpacking behind.
 */
export function unpackProgress(t: Task, byTask: Map<string, ExtractJob>): UnpackProgress | null {
  if (t.status !== 'done' && t.status !== 'extracting') return null;
  const job = byTask.get(t.id);
  if (job?.status === 'running') return { unpacked: job.unpacked ?? 0, size: job.size ?? 0, failed: false };
  if (job?.status === 'error' && job.size) return { unpacked: job.unpacked ?? 0, size: job.size, failed: true };
  return null;
}

/** unpackPercent is an unpacking's share done, or null when its size is unknown. */
export function unpackPercent(u: UnpackProgress): number | null {
  return u.size > 0 ? Math.min(100, Math.round((u.unpacked / u.size) * 100)) : null;
}
