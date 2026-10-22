import type { ServerConnection } from './types';

/** An instance approved in the group that is not there now, as it last announced itself. */
export interface AwayInstance {
  id: string;
  name: string;
  deployment: string;
  address: string;
  /** Unix seconds. */
  lastSeen: number;
}

export function awayList(raw: unknown): AwayInstance[] {
  if (!Array.isArray(raw)) return [];
  return raw.filter((m): m is AwayInstance => !!m && typeof m.id === 'string' && typeof m.lastSeen === 'number');
}

/**
 * awayFor is the group's entry for a saved connection that did not answer, so
 * its card says Inactive and when it was last seen rather than Not connected.
 * A connection the group does not list as away gets undefined.
 */
export function awayFor(conn: ServerConnection, away: AwayInstance[]): AwayInstance | undefined {
  if (conn.kind !== 'relay') return undefined;
  return away.find((m) => m.id === conn.instanceId);
}
