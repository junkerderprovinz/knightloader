import * as SecureStore from 'expo-secure-store';
import { getRandomBytes } from 'expo-crypto';
import { fromHex } from '../api/sha256';
import { fingerprint, memberId, memberPublicKey } from '../api/memberKey';

// This device's own id on a relay, and the member key it signs its calls with.
//
// The id is made from the key (relay.MemberID): "phone-" and the hex of the
// key's SHA-256. An instance takes a call under that id only with a signature
// by that key, so nobody else can speak for this phone, and the relay can
// tell a reconnecting phone from somebody announcing its id. The key's seed
// lives in the keychain, and the id follows from it on every launch.
const MEMBER_KEY = 'knightloader-member-key';

export interface RelayIdentity {
  id: string;
  /** The 32-byte seed of the member key, or null where the keychain cannot
   *  hold one; the phone then calls unsigned, and every instance refuses it
   *  with a reason. */
  seed: Uint8Array | null;
}

let cached: RelayIdentity | null = null;
let inFlight: Promise<RelayIdentity> | null = null;

const toHex = (b: Uint8Array): string => Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('');

/**
 * Returns this device's relay id and member key, making the key on first use.
 *
 * Concurrent callers share one generation rather than racing to write
 * different keys: api/client.ts calls this on every relay request, and several
 * screens fire their first request at once.
 */
export function relayIdentity(): Promise<RelayIdentity> {
  if (cached) return Promise.resolve(cached);
  if (inFlight) return inFlight;
  inFlight = (async () => {
    let seed: Uint8Array | null = null;
    try {
      const kept = JSON.parse((await SecureStore.getItemAsync(MEMBER_KEY)) ?? 'null') as { seed?: string } | null;
      if (kept?.seed && kept.seed.length === 64) seed = fromHex(kept.seed);
      if (!seed) {
        const fresh = getRandomBytes(32);
        await SecureStore.setItemAsync(MEMBER_KEY, JSON.stringify({ seed: toHex(fresh) }));
        seed = fresh;
      }
    } catch {
      seed = null;
    }
    // Without a key there is nothing to make an id from, so this launch goes
    // by a random one of the same shape.
    const id = seed ? memberId('mobile', memberPublicKey(seed)) : `phone-${toHex(getRandomBytes(32))}`;
    cached = { id, seed };
    return cached;
  })().finally(() => {
    inFlight = null;
  });
  return inFlight;
}

/**
 * Forgets this device's member key, and with it its relay id, for a phone
 * taken out of its group: scanning the phrase again then brings it back as a
 * new phone rather than the removed one, whose key the group has revoked.
 */
export async function resetRelayIdentity(): Promise<void> {
  cached = null;
  try {
    await SecureStore.deleteItemAsync(MEMBER_KEY);
  } catch {
    // A key that could not be forgotten keeps its revoked id; the phone is out
    // of the group either way and is told so on its next call.
  }
}

/** This device's identity in the shape RelayClientOptions takes it. */
export async function memberOptions(): Promise<{ selfId: string; memberSeed: Uint8Array | null }> {
  const { id, seed } = await relayIdentity();
  return { selfId: id, memberSeed: seed };
}

/** The fingerprint of this device's member key, which an instance shows beside
 *  it while it waits for approval; null where the keychain holds no key. */
export async function ownFingerprint(): Promise<string | null> {
  const { seed } = await relayIdentity();
  return seed ? fingerprint(memberPublicKey(seed)) : null;
}
