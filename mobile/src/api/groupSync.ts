import { relayClientOf } from './client';
import { followGroup, type AwayInstance } from './away';
import { fetchGroupRoster, type GroupApp, type GroupRoster } from './groupApps';
import { isRelayConnection, type RelayConnection, type ServerConnection } from './types';
import { followGroupChange, listDismissed, newConnectionId } from '../storage/connections';

export interface GroupsView {
  apps: GroupApp[];
  away: AwayInstance[];
  /** Whether the saved list changed. */
  changed: boolean;
}

const groupOf = (c: RelayConnection) => JSON.stringify([c.relayUrl, c.relayKey]);

/**
 * followGroups reads every group the saved connections belong to and brings
 * the saved list in line with it, the way the web UI lists the group: an
 * instance that joined later, or was away while this phone joined, appears on
 * its own, and one taken out of the group goes. `reachable` comes default
 * first, and the phones and extensions shown are those the first of it lists.
 */
export async function followGroups(saved: ServerConnection[], reachable: ServerConnection[]): Promise<GroupsView> {
  const first = await fetchGroupRoster(reachable);
  const groups = new Map<string, RelayConnection[]>();
  for (const c of saved) {
    if (isRelayConnection(c)) groups.set(groupOf(c), [...(groups.get(groupOf(c)) ?? []), c]);
  }
  const answered = reachable.filter(isRelayConnection).map((c) => c.instanceId);
  const dismissed = await listDismissed();
  const away: AwayInstance[] = [];
  const added: RelayConnection[] = [];
  const dropped: string[] = [];
  for (const [key, mine] of groups) {
    const keyed = mine.find((c) => c.relayFrameKey);
    if (!keyed?.relayFrameKey) continue;
    const roster: GroupRoster =
      first.from && isRelayConnection(first.from) && groupOf(first.from) === key
        ? first
        : await fetchGroupRoster(reachable.filter((c) => isRelayConnection(c) && groupOf(c) === key));
    away.push(...(roster.away ?? []));
    const client = await relayClientOf(keyed, keyed.relayFrameKey);
    const change = followGroup({
      saved: mine,
      live: [...client.siblings(), ...(roster.members ?? [])],
      away: roster.away,
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
        instanceId: f.instanceId,
      });
    }
    dropped.push(...change.drop);
  }
  const changed = added.length > 0 || dropped.length > 0;
  if (changed) await followGroupChange(added, dropped);
  return { apps: first.apps, away, changed };
}
