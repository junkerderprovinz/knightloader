import * as SecureStore from 'expo-secure-store';
import type { ServerConnection } from '../api/types';

// The API token is a bearer secret with the same reach as the account it
// belongs to, so it goes in the OS keychain (SecureStore), never
// AsyncStorage/plain storage. Only one saved connection for v1 - multi-server
// switching can reuse this key under a list later without changing the shape
// callers see.
const KEY = 'knightloader-connection';

export async function loadConnection(): Promise<ServerConnection | null> {
  const raw = await SecureStore.getItemAsync(KEY);
  if (!raw) return null;
  try {
    return JSON.parse(raw) as ServerConnection;
  } catch {
    return null;
  }
}

export async function saveConnection(conn: ServerConnection): Promise<void> {
  await SecureStore.setItemAsync(KEY, JSON.stringify(conn));
}

export async function clearConnection(): Promise<void> {
  await SecureStore.deleteItemAsync(KEY);
}
