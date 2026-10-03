// Taking downloads over from the browser and finding the media a page plays.
// Shared by the service worker (takeover.js, media.js), the options page and
// the popup. The rules are plain functions so check-capture.mjs can run them
// without a browser.
//
// Both features are off until switched on, and each asks for its permissions
// at that moment: nothing here is granted at install.

/** What taking over downloads needs: the downloads themselves, the site's
 *  cookies for each one, the note saying where it went, and the site access
 *  cookies are read under. */
const TAKEOVER_ACCESS = { permissions: ['downloads', 'cookies', 'notifications'], origins: ['<all_urls>'] };

/** What finding media needs: watching the requests a page makes, and the
 *  cookies a stream is sent with. */
const MEDIA_ACCESS = { permissions: ['webRequest', 'cookies'], origins: ['<all_urls>'] };

const TAKEOVER_DEFAULTS = { types: '', minSizeMb: 10, skip: '', bypassKey: 'alt' };
const TAKEOVER_MIN_SIZES = [0, 1, 10, 50, 100, 500];
const BYPASS_KEYS = ['alt', 'shift', 'ctrl'];

/** The stored rules, with defaults for anything missing or malformed. */
async function readTakeoverRules() {
  const { takeoverRules } = await chrome.storage.local.get('takeoverRules');
  const r = { ...TAKEOVER_DEFAULTS, ...(takeoverRules && typeof takeoverRules === 'object' ? takeoverRules : {}) };
  return {
    types: String(r.types ?? ''),
    minSizeMb: TAKEOVER_MIN_SIZES.includes(Number(r.minSizeMb)) ? Number(r.minSizeMb) : TAKEOVER_DEFAULTS.minSizeMb,
    skip: String(r.skip ?? ''),
    bypassKey: BYPASS_KEYS.includes(r.bypassKey) ? r.bypassKey : TAKEOVER_DEFAULTS.bypassKey,
  };
}

async function writeTakeoverRules(patch) {
  const next = { ...(await readTakeoverRules()), ...patch };
  await chrome.storage.local.set({ takeoverRules: next });
  return next;
}

/** A feature's state, as cnlState has it: `wanted` is the stored switch, off
 *  unless someone turned it on; `on` also needs what the browser granted. */
async function featureState(key, access) {
  const stored = await chrome.storage.local.get(key);
  const wanted = stored[key] === true;
  let granted = false;
  try {
    granted = wanted && (await chrome.permissions.contains(access));
  } catch {
    // A permission this browser does not know counts as not granted.
  }
  return { wanted, on: granted };
}

const takeoverState = () => featureState('takeoverEnabled', TAKEOVER_ACCESS);
const mediaState = () => featureState('mediaEnabled', MEDIA_ACCESS);

/**
 * Asks for a feature's access and stores the answer as its switch. Nothing may
 * be awaited before this in a click handler, or the browser stops counting
 * the click as the user's.
 */
async function requestFeature(key, access) {
  const granted = await chrome.permissions.request(access).catch(() => false);
  await chrome.storage.local.set({ [key]: granted });
  return granted;
}

/**
 * Switches a feature off and hands back what no other feature still uses.
 * Click'n'Load, taking downloads over and finding media share the access to
 * all websites, and the last two share the cookies.
 */
async function releaseFeature(key) {
  await chrome.storage.local.set({ [key]: false });
  const [cnl, takeover, media] = await Promise.all([cnlState(), takeoverState(), mediaState()]);
  const keep = new Set();
  if (cnl.on) CNL_ACCESS.origins.forEach((o) => keep.add(o));
  for (const [state, access] of [
    [takeover, TAKEOVER_ACCESS],
    [media, MEDIA_ACCESS],
  ]) {
    if (!state.on) continue;
    access.permissions.forEach((p) => keep.add(p));
    access.origins.forEach((o) => keep.add(o));
  }
  const all = { permissions: [...new Set([...TAKEOVER_ACCESS.permissions, ...MEDIA_ACCESS.permissions])], origins: ['<all_urls>'] };
  const drop = {
    permissions: all.permissions.filter((p) => !keep.has(p)),
    origins: all.origins.filter((o) => !keep.has(o)),
  };
  if (drop.permissions.length || drop.origins.length) await chrome.permissions.remove(drop).catch(() => false);
}

/** A comma, space or line separated list, lower-cased, without dots in front
 *  and without duplicates. */
function parseList(text) {
  const out = [];
  for (const raw of String(text ?? '').split(/[\s,;]+/)) {
    const v = raw.trim().toLowerCase().replace(/^\.+/, '');
    if (v && !out.includes(v)) out.push(v);
  }
  return out;
}

/** The host of a URL, lower-cased, or '' for anything unparsable. */
function hostOf(url) {
  try {
    return new URL(url).hostname.toLowerCase();
  } catch {
    return '';
  }
}

/** Whether host is one of the domains or below one of them. */
function hostListed(host, domains) {
  return !!host && domains.some((d) => host === d || host.endsWith('.' + d));
}

/**
 * Whether host is this computer or the local network: loopback, private and
 * link-local addresses, a name without a dot, and the suffixes home networks
 * use. A download from there is often from KnightLoader itself, and the
 * instance could not reach it the same way anyway.
 */
