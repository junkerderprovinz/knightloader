import type { ServerConnection } from './types';

// How the phone follows its group into a fortress. An instance whose group
// moved hands every phone of the group the keys of the pairing there, over the
// group on the old phrase, sealed and signed like every answer in it. The
// phone keeps them in place of the old keys and never learns the fortress
// phrase, which opens every app of the household.

/** Where an instance hands them over (internal/api/fortress.go). */
export const MOVED_PATH = '/api/group/peer/moved';

/** The keys of a pairing, under the names a saved connection keeps them by. */
export interface PairingKeys {
  relayKey: string;
  relayFrameKey: string;
  peerAuthKey: string;
}

const HEX_KEY = /^[0-9a-f]{64}$/;

/** How long the phone waits before it asks a group again whether it moved. */
const MOVED_CHECK_MS = 5 * 60 * 1000;

/** parseMoved reads the keys an instance answered with, or null for anything that is no set of them. */
export function parseMoved(body: string): PairingKeys | null {
  let o: { groupKey?: unknown; frameKey?: unknown; peerAuthKey?: unknown } | null;
  try {
    o = JSON.parse(body);
  } catch {
    return null;
  }
  const { groupKey, frameKey, peerAuthKey } = o ?? {};
  const hex = (k: unknown): k is string => typeof k === 'string' && HEX_KEY.test(k);
  if (!hex(groupKey) || !hex(frameKey) || !hex(peerAuthKey)) return null;
  return { relayKey: groupKey, relayFrameKey: frameKey, peerAuthKey };
}

const groupOf = (c: { relayUrl: string; relayKey: string }) => JSON.stringify([c.relayUrl, c.relayKey]);

const sameKeys = (c: ServerConnection, keys: PairingKeys) =>
  c.relayKey === keys.relayKey && c.relayFrameKey === keys.relayFrameKey && c.peerAuthKey === keys.peerAuthKey;

/**
 * followMove is list with every connection into from's group on keys, or null
 * when from holds them already.
 */
export function followMove(list: ServerConnection[], from: ServerConnection, keys: PairingKeys): ServerConnection[] | null {
  if (sameKeys(from, keys)) return null;
  const group = groupOf(from);
  return list.map((c) => (groupOf(c) === group ? { ...c, ...keys } : c));
}

// The groups that moved while the app runs, by the relay and key they left,
// for screens still holding a connection they read before.
const moves = new Map<string, PairingKeys>();

export function rememberMove(from: ServerConnection, keys: PairingKeys): void {
  for (const [group, to] of moves) {
    if (to.relayKey === from.relayKey) moves.set(group, keys);
  }
  moves.set(groupOf(from), keys);
}

/** currentConnection is conn on the keys its group moved to, or conn itself. */
export function currentConnection(conn: ServerConnection): ServerConnection {
  const keys = moves.get(groupOf(conn));
  return keys ? { ...conn, ...keys } : conn;
}

const asked = new Map<string, number>();

/** askDue reports whether conn's group is to be asked again whether it moved, and counts it asked if so. */
export function askDue(conn: ServerConnection, now: number): boolean {
  const group = groupOf(conn);
  const last = asked.get(group);
  if (last !== undefined && now - last < MOVED_CHECK_MS) return false;
  asked.set(group, now);
  return true;
}
