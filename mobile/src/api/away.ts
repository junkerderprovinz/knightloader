import type { RelaySibling } from './relayClient';
import type { RelayConnection, ServerConnection } from './types';

/** An instance approved in the group that is not there now, as it last announced itself. */
export interface AwayInstance {
  id: string;
  name: string;
  deployment: string;
  address: string;
  /** Unix seconds. */
  lastSeen: number;
}

/** awayList reads the away list of an answer, null for an instance from before it. */
export function awayList(raw: unknown): AwayInstance[] | null {
  if (!Array.isArray(raw)) return null;
  return raw.filter((m): m is AwayInstance => !!m && typeof m.id === 'string' && typeof m.lastSeen === 'number');
}

/** memberList reads the instances an answer says are there, null where it sends none. */
export function memberList(raw: unknown): RelaySibling[] | null {
  if (!Array.isArray(raw)) return null;
  return raw
    .filter((m) => !!m && typeof m.id === 'string')
    .map((m) => ({
      instanceId: m.id,
      name: typeof m.name === 'string' ? m.name : '',
      deployment: typeof m.deployment === 'string' ? m.deployment : '',
    }));
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

/** The instances to save for one group, the saved connections to drop, and
 *  the new names of those renamed since. */
export interface GroupChange {
  add: { instanceId: string; name: string }[];
  drop: string[];
  rename: { id: string; name: string }[];
}

/**
 * followGroup works out how the saved connections of one group follow it. An
 * instance live on the relay or listed as away that is not saved yet is added,
 * unless it was removed on this phone. A saved one takes the name the group
 * gives it, the one it announces on the relay before the one it was last seen
 * with. A saved one that is neither live nor away goes, since it was taken out
 * of the group, but only once the relay has announced every sibling
 * (`settled`) and an instance of the group sent its away list (`away` not
 * null); until then a missing instance may only be late.
 */
export function followGroup({
  saved,
  live,
  away,
  answered,
  settled,
  dismissed,
}: {
  saved: RelayConnection[];
  live: RelaySibling[];
  away: AwayInstance[] | null;
  /** The instances that answered this round, which are there whatever the relay says. */
  answered: string[];
  settled: boolean;
  /** The instances removed on this phone. */
  dismissed: string[];
}): GroupChange {
  const known = new Set(saved.map((c) => c.instanceId));
  const add: GroupChange['add'] = [];
  const found = [
    ...live.map((s) => ({ instanceId: s.instanceId, name: s.name || s.instanceId })),
    ...(away ?? []).map((m) => ({ instanceId: m.id, name: m.name || m.id })),
  ];
  for (const f of found) {
    if (known.has(f.instanceId) || dismissed.includes(f.instanceId)) continue;
    known.add(f.instanceId);
    add.push(f);
  }
  // An instance that announced no name keeps the one it had.
  const named = new Map<string, string>();
  for (const s of live) if (s.name && !named.has(s.instanceId)) named.set(s.instanceId, s.name);
  for (const a of away ?? []) if (a.name && !named.has(a.id)) named.set(a.id, a.name);
  const rename = saved.flatMap((c) => {
    const name = named.get(c.instanceId);
    return name && name !== c.name ? [{ id: c.id, name }] : [];
  });
  if (away === null || !settled) return { add, drop: [], rename };
  const there = new Set([...live.map((s) => s.instanceId), ...away.map((m) => m.id), ...answered]);
  return { add, drop: saved.filter((c) => !there.has(c.instanceId)).map((c) => c.id), rename };
}
