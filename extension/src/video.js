// The download button on a video's player. This part is shared by the button
// itself (video-button.js, inside the site), the service worker
// (video-worker.js) and the options page: the sites it appears on, the address
// it sends for a page, what a click downloads there, where the button sits and
// when it shows, and its switch. The rules are plain functions so
// check-video.mjs can run them without a browser.

/**
 * The sites the button appears on, with the addresses their players live
 * under. Each is one of the big video sites yt-dlp on the instance knows
 * (knownSites in internal/resolver/ytdlp/hosts.go), and the list is short
 * and explicit, so the access the button asks for names these sites and no
 * others.
 */
const VIDEO_SITES = [
  {
    id: 'youtube',
    name: 'YouTube',
    origins: ['https://www.youtube.com/*', 'https://m.youtube.com/*', 'https://www.youtube-nocookie.com/*'],
  },
  { id: 'vimeo', name: 'Vimeo', origins: ['https://vimeo.com/*'] },
  { id: 'dailymotion', name: 'Dailymotion', origins: ['https://www.dailymotion.com/*'] },
  { id: 'twitch', name: 'Twitch', origins: ['https://www.twitch.tv/*', 'https://clips.twitch.tv/*'] },
  { id: 'bilibili', name: 'Bilibili', origins: ['https://www.bilibili.com/*'] },
];

/** The resolutions POST /api/links takes as `format` (ytdlp.Qualities). */
const VIDEO_CAPS = ['4320p', '2160p', '1440p', '1080p', '720p', '480p', '360p', '240p', '144p'];

/** The rows a video becomes on the instance (ytdlp.Variants), in the order
 *  the menu and the options page list them. */
const VIDEO_PARTS = ['video', 'audio', 'subtitle', 'thumbnail', 'description'];

/** The formats the audio row converts to (ytdlp.AudioFormats), "best" keeping
 *  the source's own. */
const VIDEO_AUDIO_FORMATS = ['best', 'aac', 'alac', 'flac', 'mp3', 'opus', 'vorbis', 'wav'];

/** What a click downloads on a site nothing is set for: the video at its
 *  best. */
const VIDEO_DEFAULTS = { parts: ['video'], quality: 'best', audio: 'best' };

/**
 * A pick of parts and qualities from what is stored or what a page sent,
 * with every value the instance would refuse dropped: parts it has no row
 * for, and a quality or audio format it does not know, which become the best.
 * A pick with no part left is the defaults.
 */
function videoPick(raw) {
  const parts = Array.isArray(raw?.parts) ? VIDEO_PARTS.filter((p) => raw.parts.includes(p)) : [];
  if (parts.length === 0) return { ...VIDEO_DEFAULTS };
  return {
    parts,
    quality: VIDEO_CAPS.includes(raw.quality) ? raw.quality : 'best',
    audio: VIDEO_AUDIO_FORMATS.includes(raw.audio) ? raw.audio : 'best',
  };
}

/** What a click downloads on a site, from the stored defaults per site. */
function videoDefaultsFor(stored, siteId) {
  return videoPick(stored?.videoDefaults?.[siteId]);
}

/** The choice POST /api/links takes for a pick (ytdlp.LinkChoice). */
function videoChoice(pick) {
  return { variants: pick.parts, quality: pick.quality, audioFormat: pick.audio };
}

/**
 * What the menu offers for what the instance says a video has (GET
 * /api/ytdlp/link): the resolutions it has, highest first, and whether it
 * has sound. Without an answer, none and `unknown` set.
 */
function videoOffer(answer) {
  if (!answer) return { qualities: [], audio: true, unknown: true };
  const qualities = Array.isArray(answer.qualities) ? VIDEO_CAPS.filter((q) => answer.qualities.includes(q)) : [];
  return { qualities, audio: answer.audio === true };
}

/** The resolutions offered for a site: those the instance has found there so
 *  far (videoQualities, by site), or every one it knows where it has found
 *  none. */
