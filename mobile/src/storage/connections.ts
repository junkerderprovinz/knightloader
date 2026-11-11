import * as SecureStore from 'expo-secure-store';
import type { ServerConnection } from '../api/types';
import { closeAllRelayClients, closeRelayClient } from '../api/relayClient';
import { offersPeerAuth, splitSaved } from '../api/savedConnections';

// Every saved connection carries its group's relay key, a secret, so the whole
// list goes in the OS keychain (SecureStore) rather than AsyncStorage. The
// active-connection pointer is no secret on its own, but it lives here too
// rather than in a second storage mechanism, being meaningless without the list
// it points into.
const LIST_KEY = 'knightloader-connections';
const ACTIVE_KEY = 'knightloader-active-connection';
const DEFAULT_KEY = 'knightloader-default-connection';
// The group's instances this phone was told to leave off its list. The overview
// adds every instance of the group on its own, so a removal has to be kept.
const DISMISSED_KEY = 'knightloader-dismissed-instances';
// Set once the app has offered to scan the phrase again, so it offers once.
const PEER_AUTH_OFFERED_KEY = 'knightloader-peer-auth-offered';

export function newConnectionId(): string {
  return `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
}

async function readList(): Promise<unknown> {
  const raw = await SecureStore.getItemAsync(LIST_KEY);
  if (!raw) return null;
  try {
    return JSON.parse(raw);
  } catch {
    return null;
  }
}

export async function listConnections(): Promise<ServerConnection[]> {
  return splitSaved(await readList()).kept;
}

/**
 * offerPeerAuthOnce is true the one time the app should offer to scan the
 * phrase again, for connections paired before it kept the key of the local
 * network.
 */
export async function offerPeerAuthOnce(): Promise<boolean> {
  if (await SecureStore.getItemAsync(PEER_AUTH_OFFERED_KEY)) return false;
  if (!offersPeerAuth(await listConnections())) return false;
  await SecureStore.setItemAsync(PEER_AUTH_OFFERED_KEY, '1');
  return true;
}

/**
 * dropAddressConnections takes the connections an older build saved by their
 * address off the list for good and returns their names, so the app can say
 * once that those instances are added again with the phrase.
 */
export async function dropAddressConnections(): Promise<string[]> {
  const { kept, dropped } = splitSaved(await readList());
  if (dropped.length === 0) return [];
  await saveList(kept);
  const active = await getActiveConnectionId();
  if (active && !kept.some((c) => c.id === active)) await setActiveConnectionId(null);
  return dropped;
}

async function saveList(list: ServerConnection[]): Promise<void> {
  await SecureStore.setItemAsync(LIST_KEY, JSON.stringify(list));
}

export async function addConnection(conn: ServerConnection): Promise<void> {
  const list = await listConnections();
  await saveList([...list.filter((c) => c.id !== conn.id), conn]);
  // Saving an instance by hand, by joining with the phrase again, takes back
  // its removal.
  const dismissed = await listDismissed();
  if (dismissed.includes(conn.instanceId)) await saveDismissed(dismissed.filter((id) => id !== conn.instanceId));
}

export async function listDismissed(): Promise<string[]> {
  try {
    const parsed = JSON.parse((await SecureStore.getItemAsync(DISMISSED_KEY)) ?? '[]');
    return Array.isArray(parsed) ? parsed.filter((id): id is string => typeof id === 'string') : [];
  } catch {
    return [];
  }
}

async function saveDismissed(ids: string[]): Promise<void> {
  if (ids.length > 0) await SecureStore.setItemAsync(DISMISSED_KEY, JSON.stringify(ids));
  else await SecureStore.deleteItemAsync(DISMISSED_KEY);
}

/** dismissConnection removes a connection somebody removed on this phone, and
 *  keeps its instance off the list while it stays in the group. */
export async function dismissConnection(id: string): Promise<void> {
  const conn = (await listConnections()).find((c) => c.id === id);
  await removeConnection(id);
  if (!conn) return;
  const dismissed = await listDismissed();
  if (!dismissed.includes(conn.instanceId)) await saveDismissed([...dismissed, conn.instanceId]);
}

/** followGroupChange saves the instances a group gained, drops the
 *  connections of those it lost and renames the rest, in one write. */
export async function followGroupChange(
  added: ServerConnection[],
  dropped: string[],
  renamed: Map<string, string>,
): Promise<void> {
  const list = await listConnections();
  const kept = list.filter((c) => !dropped.includes(c.id)).map((c) => ({ ...c, name: renamed.get(c.id) ?? c.name }));
  await saveList([...kept, ...added]);
  const active = await getActiveConnectionId();
  if (active && dropped.includes(active)) await setActiveConnectionId(null);
}

export async function removeConnection(id: string): Promise<void> {
  const list = await listConnections();
  const remaining = list.filter((c) => c.id !== id);
  await saveList(remaining);
  releaseRelayFor(list.find((c) => c.id === id), remaining);
  const active = await getActiveConnectionId();
  if (active === id) await setActiveConnectionId(null);
}

// A removed relay connection's socket goes too, once nothing else needs it.
// One relay client is shared by every connection through the same relay and key
// (see api/relayClient.ts), so removing one of two instances behind one relay
// has to leave the transport the other uses open.
function releaseRelayFor(removed: ServerConnection | undefined, remaining: ServerConnection[]): void {
  if (!removed) return;
  const stillUsed = remaining.some(
    (c) => c.relayUrl === removed.relayUrl && c.relayKey === removed.relayKey,
  );
  if (!stillUsed) closeRelayClient(removed.relayUrl, removed.relayKey);
}

// removeGroupConnections drops every connection into one group, which an
// instance ends when it takes this phone out of the group.
export async function removeGroupConnections(relayUrl: string, relayKey: string): Promise<void> {
  const list = await listConnections();
  const inGroup = (c: ServerConnection) => c.relayUrl === relayUrl && c.relayKey === relayKey;
  await saveList(list.filter((c) => !inGroup(c)));
  closeRelayClient(relayUrl, relayKey);
  const active = await getActiveConnectionId();
  if (active && list.some((c) => c.id === active && inGroup(c))) await setActiveConnectionId(null);
}

// removeAllConnections is Settings' start-over action: every saved key gone
// from this device in one step.
export async function removeAllConnections(): Promise<void> {
  await saveList([]);
  await saveDismissed([]);
  await setActiveConnectionId(null);
  // Nothing is left to keep a relay socket open for. Without this, removing
  // every connection would leave sockets retrying against relays whose keys
  // this device no longer stores.
  closeAllRelayClients();
}

export async function getActiveConnectionId(): Promise<string | null> {
  return (await SecureStore.getItemAsync(ACTIVE_KEY)) || null;
}

export async function setActiveConnectionId(id: string | null): Promise<void> {
  if (id) await SecureStore.setItemAsync(ACTIVE_KEY, id);
  else await SecureStore.deleteItemAsync(ACTIVE_KEY);
}

export async function getDefaultConnectionId(): Promise<string | null> {
  return (await SecureStore.getItemAsync(DEFAULT_KEY)) || null;
}

export async function setDefaultConnectionId(id: string): Promise<void> {
  await SecureStore.setItemAsync(DEFAULT_KEY, id);
}

/**
 * loadDefaultConnection is the instance the app opens on and takes its look
 * from: the one marked with the star, or the first in the list while none is,
 * so removing the default hands the role to the next one.
 */
export async function loadDefaultConnection(): Promise<ServerConnection | null> {
  const [list, id] = await Promise.all([listConnections(), getDefaultConnectionId()]);
  return list.find((c) => c.id === id) ?? list[0] ?? null;
}

// loadActiveConnection resolves the saved pointer against the current list in
// one call. A pointer into a connection removed elsewhere sends the user to the
// connections list rather than crashing.
export async function loadActiveConnection(): Promise<ServerConnection | null> {
  const [list, activeId] = await Promise.all([listConnections(), getActiveConnectionId()]);
  if (!activeId) return null;
  return list.find((c) => c.id === activeId) ?? null;
}
