import { fetchQueue, request } from './client';
import type { ServerConnection } from './types';
import { ltr } from '../i18n/bidi';

/**
 * What one instance is doing, in the figures the whole family shows. The
 * browser extension's instance card answers the same question with the same
 * numbers.
 *
 * Read from /api/queue/counters, the server's own answer and the one the
 * extension reads. It is relay-forwardable like every other queue route and
 * counts files owed rather than rows on screen: done, failed and collected
 * tasks are excluded, disabled ones are counted but contribute no speed.
 * Deriving the same figures from the task list would mean a second call per
 * instance and a second definition of them.
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
 * A reason rather than null, because an instance that refused the call, an
 * expired token and a relay that never answered are three different things and
 * a card that cannot show numbers should say which one stopped it.
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
    // Halted only when every instance that answered is halted: one running
    // instance means the group is not stopped, and a play button that claimed
    // otherwise would be lying about what it offers to do.
    halted: live.length > 0 && live.every((s) => s.halted),
    online: live.length,
    total: all.length,
  };
}

function bytes(n: number): string {
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

/**
 * Binary units, the same ladder the web UI and the extension walk. The figure
 * and its unit keep their order in a right-to-left line (i18n/bidi.ts).
 */
export const fmtBytes = (n: number): string => ltr(bytes(n));

/** A transfer rate, one token with its unit for the same reason. */
export const fmtSpeed = (n: number): string => ltr(`${bytes(n)}/s`);
