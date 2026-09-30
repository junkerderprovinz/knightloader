// The name this phone goes by on the Instances page of every instance in its
// group: the device model Android reports, such as "Pixel 8".
import { Platform } from 'react-native';

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
  const { Model, Manufacturer } = Platform.constants as { Model?: string; Manufacturer?: string };
  const model = (Model ?? '').trim();
  // Some makers put the brand into the model name, others leave it out.
  const maker = (Manufacturer ?? '').trim();
  const name = maker && model && !model.toLowerCase().startsWith(maker.toLowerCase()) ? `${maker} ${model}` : model;
  return clipName(name || 'Android');
}
