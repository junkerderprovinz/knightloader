import type { ServerConnection } from './types';

/**
 * splitSaved reads the stored connection list into the connections this app
 * keeps, every one reached with the twelve words, and the names of those an
 * older build saved by their address, which this one cannot reach.
 */
export function splitSaved(raw: unknown): { kept: ServerConnection[]; dropped: string[] } {
  const kept: ServerConnection[] = [];
  const dropped: string[] = [];
  if (!Array.isArray(raw)) return { kept, dropped };
  for (const c of raw) {
    if (!c || typeof c !== 'object') continue;
    const entry = c as { kind?: unknown; name?: unknown };
    if (entry.kind === 'relay') kept.push(c as ServerConnection);
    else dropped.push(typeof entry.name === 'string' ? entry.name : '');
  }
  return { kept, dropped: dropped.filter((name) => name !== '') };
}

/**
 * offersPeerAuth reports whether a saved connection lacks the key of the local
 * network, paired before the app kept it. Scanning the phrase again gives it,
 * and files then play at home straight from the instance.
 */
export function offersPeerAuth(list: ServerConnection[]): boolean {
  return list.some((c) => c.relayFrameKey && !c.peerAuthKey);
}
