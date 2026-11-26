import { request } from './client';
import type { Call, PriorityChoice } from './taskActions';
import type { ServerConnection } from './types';

/** What a call on a selection answers: the links it touched, and for a
 *  removal the token that undoes it while the instance still holds the rows. */
export interface BulkResult {
  ids: string[];
  count: number;
  undo?: string;
  undoMs?: number;
}

/** sendCall makes one entry's request. Reset and recheck answer with no body,
 *  which comes back as undefined. */
export async function sendCall(conn: ServerConnection, base: string, call: Call): Promise<BulkResult | undefined> {
  return request<BulkResult | undefined>(conn, base, call.path, { method: 'POST', body: JSON.stringify(call.body) });
}

/** The priorities the instance orders by, read rather than built in, so the
 *  sheet cannot offer a value the instance clamps away. */
export async function fetchPriorities(conn: ServerConnection, base = '/api'): Promise<PriorityChoice[]> {
  return (await request<PriorityChoice[] | null>(conn, base, '/queue/priorities')) ?? [];
}
