import AsyncStorage from '@react-native-async-storage/async-storage';
import * as SecureStore from 'expo-secure-store';
import { getRandomBytes } from 'expo-crypto';
import { fromHex } from '../api/sha256';

// This device's own id on a relay, and the member key it signs its calls with.
//
// The relay needs an id before it joins a connection to a key, and it has to be
// stable: it is how the relay tells a reconnecting client from a second one,
// and a fresh id every launch would leave the old one in every sibling's view
// until it timed out. So it is generated once and kept.
//
// The id names this phone and the key proves it: an instance pins the key to
// the id on the first signed call and accepts no other key for it after that
// (internal/relay/member.go). The id is no secret and lives in AsyncStorage;
// the key's seed lives in the keychain with the id it was made for.
const KEY = 'knightloader-relay-identity';
const MEMBER_KEY = 'knightloader-member-key';
// Set once this phone has signed under its id. A restored backup brings
// AsyncStorage back without the keychain, and a new key under an id the group
// already pinned would be refused, so such a phone takes a new id as well.
const SIGNED_KEY = 'knightloader-member-signed';

export interface RelayIdentity {
  id: string;
  /** The 32-byte seed of the member key, or null where the keychain cannot
   *  hold one; the phone then calls unsigned, as a release before keys did. */
  seed: Uint8Array | null;
}

let cached: RelayIdentity | null = null;
let inFlight: Promise<RelayIdentity> | null = null;

const toHex = (b: Uint8Array): string => Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('');

const newId = () => `phone-${toHex(getRandomBytes(12))}`;

/**
 * Returns this device's relay id and member key, generating and persisting
 * them on first use.
 *
 * Concurrent callers share one generation rather than racing to write
 * different ids: api/client.ts calls this on every relay request, and several
 * screens fire their first request at once.
 */
export function relayIdentity(): Promise<RelayIdentity> {
  if (cached) return Promise.resolve(cached);
  if (inFlight) return inFlight;
  inFlight = (async () => {
    let id: string | null = null;
    let signed = false;
    try {
      id = await AsyncStorage.getItem(KEY);
      signed = (await AsyncStorage.getItem(SIGNED_KEY)) === '1';
    } catch {
      // Unreadable storage is not worth failing a connection over; a fresh id
      // for this run will do.
    }
    let seed: Uint8Array | null = null;
    try {
      const kept = JSON.parse((await SecureStore.getItemAsync(MEMBER_KEY)) ?? 'null') as { id?: string; seed?: string } | null;
      if (kept?.id && kept.seed && kept.id === id) seed = fromHex(kept.seed);
      if (!seed) {
        // A phone from before member keys keeps its id, so the group pins
        // the new key under the card it already has.
        if (!id || signed) id = newId();
        const fresh = getRandomBytes(32);
        await SecureStore.setItemAsync(MEMBER_KEY, JSON.stringify({ id, seed: toHex(fresh) }));
        seed = fresh;
      }
    } catch {
      seed = null;
    }
    if (!id) id = newId();
    try {
      await AsyncStorage.setItem(KEY, id);
      if (seed) await AsyncStorage.setItem(SIGNED_KEY, '1');
    } catch {
      // Unwritable storage costs a stable id across launches, not the ability
      // to connect now.
    }
    cached = { id, seed };
    return cached;
  })().finally(() => {
    inFlight = null;
  });
  return inFlight;
}

/**
 * Forgets this device's relay id and member key, for a phone taken out of its
 * group: scanning the phrase again then brings it back as a new phone rather
 * than the removed one, whose key the group has revoked.
 */
export async function resetRelayIdentity(): Promise<void> {
  cached = null;
  try {
    await AsyncStorage.removeItem(KEY);
    await AsyncStorage.removeItem(SIGNED_KEY);
    await SecureStore.deleteItemAsync(MEMBER_KEY);
  } catch {
    // An id that could not be forgotten is replaced on the next launch at the
    // latest; the phone is out of the group either way.
  }
}

/** This device's identity in the shape RelayClientOptions takes it. */
export async function memberOptions(): Promise<{ selfId: string; memberSeed: Uint8Array | null }> {
  const { id, seed } = await relayIdentity();
  return { selfId: id, memberSeed: seed };
}
