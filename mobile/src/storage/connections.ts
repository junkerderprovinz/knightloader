import * as SecureStore from 'expo-secure-store';
import type { ServerConnection } from '../api/types';
import { closeAllRelayClients, closeRelayClient } from '../api/relayClient';

// Every saved connection carries an API token - a bearer secret with the
// same reach as the account it belongs to - so the whole list goes in the
// OS keychain (SecureStore), never AsyncStorage/plain storage, same as the
// single connection this replaced. The active-connection pointer is not a
// secret on its own, but it lives here too rather than in a second storage
// mechanism: it is meaningless without the list it points into.
const LIST_KEY = 'knightloader-connections';
const ACTIVE_KEY = 'knightloader-active-connection';

export async function listConnections(): Promise<ServerConnection[]> {
  const raw = await SecureStore.getItemAsync(LIST_KEY);
  if (!raw) return [];
  try {
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed) ? (parsed as ServerConnection[]) : [];
  } catch {
    return [];
  }
}

async function saveList(list: ServerConnection[]): Promise<void> {
  await SecureStore.setItemAsync(LIST_KEY, JSON.stringify(list));
}

export async function addConnection(conn: ServerConnection): Promise<void> {
  const list = await listConnections();
  await saveList([...list.filter((c) => c.id !== conn.id), conn]);
}

export async function removeConnection(id: string): Promise<void> {
  const list = await listConnections();
  const remaining = list.filter((c) => c.id !== id);
  await saveList(remaining);
  releaseRelayFor(list.find((c) => c.id === id), remaining);
  const active = await getActiveConnectionId();
  if (active === id) await setActiveConnectionId(null);
}

// A removed relay connection's socket has to go too - but only once nothing
// else still needs it. One relay client is shared by every connection through
// the same relay and key (see api/relayClient.ts), so removing one of two
// instances behind one relay must not close the transport the other is still
// using.
function releaseRelayFor(removed: ServerConnection | undefined, remaining: ServerConnection[]): void {
  if (!removed || removed.kind !== 'relay') return;
  const stillUsed = remaining.some(
    (c) => c.kind === 'relay' && c.relayUrl === removed.relayUrl && c.relayKey === removed.relayKey,
  );
  if (!stillUsed) closeRelayClient(removed.relayUrl, removed.relayKey);
}

// removeAllConnections is Settings' own "start over" action - every saved
// token gone from this device in one step, not one remove tap per
// connection.
export async function removeAllConnections(): Promise<void> {
  await saveList([]);
  await setActiveConnectionId(null);
  // Nothing is left to keep a relay socket open for, so none stays open -
  // otherwise "remove all connections" would leave sockets retrying against
  // relays whose keys this device no longer stores.
  closeAllRelayClients();
}

export async function getActiveConnectionId(): Promise<string | null> {
  return (await SecureStore.getItemAsync(ACTIVE_KEY)) || null;
}

export async function setActiveConnectionId(id: string | null): Promise<void> {
  if (id) await SecureStore.setItemAsync(ACTIVE_KEY, id);
  else await SecureStore.deleteItemAsync(ACTIVE_KEY);
}

// loadActiveConnection resolves the saved pointer against the current list
// in one call, since a stale pointer (its connection got removed elsewhere)
// is a "go to the connections list" case, not a crash.
export async function loadActiveConnection(): Promise<ServerConnection | null> {
  const [list, activeId] = await Promise.all([listConnections(), getActiveConnectionId()]);
  if (!activeId) return null;
  return list.find((c) => c.id === activeId) ?? null;
}
