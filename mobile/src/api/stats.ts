import { fetchQueue, fetchTasks } from './client';
import type { ServerConnection, Task } from './types';

/**
 * What one instance is DOING, in the four numbers the whole family shows.
 *
 * The browser extension's own instance card answers exactly this question, and
 * the app's overview answered a different one: it printed the relay address
 * ("über Relay …"), which says where the connection goes rather than what is
 * happening at the other end (jdp, 2026-08-30: "über relay text soll nicht
 * dort stehen. dort sollen die gleichen infos in der card stehen wie in den
 * cards in der browsererweiterung"). One shape, three surfaces.
 *
 * Derived from the task list rather than fetched: the server has no counters
 * endpoint, and the extension computes the same figures the same way from what
 * /api/tasks already returns. Two calls per instance, which is why the overview
 * polls on a slow interval rather than streaming.
 */
export interface InstanceStats {
  files: number;
  running: number;
  speed: number;
  /** Bytes still to fetch across everything unfinished. */
  remaining: number;
  halted: boolean;
}

const unfinished = (t: Task) => t.status !== 'done' && t.status !== 'error';

export function statsFromTasks(tasks: Task[], halted: boolean): InstanceStats {
  let speed = 0;
  let remaining = 0;
  let running = 0;
  for (const t of tasks) {
    if (t.status === 'downloading') running++;
    speed += t.speed || 0;
    if (unfinished(t)) remaining += Math.max(0, (t.size || 0) - (t.loaded || 0));
  }
  return { files: tasks.length, running, speed, remaining, halted };
}

/** null means "asked and got nothing back" - a different fact than "not asked",
 *  and the card says so rather than drawing zeroes for an instance that is
 *  simply not there. */
export async function fetchInstanceStats(conn: ServerConnection): Promise<InstanceStats | null> {
  try {
    const [queue, tasks] = await Promise.all([fetchQueue(conn), fetchTasks(conn)]);
    return statsFromTasks(tasks, queue.halted);
  } catch {
    return null;
  }
}

/** The same four numbers across every instance that answered, plus how many of
 *  them there were. What the overview card at the top of the list shows. */
export function aggregate(all: (InstanceStats | null)[]): InstanceStats & { online: number; total: number; anyRunning: boolean } {
  const live = all.filter((s): s is InstanceStats => s !== null);
  return {
    files: live.reduce((n, s) => n + s.files, 0),
    running: live.reduce((n, s) => n + s.running, 0),
    speed: live.reduce((n, s) => n + s.speed, 0),
    remaining: live.reduce((n, s) => n + s.remaining, 0),
    // Halted only when EVERY instance that answered is halted: a single
    // running instance means the group is not stopped, and a play button that
    // claimed otherwise would be lying about the thing it is offering to do.
    halted: live.length > 0 && live.every((s) => s.halted),
    online: live.length,
    total: all.length,
    anyRunning: live.some((s) => s.running > 0),
  };
}

/** Binary units, the same ladder the web UI and the extension walk. */
export function fmtBytes(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return '0 B';
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
  let v = n;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v >= 100 || i === 0 ? Math.round(v) : v.toFixed(1)} ${units[i]}`;
}
