import AsyncStorage from '@react-native-async-storage/async-storage';

// This device's own id on a relay.
//
// The relay needs one before it will join a connection to a key at all, and it
// has to be STABLE: it is how the relay tells a reconnecting client from a
// second one, and joining under a fresh id every launch would leave the old
// one lingering in every sibling's view until it timed out. So it is generated
// once and kept.
//
// Not a secret - it identifies, it does not authorise, exactly like an
// instance's own InstanceID - so AsyncStorage rather than the keychain, same
// call as storage/languagePreference.ts. The relay KEY is the credential and
// lives with the connection in the keychain.
const KEY = 'knightloader-relay-identity';

let cached: string | null = null;
let inFlight: Promise<string> | null = null;

/**
 * Returns this device's relay id, generating and persisting one on first use.
 *
 * Concurrent callers share one generation rather than racing to write
 * different ids: api/client.ts calls this on every relay request, and several
 * screens fire their first request at once.
 */
export function relayIdentity(): Promise<string> {
  if (cached) return Promise.resolve(cached);
  if (inFlight) return inFlight;
  inFlight = (async () => {
    try {
      const stored = await AsyncStorage.getItem(KEY);
      if (stored) {
        cached = stored;
        return stored;
      }
    } catch {
      // Unreadable storage is not worth failing a connection over - fall
      // through and use a fresh id for this run.
    }
    const id = `phone-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
    try {
      await AsyncStorage.setItem(KEY, id);
    } catch {
      // Same reasoning: unwritable storage costs a stable id across launches,
      // not the ability to connect now.
    }
    cached = id;
    return id;
  })().finally(() => {
    inFlight = null;
  });
  return inFlight;
}