function videoSiteQualities(stored, siteId) {
  const seen = stored?.videoQualities?.[siteId];
  const known = Array.isArray(seen) ? VIDEO_CAPS.filter((q) => seen.includes(q)) : [];
  return known.length ? known : VIDEO_CAPS;
}

/**
 * Where the button's middle goes for a spot stored as a share of the
 * player's width and height, kept inside the player with the whole button in
 * view. A player smaller than the button gets it in its middle.
 */
function videoSpotPoint(spot, rect, size) {
  const along = (start, length, share, half) => {
    const room = Math.max(0, length / 2 - half);
    return start + length / 2 + Math.min(room, Math.max(-room, share * length - length / 2));
  };
  return { x: along(rect.left, rect.width, spot.x, size.width / 2), y: along(rect.top, rect.height, spot.y, size.height / 2) };
}

/** The spot a point in the player is stored as: where the button's middle
 *  lands, clamped like videoSpotPoint, as shares of the player rounded to a
 *  ten-thousandth. */
function videoSpotOf(point, rect, size) {
  const p = videoSpotPoint({ x: (point.x - rect.left) / rect.width, y: (point.y - rect.top) / rect.height }, rect, size);
  const share = (v) => Math.round(v * 10000) / 10000;
  return { x: share((p.x - rect.left) / rect.width), y: share((p.y - rect.top) / rect.height) };
}

/** The spot stored for a site (videoButtonSpot, by site), or null where the
 *  button keeps its place at the top of the video. */
function videoSpotFor(stored, siteId) {
  const spot = stored?.videoButtonSpot?.[siteId];
  const share = (v) => typeof v === 'number' && v >= 0 && v <= 1;
  return share(spot?.x) && share(spot?.y) ? { x: spot.x, y: spot.y } : null;
}

/**
 * Whether the button shows. While it is in use it always does: the pointer
 * or the keyboard on it, its menu open, a drag or a send going on. Otherwise
 * it shows wherever its player is in view, or with auto-hide on only while
 * the pointer moves over the video, as the player's own controls do.
 */
function videoButtonShows(s) {
  if (s.hovering || s.focused || s.menuOpen || s.dragging || s.busy) return true;
  return !s.autoHide || s.pointerOver;
}

/** Each site by the host names of its addresses. */
const VIDEO_HOSTS = new Map(VIDEO_SITES.flatMap((s) => s.origins.map((o) => [new URL(o.slice(0, -1)).hostname, s])));

/** The site an address belongs to, or undefined. */
function videoSiteOf(href) {
  let u;
  try {
    u = new URL(href);
  } catch {
    return undefined;
  }
  return u.protocol === 'https:' ? VIDEO_HOSTS.get(u.hostname) : undefined;
}

const YOUTUBE_ID = /^[A-Za-z0-9_-]{11}$/;

// Watch later and the liked videos are lists only their owner sees signed in,
// which the instance is not.
const YOUTUBE_PRIVATE_LISTS = new Set(['WL', 'LL', 'LM']);

function youtubePage(u) {
  const v = u.pathname === '/watch' ? (u.searchParams.get('v') ?? '') : (u.pathname.match(/^\/(?:shorts|live|embed)\/([^/]+)/)?.[1] ?? '');
  if (!YOUTUBE_ID.test(v)) return null;
  const list = u.searchParams.get('list') ?? '';
  const playlist = /^[A-Za-z0-9_-]{2,64}$/.test(list) && !YOUTUBE_PRIVATE_LISTS.has(list) ? `https://www.youtube.com/playlist?list=${list}` : '';
  return { url: `https://www.youtube.com/watch?v=${v}`, playlist };
}

