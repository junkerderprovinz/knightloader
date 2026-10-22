import { ApiError, request } from './client';
import { awayList, memberList, type AwayInstance } from './away';
import type { RelaySibling } from './relayClient';
import type { ServerConnection } from './types';

/** A phone or browser extension in the group, as an instance lists it. */
export interface GroupApp {
  id: string;
  name: string;
  deployment: 'mobile' | 'extension';
  connected: boolean;
  approved: boolean;
  /** Unix seconds, 0 for a device no instance has seen yet. */
  lastSeen: number;
}

/** The group's phones and extensions, and the instances approved in it that are away. */
export interface GroupRoster {
  apps: GroupApp[];
  /** null from an instance from before the away list, or when none answered. */
  away: AwayInstance[] | null;
  /** The other instances there now as the instance that answered sees them,
   *  on the relay or on its network; null where it does not send them. */
  members: RelaySibling[] | null;
  /** The connection that answered. */
  from?: ServerConnection;
}

function isApp(a: unknown): a is GroupApp {
  const o = a as Partial<GroupApp> | null;
  return !!o && typeof o.id === 'string' && (o.deployment === 'mobile' || o.deployment === 'extension');
}

const NOBODY: GroupRoster = { apps: [], away: null, members: null };

/**
 * fetchGroupRoster reads the group from the first of conns that answers, so
 * the caller passes the default instance first. An instance too old for
 * GET /api/group/apps answers 404, or refuses with ownLoginNeeded, and either
 * means no device cards rather than an error on the overview. One from before
 * the away list sends none.
 */
export async function fetchGroupRoster(conns: ServerConnection[]): Promise<GroupRoster> {
  for (const conn of conns) {
    try {
      const r = await request<{ apps?: unknown; away?: unknown; members?: unknown } | undefined>(conn, '/api', '/group/apps');
      return {
        apps: Array.isArray(r?.apps) ? r.apps.filter(isApp) : [],
        away: awayList(r?.away),
        members: memberList(r?.members),
        from: conn,
      };
    } catch (e) {
      if (e instanceof ApiError && (e.status === 404 || e.code === 'ownLoginNeeded')) return NOBODY;
    }
  }
  return NOBODY;
}
