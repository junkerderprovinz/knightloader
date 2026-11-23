import { relayClientOf } from './client';
import { currentConnection } from './moved';
import { followGroup, type AwayInstance, type BehindDevice } from './away';
import { fetchGroupRoster, type GroupApp, type GroupRoster } from './groupApps';
import type { ServerConnection } from './types';
import { followGroupChange, listDismissed, newConnectionId } from '../storage/connections';

export interface GroupsView {
  apps: GroupApp[];
  away: AwayInstance[];
  behind: BehindDevice[];
  /** Whether the saved list changed. */
  changed: boolean;
}

const groupOf = (c: ServerConnection) => JSON.stringify([c.relayUrl, c.relayKey]);

/**
 * followGroups reads every group the saved connections belong to and brings
 * the saved list in line with it, the way the web UI lists the group: an
 * instance that joined later, or was away while this phone joined, appears on
 * its own, and one taken out of the group goes. `reachable` comes default
 * first, and the phones and extensions shown are those the first of it lists.
 */
export async function followGroups(read: ServerConnection[], answering: ServerConnection[]): Promise<GroupsView> {
  // A group that moved while the caller read the list is followed where it went.
  const saved = read.map(currentConnection);
  const reachable = answering.map(currentConnection);
  const first = await fetchGroupRoster(reachable);
  const groups = new Map<string, ServerConnection[]>();
  for (const c of saved) {
    groups.set(groupOf(c), [...(groups.get(groupOf(c)) ?? []), c]);
  }
  const answered = reachable.map((c) => c.instanceId);
  const dismissed = await listDismissed();
  const away: AwayInstance[] = [];
  const behind: BehindDevice[] = [];
  const added: ServerConnection[] = [];
  const dropped: string[] = [];
  const renamed = new Map<string, string>();
  for (const [key, mine] of groups) {
    const keyed = mine.find((c) => c.relayFrameKey);
    if (!keyed?.relayFrameKey) continue;
    const peerAuthKey = mine.find((c) => c.peerAuthKey)?.peerAuthKey;
    const roster: GroupRoster =
      first.from && groupOf(first.from) === key
        ? first
        : await fetchGroupRoster(reachable.filter((c) => groupOf(c) === key));
    away.push(...(roster.away ?? []));
    behind.push(...roster.behind);
    const client = await relayClientOf(keyed, keyed.relayFrameKey);
    const change = followGroup({
      saved: mine,
      live: [...client.siblings(), ...(roster.members ?? [])],
      away: roster.away,
      behind: roster.behind.map((b) => b.id),
      answered,
      settled: client.settled(),
      dismissed,
    });
    for (const f of change.add) {
      added.push({
        kind: 'relay',
        id: newConnectionId(),
        name: f.name,
        relayUrl: keyed.relayUrl,
        relayKey: keyed.relayKey,
        relayFrameKey: keyed.relayFrameKey,
        ...(peerAuthKey ? { peerAuthKey } : {}),
        instanceId: f.instanceId,
      });
    }
    dropped.push(...change.drop);
    for (const r of change.rename) renamed.set(r.id, r.name);
  }
  const changed = added.length > 0 || dropped.length > 0 || renamed.size > 0;
  if (changed) await followGroupChange(added, dropped, renamed);
  return { apps: first.apps, away, behind, changed };
}
