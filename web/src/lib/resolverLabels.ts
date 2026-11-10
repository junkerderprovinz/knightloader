import type { TranslationKey } from './i18n';

type Translate = (key: TranslationKey) => string;

// How a backend reads wherever it is named: the task row's badge, the volume
// chart's legend and the priority card. A product name stays as it is; a
// backend named by what it does goes through the locale files.
const PRODUCT_NAMES: Record<string, string> = {
  torbox: 'TorBox',
  alldebrid: 'AllDebrid',
  realdebrid: 'Real-Debrid',
  premiumize: 'Premiumize.me',
  debridlink: 'Debrid-Link',
  linksnappy: 'Linksnappy',
  offcloud: 'Offcloud',
  bestdebrid: 'BestDebrid',
  cocoleech: 'CocoLeech',
  cooldebrid: 'CoolDebrid',
  debriditalia: 'DebridItalia',
  deepbrid: 'Deepbrid',
  fakirdebrid: 'FakirDebrid',
  megadebrid: 'Mega-Debrid',
  multiup: 'MultiUp',
  mydebrid: 'MyDebrid',
  neodebrid: 'NeoDebrid',
  proleech: 'ProLeech',
  rpnet: 'RPNet',
  zevera: 'Zevera',
  ytdlp: 'yt-dlp',
  jd: 'JDownloader',
  mega: 'MEGA',
  '1fichier': '1fichier',
  rapidgator: 'Rapidgator',
  keep2share: 'Keep2Share',
  fileboom: 'FileBoom',
  tezfiles: 'TezFiles',
  nitroflare: 'Nitroflare',
  filer: 'filer.net',
  'rapidgator-free': 'Rapidgator',
  'keep2share-free': 'Keep2Share',
  'fileboom-free': 'FileBoom',
  'tezfiles-free': 'TezFiles',
  'filer-free': 'filer.net',
  ddownload: 'ddownload',
  katfile: 'KatFile',
  clicknupload: 'Clicknupload',
  filespace: 'FileSpace',
  uploadrar: 'Uploadrar',
  pixeldrain: 'Pixeldrain',
  gofile: 'Gofile',
  mediafire: 'Mediafire',
  googledrive: 'Google Drive',
  dropbox: 'Dropbox',
  onedrive: 'OneDrive',
  wetransfer: 'WeTransfer',
  swisstransfer: 'SwissTransfer',
  krakenfiles: 'Krakenfiles',
  workupload: 'Workupload',
  sendnow: 'Send.now',
};

const DESCRIBED: Record<string, TranslationKey> = {
  direct: 'resolver.direct',
  http: 'resolver.http',
  torrent: 'resolver.torrent',
  nntp: 'resolver.nntp',
};

/** resolverLabel names a backend, or returns the id itself when it is unknown,
 *  so a legend entry is never blank. */
export function resolverLabel(id: string, t: Translate): string {
  const key = DESCRIBED[id];
  return key ? t(key) : (PRODUCT_NAMES[id] ?? id);
}

// The debrid services of the accounts catalogue (internal/accounts), by the
// site whose mark stands for them: bundled in public/hosters where there is
// one, else fetched by the instance.
export const DEBRID_SITES: Readonly<Record<string, string>> = {
  torbox: 'torbox.app',
  alldebrid: 'alldebrid.com',
  realdebrid: 'real-debrid.com',
  debridlink: 'debrid-link.com',
  premiumize: 'premiumize.me',
  linksnappy: 'linksnappy.com',
  offcloud: 'offcloud.com',
  bestdebrid: 'bestdebrid.com',
  cocoleech: 'cocoleech.com',
  cooldebrid: 'cooldebrid.com',
  debriditalia: 'debriditalia.com',
  deepbrid: 'deepbrid.com',
  fakirdebrid: 'fakirdebrid.net',
  megadebrid: 'mega-debrid.eu',
  multiup: 'multiup.io',
  mydebrid: 'mydebrid.com',
  neodebrid: 'neodebrid.com',
  proleech: 'proleech.link',
  rpnet: 'rpnet.biz',
  zevera: 'zevera.com',
};
