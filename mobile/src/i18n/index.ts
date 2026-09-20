import { en, type Dict } from './en';

// Loaders rather than dictionaries, the shape the web UI's lib/locales/index.ts
// uses, over the same 42-language catalogue, so the family offers one language
// list. On the web each language is a chunk fetched on demand; Metro has no
// equivalent for a native bundle, since every import() lands in the one JS
// bundle the APK ships, so here a language is evaluated rather than downloaded
// when it is selected. The shared interface is what lets this app grow a web
// build without a second load()/AVAILABLE story.
//
// English is bundled, being both the likeliest fallback and needed before any
// other chunk has loaded.
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
  bg: async () => (await import('./bg')).bg,
  sk: async () => (await import('./sk')).sk,
  sl: async () => (await import('./sl')).sl,
  hr: async () => (await import('./hr')).hr,
  sr: async () => (await import('./sr')).sr,
  lt: async () => (await import('./lt')).lt,
  lv: async () => (await import('./lv')).lv,
  et: async () => (await import('./et')).et,
  is: async () => (await import('./is')).is,
  ca: async () => (await import('./ca')).ca,
  gl: async () => (await import('./gl')).gl,
  eu: async () => (await import('./eu')).eu,
  id: async () => (await import('./id')).id,
  ms: async () => (await import('./ms')).ms,
  hi: async () => (await import('./hi')).hi,
  fa: async () => (await import('./fa')).fa,
};

/** The codes that actually have a translation. */
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
