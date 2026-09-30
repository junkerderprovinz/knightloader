// The name this phone goes by on the Instances page of every instance in its
// group: the one set in Settings, or else the device model Android reports,
// such as "Pixel 8".
import { Platform } from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import { closeAllRelayClients } from './relayClient';

const KEY = 'knightloader-device-name';

// Read once at startup, so the relay hello can take the name synchronously.
let chosen = '';

/** relay.MaxNameBytes: every hello has to fit the relay's first-frame limit. */
const MAX_NAME_BYTES = 200;

/** clipName cuts name to MAX_NAME_BYTES of UTF-8 at a whole character, as
 *  relay.ClipName does. */
export function clipName(name: string): string {
  let bytes = 0;
  let out = '';
  for (const ch of name) {
    const cp = ch.codePointAt(0)!;
    const size = cp < 0x80 ? 1 : cp < 0x800 ? 2 : cp < 0x10000 ? 3 : 4;
    if (bytes + size > MAX_NAME_BYTES) break;
    bytes += size;
    out += ch;
  }
  return out;
}

export function deviceName(): string {
  return chosen || modelName();
}

/** The name set in Settings, '' while the model name is in use. */
export function chosenDeviceName(): string {
  return chosen;
}

export async function loadDeviceName(): Promise<void> {
  chosen = (await AsyncStorage.getItem(KEY)) ?? '';
}

/**
 * setDeviceName stores name, or goes back to the model name for ''. The relay
 * only reads a name from a connection's hello, so the open connections are
 * closed and the next request opens them again under the new one.
 */
export async function setDeviceName(name: string): Promise<void> {
  const next = clipName(name.trim());
  if (next === chosen) return;
  chosen = next;
  if (next) await AsyncStorage.setItem(KEY, next);
  else await AsyncStorage.removeItem(KEY);
  closeAllRelayClients();
}

export function modelName(): string {
  const { Model, Manufacturer } = Platform.constants as { Model?: string; Manufacturer?: string };
  const model = (Model ?? '').trim();
  // Some makers put the brand into the model name, others leave it out.
  const maker = (Manufacturer ?? '').trim();
  const name = maker && model && !model.toLowerCase().startsWith(maker.toLowerCase()) ? `${maker} ${model}` : model;
  return clipName(name || 'Android');
}