function vimeoPage(u) {
  const m =
    u.pathname.match(/^\/(\d+)(?:\/([0-9a-f]{6,}))?\/?$/) ??
    u.pathname.match(/^\/channels\/[^/]+\/(\d+)\/?$/) ??
    u.pathname.match(/^\/groups\/[^/]+\/videos\/(\d+)\/?$/);
  if (!m) return null;
  // The second part is the key of an unlisted video, which the video does not
  // play without.
  return { url: `https://vimeo.com/${m[1]}${m[2] ? `/${m[2]}` : ''}`, playlist: '' };
}

function dailymotionPage(u) {
  const id = u.pathname.match(/^\/video\/([A-Za-z0-9]+)/)?.[1];
  return id ? { url: `https://www.dailymotion.com/video/${id}`, playlist: '' } : null;
}

function twitchPage(u) {
  if (u.hostname === 'clips.twitch.tv') {
    const slug = u.pathname.match(/^\/([A-Za-z0-9_-]+)\/?$/)?.[1];
    return slug && slug !== 'embed' ? { url: `https://clips.twitch.tv/${slug}`, playlist: '' } : null;
  }
  const vod = u.pathname.match(/^\/videos\/(\d+)/)?.[1];
  if (vod) return { url: `https://www.twitch.tv/videos/${vod}`, playlist: '' };
  const clip = u.pathname.match(/^\/[^/]+\/clip\/([A-Za-z0-9_-]+)/)?.[1];
  return clip ? { url: `https://clips.twitch.tv/${clip}`, playlist: '' } : null;
}

function bilibiliPage(u) {
  const id = u.pathname.match(/^\/video\/(BV[0-9A-Za-z]{10}|av\d+)/)?.[1];
  if (!id) return null;
  // A video in several parts plays one of them; the first needs no number.
  const part = Number(u.searchParams.get('p'));
  return { url: `https://www.bilibili.com/video/${id}${Number.isInteger(part) && part > 1 ? `?p=${part}` : ''}`, playlist: '' };
}

const VIDEO_PAGES = { youtube: youtubePage, vimeo: vimeoPage, dailymotion: dailymotionPage, twitch: twitchPage, bilibili: bilibiliPage };

/**
 * The video an address plays: `url` is the one address the video goes by,
 * whatever form the page was opened in, so the instance sees the same link
 * from a Short, an embedded player and the watch page, and `playlist` the
 * list the page plays it in, or ''. Null for a page that plays no one video.
 */
function videoPage(href) {
  const site = videoSiteOf(href);
  if (!site) return null;
  const page = VIDEO_PAGES[site.id](new URL(href));
  return page ? { site: site.id, ...page } : null;
}

/** Whether the stored switches show the button on a site. Off means the
 *  switch was turned off; nothing stored means on. */
function videoButtonWanted(stored, siteId) {
  const hidden = Array.isArray(stored?.videoButtonHidden) ? stored.videoButtonHidden : [];
  return stored?.videoButton !== false && !hidden.includes(siteId);
}

/** The access a list of sites needs. */
function videoAccess(siteIds) {
  return { origins: VIDEO_SITES.filter((s) => siteIds.includes(s.id)).flatMap((s) => s.origins) };
}

/**
 * The button's state, as cnlState has it for Click'n'Load: `wanted` is the
 * switch, `shown` the sites it is not hidden on, `granted` those the browser
 * gave access to, and `on` whether it runs anywhere. Wanted without access is
 * the state the options page asks the browser to fix.
 */
async function videoButtonState() {
  const stored = await chrome.storage.local.get(['videoButton', 'videoButtonHidden']);
  const wanted = stored.videoButton !== false;
  const shown = VIDEO_SITES.filter((s) => videoButtonWanted({ ...stored, videoButton: true }, s.id)).map((s) => s.id);
  const granted = [];
  for (const s of VIDEO_SITES) {
    if (await chrome.permissions.contains({ origins: s.origins })) granted.push(s.id);
  }
  const running = wanted ? shown.filter((id) => granted.includes(id)) : [];
  return { wanted, shown, granted, running, on: running.length > 0 };
}
