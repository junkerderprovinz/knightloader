import { request } from './client';
import type { ServerConnection } from './types';
import type { PausedAccounts, WatchJob } from '../watch/archiveAccountRules';

/** Every unpacking the instance knows about, for the background watch. */
export async function fetchWatchJobs(conn: ServerConnection, signal?: AbortSignal): Promise<WatchJob[]> {
  return (await request<WatchJob[] | null>(conn, '/api', '/extract', { signal })) ?? [];
}

/** The benches running on the instance's accounts, without the accounts. */
export async function fetchPausedAccounts(conn: ServerConnection, signal?: AbortSignal): Promise<PausedAccounts> {
  const read = await request<Partial<PausedAccounts> | null>(conn, '/api', '/accounts/paused', { signal });
  return { benches: read?.benches ?? [], on: read?.on ?? [] };
}
