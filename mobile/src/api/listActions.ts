// The calls behind the collector's rejected and skipped links and the queue's
// two limits. Each goes to the instance's own routes: none of them is
// forwarded to a peer.
import { request } from './client';
import type { SkippedLink } from './notAdded';
import type { ServerConnection } from './types';

/** The links folded into one already in the list, or lost before it, oldest first. */
export async function fetchSkipped(conn: ServerConnection): Promise<SkippedLink[]> {
  return (await request<SkippedLink[] | undefined>(conn, '/api', '/collector/skipped')) ?? [];
}

/** clearSkipped forgets those notes and adds nothing back. */
export async function clearSkipped(conn: ServerConnection): Promise<void> {
  await request(conn, '/api', '/collector/skipped', { method: 'DELETE' });
}

/** restoreHeld puts rejected links back in the collector past what caught them. */
export async function restoreHeld(conn: ServerConnection, ids: string[]): Promise<void> {
  await request(conn, '/api', '/collector/filtered/restore', { method: 'POST', body: JSON.stringify({ ids }) });
}

// The ids go in the query, the way the web sends them, since an empty list
// there means all of them.
export async function clearHeld(conn: ServerConnection, ids: string[]): Promise<void> {
  await request(conn, '/api', `/collector/filtered?ids=${encodeURIComponent(ids.join(','))}`, { method: 'DELETE' });
}

/** The two values the web's quick settings change while downloads run. */
export interface QueueLimits {
  /** Bytes per second for every download together, 0 for none. */
  speedLimit: number;
  maxConcurrent: number;
}

export async function fetchLimits(conn: ServerConnection): Promise<QueueLimits> {
  return request<QueueLimits>(conn, '/api', '/queue/limits');
}

/** setLimits saves what it is given and answers with what the instance stored. */
export async function setLimits(conn: ServerConnection, limits: Partial<QueueLimits>): Promise<QueueLimits> {
  return request<QueueLimits>(conn, '/api', '/queue/limits', { method: 'PUT', body: JSON.stringify(limits) });
}
