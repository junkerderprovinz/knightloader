// What the background watch counts as news, when it looks again and when it
// stops, apart from React and the native module so check-watch.mjs can run the
// rules in node. It imports nothing at run time for the same reason.

import type { CaptchaChallenge, Task } from '../api/types';

/** The part of one look at an instance that the next look is compared with. */
export interface Look {
  /** Status by task id. */
  status: Record<string, string>;
  /** The ids of the captchas that were waiting. */
  captchas: string[];
}

export function lookOf(tasks: Task[], captchas: CaptchaChallenge[]): Look {
  const status: Record<string, string> = {};
  for (const t of tasks) status[t.id] = t.status;
  return { status, captchas: captchas.map((ch) => ch.id) };
}

/**
 * Whether an instance has something on the go that a notification could be
 * about. A queued link counts only once it runs: a queue held by a schedule or
 * by hand can wait for hours, and the service would keep the phone awake for
 * all of them. Seeding has no end and does not count either.
 */
export function isBusy(tasks: Task[], captchas: CaptchaChallenge[]): boolean {
  return captchas.length > 0 || tasks.some((t) => t.status === 'running' || t.status === 'extracting');
}

/** A package, or a lone link, that news is about. */
export interface PackageNews {
  /** Stable for the package, so a later notification replaces this one. */
  key: string;
  name: string;
  /** The tasks the news is about: every file of a finished package, or the
   *  ones that have just failed. */
  tasks: Task[];
  /** The files of the package that count, and how they ended. */
  total: number;
  done: number;
  failed: number;
}

export interface News {
  /** Captchas that were not waiting at the last look. */
  arrived: CaptchaChallenge[];
  /** Packages whose last file has just settled, with at least one finished. */
  finished: PackageNews[];
  /** Packages with files that have just failed, those files only. */
  failed: PackageNews[];
  /** Whether anything moved from one state to another, or a captcha came or went. */
  changed: boolean;
}

const packageKey = (t: Task): string => (t.package ? `p:${t.package}` : `t:${t.id}`);

// A disabled link never starts and a collected one waits for somebody to say
// go, so neither holds a package back from being finished.
const counts = (t: Task): boolean => t.enabled !== false && t.status !== 'collected';

/**
 * What changed between two looks at the same instance. The first look has no
 * `before` and finds nothing new: what is already finished or waiting there
 * happened before the watch began.
 *
 * A task the last look did not have counts as one that was still to come, so a
 * small file added and finished between two looks is still announced.
 */
export function compare(before: Look | null, tasks: Task[], captchas: CaptchaChallenge[]): News {
  if (!before) return { arrived: [], finished: [], failed: [], changed: false };

  const seen = new Set(before.captchas);
  const arrived = captchas.filter((ch) => !seen.has(ch.id));
  const still = new Set(captchas.map((ch) => ch.id));
  const captchaGone = before.captchas.some((id) => !still.has(id));

  const turned = (t: Task, to: string) => t.status === to && before.status[t.id] !== to;
  let changed = arrived.length > 0 || captchaGone;
  const present = new Set<string>();
  const groups = new Map<string, Task[]>();
  for (const t of tasks) {
    present.add(t.id);
    if (before.status[t.id] !== t.status) changed = true;
    const key = packageKey(t);
    const list = groups.get(key);
    if (list) list.push(t);
    else groups.set(key, [t]);
  }
  if (Object.keys(before.status).some((id) => !present.has(id))) changed = true;

  const finished: PackageNews[] = [];
  const failed: PackageNews[] = [];
  for (const [key, group] of groups) {
    const counted = group.filter(counts);
    const done = counted.filter((t) => t.status === 'done').length;
    const errors = counted.filter((t) => t.status === 'error').length;
    const name = group[0].package || group[0].name || group[0].url;
    const settled = counted.length > 0 && done + errors === counted.length;
    if (settled && done > 0 && counted.some((t) => turned(t, 'done') || turned(t, 'error'))) {
      finished.push({ key, name, tasks: counted, total: counted.length, done, failed: errors });
    }
    const fresh = group.filter((t) => turned(t, 'error'));
    if (fresh.length > 0) {
      failed.push({ key, name, tasks: fresh, total: counted.length, done, failed: fresh.length });
    }
  }
  return { arrived, finished, failed, changed };
}

/** The pace while something changes or a captcha waits, the slowest one while
 *  a download runs, and the pace while nothing does. */
export const FAST_MS = 10_000;
export const SLOW_MS = 30_000;
export const IDLE_MS = 60_000;

/** How long to wait before the next look: soon after a change, then less and
 *  less often while a download runs without news, and once a minute while
 *  nothing runs at all. */
export function nextDelay(previous: number, changed: boolean, captchaWaiting: boolean, busy: boolean): number {
  if (changed || captchaWaiting) return FAST_MS;
  if (!busy) return IDLE_MS;
  return Math.min(Math.max(Math.round(previous * 1.5), FAST_MS), SLOW_MS);
}

/** Whether the phone stays awake until the next look. A wait at the idle pace
 *  is left to an alarm, which Android may hold back a little. */
export const stayAwake = (delay: number): boolean => delay < IDLE_MS;

/** How long the watch outlasts the last busy look: long enough for a queue to
 *  start its next link, and much longer for an instance that stopped answering
 *  while it was busy, since a phone on a train loses its connection for a
 *  while and the download carries on without it. */
export const GRACE_MS = 2 * 60_000;
export const OFFLINE_GRACE_MS = 15 * 60_000;

export interface Pulse {
  /** When a look last found the instance busy, 0 if never. */
  lastBusy: number;
  /** Whether the last look got an answer. */
  ok: boolean;
}

/**
 * Whether the service keeps running. With "Stay connected" on it always does
 * while there is an instance to watch. Otherwise it also runs for GRACE_MS
 * after it started, because a download added from the phone is often still in
 * the collector or the queue at the first look.
 */
export function keepRunning(pulses: Pulse[], startedAt: number, now: number, stay: boolean): boolean {
  if (stay && pulses.length > 0) return true;
  if (now - startedAt < GRACE_MS) return true;
  return pulses.some((p) => p.lastBusy > 0 && now - p.lastBusy < (p.ok ? GRACE_MS : OFFLINE_GRACE_MS));
}

/**
 * A notification id from its parts: the same parts give the same id, so news
 * about the same package replaces the last notification about it. 1 belongs to
 * the service's own notification.
 */
export function noticeId(...parts: string[]): number {
  let h = 0x811c9dc5;
  for (const ch of parts.join('\u0000')) {
    h ^= ch.codePointAt(0)!;
    h = Math.imul(h, 0x01000193);
  }
  const id = (h >>> 0) & 0x7fffffff;
  return id < 2 ? id + 2 : id;
}