function localHost(host) {
  const h = String(host).replace(/^\[|\]$/g, '').toLowerCase();
  if (!h || (!h.includes('.') && !h.includes(':'))) return true;
  if (/\.(local|lan|home|internal|localhost)$/.test(h) || h.endsWith('.home.arpa') || h === 'localhost') return true;
  const v4 = h.match(/^(\d+)\.(\d+)\.(\d+)\.(\d+)$/);
  if (v4) {
    const [a, b] = [Number(v4[1]), Number(v4[2])];
    return a === 10 || a === 127 || a === 0 || (a === 172 && b >= 16 && b <= 31) || (a === 192 && b === 168) || (a === 169 && b === 254) || (a === 100 && b >= 64 && b <= 127);
  }
  if (h.includes(':')) return h === '::1' || /^f[cd]/.test(h) || /^fe[89ab]/.test(h);
  return false;
}

/** The file name a download goes by: the browser's choice where it has one,
 *  else the last part of the address. */
function downloadName(item) {
  const fromBrowser = String(item.filename ?? '').split(/[\\/]/).pop();
  if (fromBrowser) return fromBrowser;
  try {
    return decodeURIComponent(new URL(item.finalUrl || item.url).pathname.split('/').pop() || '');
  } catch {
    return '';
  }
}

/**
 * Whether a download the browser started should go to KnightLoader instead,
 * and if not, why. `item` is the browser's DownloadItem. A size the browser
 * does not know yet never passes a minimum, so an unsure case stays in the
 * browser.
 */
function takeoverVerdict(item, rules, now = Date.now(), bypassUntil = 0) {
  const url = String(item.url ?? '');
  if (!/^https?:\/\//i.test(url)) return { take: false, why: 'scheme' };
  if (item.incognito) return { take: false, why: 'private' };
  if (item.byExtensionId) return { take: false, why: 'extension' };
  if (now < bypassUntil) return { take: false, why: 'bypass' };
  const hosts = [url, item.finalUrl, item.referrer].map(hostOf).filter(Boolean);
  if (hosts.some(localHost)) return { take: false, why: 'local' };
  if (hosts.some((h) => hostListed(h, parseList(rules.skip)))) return { take: false, why: 'skipped' };
  const types = parseList(rules.types);
  if (types.length) {
    const name = downloadName(item).toLowerCase();
    if (!types.some((ext) => name.endsWith('.' + ext))) return { take: false, why: 'type' };
  }
  const min = Number(rules.minSizeMb) * 1024 * 1024;
  if (min > 0) {
    const size = Number(item.totalBytes) > 0 ? Number(item.totalBytes) : Number(item.fileSize);
    if (!(size >= min)) return { take: false, why: 'size' };
  }
  return { take: true, why: '' };
}

/** The Cookie header a browser would send, from chrome.cookies.getAll. */
function cookieHeader(cookies) {
  return (cookies ?? [])
    .filter((c) => c.name && !/[\r\n;]/.test(c.name + c.value))
    .map((c) => `${c.name}=${c.value}`)
    .join('; ');
}

/**
 * The headers a link is handed over with: the cookies for its own address, the
 * page it came from and the browser's user agent. Empty values are left out,
 * and the server keeps them to that link's origin and that one download.
 */
function handedHeaders({ cookie, referrer, userAgent }) {
  const out = {};
  if (cookie) out.Cookie = cookie;
  if (/^https?:\/\//i.test(referrer ?? '')) out.Referer = referrer;
  if (userAgent) out['User-Agent'] = userAgent;
  return out;
}

const HLS_TYPES = ['application/vnd.apple.mpegurl', 'application/x-mpegurl', 'audio/mpegurl', 'audio/x-mpegurl'];
const DASH_TYPES = ['application/dash+xml'];
const VIDEO_EXT = ['mp4', 'm4v', 'webm', 'mkv', 'mov', 'ogv'];
const AUDIO_EXT = ['mp3', 'm4a', 'ogg', 'oga', 'opus', 'flac', 'wav'];

/**
 * What a request a page made is, as media worth sending: 'hls' or 'dash' for a
 * streaming playlist, 'video' or 'audio' for a file a player element loads, or
 * '' for anything else. A player's fragments (.ts, .m4s, and the byte ranges a
 * script fetches) are left out, since the playlist stands for all of them.
 */
function mediaKind(url, contentType, requestType) {
  let path = '';
  try {
    path = new URL(url).pathname.toLowerCase();
  } catch {
    return '';
  }
  const mime = String(contentType ?? '').split(';')[0].trim().toLowerCase();
  const ext = path.includes('.') ? path.split('.').pop() : '';
  if (ext === 'm3u8' || HLS_TYPES.includes(mime)) return 'hls';
  if (ext === 'mpd' || DASH_TYPES.includes(mime)) return 'dash';
  if (requestType !== 'media') return '';
  if (ext === 'ts' || ext === 'm4s' || mime === 'video/mp2t') return '';
  if (VIDEO_EXT.includes(ext) || mime.startsWith('video/')) return 'video';
  if (AUDIO_EXT.includes(ext) || mime.startsWith('audio/')) return 'audio';
  return '';
}

/** Where the media a tab played is kept, in session storage, which the
 *  browser clears when it closes and content scripts cannot read. */
function mediaKey(tabId) {
  return `media:${tabId}`;
}

/** How many streams one tab keeps. A page that plays more is a feed, and the
 *  first ones are the ones the reader saw. */
const MEDIA_PER_TAB = 30;

/** Adds one stream to a tab's list unless it is there already. */
function withMedia(list, entry) {
  const items = Array.isArray(list?.items) ? list.items : [];
  if (items.some((m) => m.url === entry.url) || items.length >= MEDIA_PER_TAB) return list;
  return { page: list?.page ?? '', items: [...items, entry] };
}
