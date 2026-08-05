import { en, type Dict } from './en';

// Loaders, not dictionaries: 26 languages eagerly bundled would make every
// visitor download 25 they will never read. Each import() becomes its own chunk
// and is fetched when that language is actually chosen.
//
// English is the exception — it is bundled, because it is both the most likely
// choice and the fallback while another language is still on the wire.
const LOADERS: Record<string, () => Promise<Dict>> = {
  en: async () => en,
  de: async () => (await import('./de')).de,
  fr: async () => (await import('./fr')).fr,
  es: async () => (await import('./es')).es,
  it: async () => (await import('./it')).it,
  pt: async () => (await import('./pt')).pt,
  nl: async () => (await import('./nl')).nl,
  pl: async () => (await import('./pl')).pl,
  ru: async () => (await import('./ru')).ru,
  uk: async () => (await import('./uk')).uk,
  cs: async () => (await import('./cs')).cs,
  sv: async () => (await import('./sv')).sv,
  da: async () => (await import('./da')).da,
  fi: async () => (await import('./fi')).fi,
  no: async () => (await import('./no')).no,
  tr: async () => (await import('./tr')).tr,
  el: async () => (await import('./el')).el,
  hu: async () => (await import('./hu')).hu,
  ro: async () => (await import('./ro')).ro,
  ja: async () => (await import('./ja')).ja,
  ko: async () => (await import('./ko')).ko,
  zh: async () => (await import('./zh')).zh,
  ar: async () => (await import('./ar')).ar,
  he: async () => (await import('./he')).he,
  th: async () => (await import('./th')).th,
  vi: async () => (await import('./vi')).vi,
};

/** The codes that actually have a translation, in catalogue order. */
export const AVAILABLE = Object.keys(LOADERS);

const cache: Record<string, Dict> = { en };

/** loaded returns a dictionary already in memory, or undefined. */
export const loaded = (code: string): Dict | undefined => cache[code];

/** load fetches a language's dictionary, caching it for the rest of the session. */
export async function load(code: string): Promise<Dict> {
  if (cache[code]) return cache[code];
  const loader = LOADERS[code];
  if (!loader) return en;
  const dict = await loader();
  cache[code] = dict;
  return dict;
}
