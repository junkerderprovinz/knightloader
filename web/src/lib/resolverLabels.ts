import type { TranslationKey } from './i18n';

type Translate = (key: TranslationKey) => string;

// How a backend reads wherever it is named: the task row's badge, the volume
// chart's legend and the priority card. A product name stays as it is; a
// backend named by what it does goes through the locale files.
const PRODUCT_NAMES: Record<string, string> = {
  torbox: 'TorBox',
  alldebrid: 'AllDebrid',
  realdebrid: 'Real-Debrid',
  ytdlp: 'yt-dlp',
  jd: 'JDownloader',
};

const DESCRIBED: Record<string, TranslationKey> = {
  direct: 'resolver.direct',
  http: 'resolver.http',
  torrent: 'resolver.torrent',
};

/** resolverLabel names a backend, or returns the id itself when it is unknown,
 *  so a legend entry is never blank. */
export function resolverLabel(id: string, t: Translate): string {
  const key = DESCRIBED[id];
  return key ? t(key) : (PRODUCT_NAMES[id] ?? id);
}
