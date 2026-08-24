import AsyncStorage from '@react-native-async-storage/async-storage';

// Not a secret, unlike storage/connections.ts - a plain preference, so
// AsyncStorage rather than the OS keychain.
const KEY = 'knightloader-language-override';

/** null means "no override" - follow the device's own language setting. */
export async function getLanguageOverride(): Promise<string | null> {
  return AsyncStorage.getItem(KEY);
}

export async function setLanguageOverride(code: string | null): Promise<void> {
  if (code) await AsyncStorage.setItem(KEY, code);
  else await AsyncStorage.removeItem(KEY);
}
