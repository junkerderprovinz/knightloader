import { fetchQueue, request } from './client';
import type { ServerConnection } from './types';

/**
 * What one instance is DOING, in the figures the whole family shows.
 *
 * The browser extension's own instance card answers exactly this question, and
 * the app's overview answered a different one: it printed the relay address
 * ("über Relay …"), which says where the connection goes rather than what is
 * happening at the other end (jdp, 2026-08-30: "über relay text soll nicht
 * dort stehen. dort sollen die gleichen infos in der card stehen wie in den
 * cards in der browsererweiterung"). One shape, three surfaces.
 *
 * Read from /api/queue/counters, which is the server's own answer to this
 * question and the one the extension reads. The first cut derived the numbers
 * from the task list instead, on the belief that no counters endpoint existed -
 * it does, it is relay-forwardable like every other queue route, and it counts
 * FILES OWED rather than rows on screen (done, failed and collected tasks are
 * excluded, disabled ones are counted but contribute no speed). Deriving them
 * here meant two calls per instance AND a second, quietly different definition
 * of the same four numbers.
 */
export interface InstanceStats {
  files: number;
  running: number;
  speed: number;
  /** Bytes still to fetch across everything unfinished. */
  remaining: number;
  halted: boolean;
}

interface RawCounters {
  files?: number;
  running?: number;
  disabled?: number;
  speed?: number;
  remaining?: number;
}

/**
 * Why this returns a reason and not just null: the overview swallowed every
 * failure (`.catch(() => null)`), so an instance that refused a call, a token
 * that had expired and a relay that never answered all rendered as the same
 * silent nothing - and when jdp reported that the play/stop buttons "have no
 * effect", the app had thrown away every piece of evidence that could have
 * said why. A card that cannot show numbers should say what stopped it.
 */
export type StatsResult = { ok: true; stats: InstanceStats } | { ok: false; reason: string };

export async function fetchInstanceStats(conn: ServerConnection): Promise<StatsResult> {
  try {
    const [queue, counters] = await Promise.all([
      fetchQueue(conn),
      request<RawCounters>(conn, '/api', '/queue/counters'),
    ]);
    return {
      ok: true,
      stats: {
        files: counters.files ?? 0,
        running: counters.running ?? 0,
        speed: counters.speed ?? 0,
        remaining: counters.remaining ?? 0,
        halted: queue.halted,
      },
    };
  } catch (e) {
    return { ok: false, reason: e instanceof Error ? e.message : String(e) };
  }
}

/** The same figures across every instance that answered, plus how many there
 *  were. What the overview card at the top of the list shows. */
export function aggregate(all: (InstanceStats | null)[]): InstanceStats & {
  online: number;
  total: number;
} {
  const live = all.filter((s): s is InstanceStats => s !== null);
  return {
    files: live.reduce((n, s) => n + s.files, 0),
    running: live.reduce((n, s) => n + s.running, 0),
    speed: live.reduce((n, s) => n + s.speed, 0),
    remaining: live.reduce((n, s) => n + s.remaining, 0),
    // Halted only when EVERY instance that answered is halted: a single
    // running instance means the group is not stopped, and a play button that
    // claimed otherwise would be lying about the thing it offers to do.
    halted: live.length > 0 && live.every((s) => s.halted),
    online: live.length,
    total: all.length,
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
