// What the background watch counts as news about unpacking and about hoster
// accounts, beside rules.ts and for the same reason apart from React and the
// native module: check-watch-archives-accounts.mjs runs these in node. It
// imports nothing at run time.

/** One unpacking as GET /api/extract sends it, the part the watch reads. */
export interface WatchJob {
  id: string;
  /** 'queued' | 'running' | 'done' | 'error' | 'cancelled'. */
  status: string;
  /** The archive's file name. */
  name?: string;
  package?: string;
}

/** One bench as GET /api/accounts/paused sends it. */
export interface PausedAccount {
  /** The same for the same bench from one read to the next. */
  id: string;
  /** The account's own hash, as PausedAccounts.on lists it. */
  account: string;
  service: string;
  /** The hoster the bench covers, absent for every hoster. */
  host?: string;
  /** A used-up download volume rather than a failure. */
  volume?: boolean;
  /** RFC3339. */
  until: string;
}

/** What GET /api/accounts/paused answers: the benches, and every account
 *  that is switched on. */
export interface PausedAccounts {
  benches: PausedAccount[];
  on: string[];
}

/** Each unpacking's status at the last read of the list, by id. */
export type JobLook = Record<string, string>;

export const jobLookOf = (jobs: WatchJob[]): JobLook => Object.fromEntries(jobs.map((j) => [j.id, j.status]));

/**
 * Whether this look reads the unpackings, which costs a call of its own. A
 * task stands in 'extracting' while its archive waits or unpacks, so the list
 * is read while one does and once after. A small archive can be queued and
 * unpacked between two looks, so a download that has just finished counts
 * too, and so does a job the last read found still to end. The first read is
 * the one the later ones are compared with.
 */
export function readsJobs(
  before: JobLook | null,
  statusBefore: Record<string, string> | null,
  tasks: { id: string; status: string }[],
): boolean {
  if (!before) return true;
  if (Object.values(before).some((s) => s === 'queued' || s === 'running')) return true;
  return tasks.some(
    (t) =>
      t.status === 'extracting' ||
      statusBefore?.[t.id] === 'extracting' ||
      (t.status === 'done' && statusBefore?.[t.id] !== 'done'),
  );
}

export interface JobNews {
  unpacked: WatchJob[];
  failed: WatchJob[];
}

/**
 * The unpackings that ended since the last read. A job that read had not seen
 * was started after it, so one that already ended is news as well. A
 * cancelled one never is, since somebody called it off.
 */
export function compareJobs(before: JobLook | null, jobs: WatchJob[]): JobNews {
  if (!before) return { unpacked: [], failed: [] };
  const ended = (to: string) => jobs.filter((j) => j.status === to && before[j.id] !== to);
  return { unpacked: ended('done'), failed: ended('error') };
}

/** What a notification calls an unpacking: its archive, else its package. */
export const jobName = (j: WatchJob): string => j.name || j.package || '';

/** The benches at the last read, by id. */
export type BenchLook = Record<string, PausedAccount>;

export const benchLookOf = (read: PausedAccounts): BenchLook => Object.fromEntries(read.benches.map((p) => [p.id, p]));

export interface BenchNews {
  paused: PausedAccount[];
  back: PausedAccount[];
}

/**
 * The benches that began and the ones that ended since the last read. A bench
 * that is gone because its account was switched off or removed did not end:
 * that account is not in use again, so only one still switched on is back.
 */
export function compareBenches(before: BenchLook | null, read: PausedAccounts): BenchNews {
  if (!before) return { paused: [], back: [] };
  const still = new Set(read.benches.map((p) => p.id));
  const on = new Set(read.on);
  return {
    paused: read.benches.filter((p) => !before[p.id]),
    back: Object.values(before).filter((p) => !still.has(p.id) && on.has(p.account)),
  };
}

/** How often the benches are read. A bench lasts minutes at least, and a look
 *  every ten seconds would ask for them far more often than they change. */
export const BENCH_MS = 60_000;

export const benchesDue = (lastRead: number, now: number): boolean => now - lastRead >= BENCH_MS;
