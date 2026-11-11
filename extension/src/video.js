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

/** The codecs an audio track comes in (audioFamilies in
 *  internal/resolver/ytdlp/formats.go), which a site's default can prefer. */
const VIDEO_AUDIO_CODECS = ['aac', 'opus', 'vorbis', 'mp3', 'flac', 'alac'];

/** What a click downloads on a site nothing is set for: the video at its
 *  best. */
const VIDEO_DEFAULTS = { parts: ['video'], quality: 'best', audio: 'best', subLangs: [], subFormat: 'best' };

// The words the instance's choices are made of, as ytdlp.LinkChoice.Valid
// takes them: a language as yt-dlp reports one, a format, and the keys of a
// video and an audio track (VideoTracks and AudioTracks).
const VIDEO_LANG = /^[A-Za-z]{2,3}(-[A-Za-z0-9]{1,8}){0,2}$/;
const VIDEO_TOKEN = /^[a-z0-9]{1,10}$/;
const VIDEO_TRACK = /^[1-9][0-9]{1,4}p([1-9][0-9]{1,2})? [a-z0-9]{1,10}( [a-z0-9]{1,10})?$/;
const AUDIO_TRACK = /^[a-z0-9]{1,10} [1-9][0-9]{0,3}k$/;

/** Whether v is a string of the form re describes. A pattern alone would
 *  take undefined, which it reads as the word. */
function videoIs(re, v) {
  return typeof v === 'string' && re.test(v);
}

/**
 * A pick from what is stored or what a page sent, with every value the
 * instance would refuse dropped. The part of it a site's defaults hold says
 * what is wanted wherever a video has it: the parts, a cap on the
 * resolution, a codec for the audio, subtitle languages in order and a
 * subtitle format. The menu adds the tracks, the audio language and whether
 * generated subtitles count, from what one video has. A pick with no part
 * left is the defaults.
 */
function videoPick(raw) {
  const parts = Array.isArray(raw?.parts) ? VIDEO_PARTS.filter((p) => raw.parts.includes(p)) : [];
  if (parts.length === 0) return { ...VIDEO_DEFAULTS, subLangs: [] };
  const pick = {
    parts,
    quality: VIDEO_CAPS.includes(raw.quality) ? raw.quality : 'best',
    audio: VIDEO_AUDIO_CODECS.includes(raw.audio) ? raw.audio : 'best',
    subLangs: Array.isArray(raw.subLangs) ? raw.subLangs.filter((l) => videoIs(VIDEO_LANG, l)) : [],
    subFormat: videoIs(VIDEO_TOKEN, raw.subFormat) ? raw.subFormat : 'best',
  };
  if (videoIs(VIDEO_TRACK, raw.videoTrack)) pick.videoTrack = raw.videoTrack;
  if (videoIs(AUDIO_TRACK, raw.audioTrack)) pick.audioTrack = raw.audioTrack;
  if (videoIs(VIDEO_LANG, raw.audioLang)) pick.audioLang = raw.audioLang;
  if (typeof raw.subAuto === 'boolean') pick.subAuto = raw.subAuto;
  return pick;
}

/** What a click downloads on a site, from the stored defaults per site. */
function videoDefaultsFor(stored, siteId) {
  return videoPick(stored?.videoDefaults?.[siteId]);
}

/**
 * The choice POST /api/links takes for a pick (ytdlp.LinkChoice). A pick
 * without tracks leaves them to the instance, which takes the best under the
 * cap and the preferred codec's track where the video has one. Generated
 * subtitles count unless the menu said otherwise, so a video without
 * subtitles by hand still gets its own language's.
 */
function videoChoice(pick) {
  return {
    variants: pick.parts,
    quality: pick.quality,
    audioFormat: pick.audio,
    subtitleLangs: pick.subLangs,
    subtitleAuto: pick.subAuto ?? true,
    subtitleFormat: pick.subFormat,
    ...(pick.videoTrack ? { videoTrack: pick.videoTrack } : {}),
    ...(pick.audioTrack ? { audioTrack: pick.audioTrack } : {}),
    ...(pick.audioLang ? { audioLang: pick.audioLang } : {}),
  };
}

/**
 * What a video offers, from what the instance says it has (GET
 * /api/ytdlp/link): its video and audio tracks, the languages its audio
 * comes in, its subtitles by hand and generated and the formats they come
 * in, the picture the thumbnail row fetches and whether it has a
 * description. `known` is false where there is no answer, or one from an
 * instance that only lists resolutions, and then nothing is ruled out.
 */
function videoOffer(answer) {
  const list = (v, re) => (Array.isArray(v) ? v.filter((x) => videoIs(re, x)) : []);
  const qualities = Array.isArray(answer?.qualities) ? VIDEO_CAPS.filter((q) => answer.qualities.includes(q)) : [];
  if (!Array.isArray(answer?.videoTracks)) {
    return {
      known: false, qualities, audio: answer ? answer.audio === true : true, videoTracks: [], audioTracks: [], audioLangs: [],
      subtitles: [], autoCaptions: [], subtitleFormats: [], thumbnail: null, description: true,
    };
  }
  const th = answer.thumbnail;
  const size = (n) => (Number.isInteger(n) && n > 0 ? n : 0);
  return {
    known: true,
    qualities,
    audio: answer.audio === true,
    videoTracks: list(answer.videoTracks, VIDEO_TRACK),
    audioTracks: list(answer.audioTracks, AUDIO_TRACK),
    audioLangs: Array.isArray(answer.audioLangs)
      ? answer.audioLangs.filter((l) => videoIs(VIDEO_LANG, l?.code)).map((l) => ({ code: l.code, dubbed: l.dubbed === true }))
      : [],
    subtitles: list(answer.subtitles, VIDEO_LANG),
    autoCaptions: list(answer.autoCaptions, VIDEO_LANG),
    subtitleFormats: list(answer.subtitleFormats, VIDEO_TOKEN),
    thumbnail: videoIs(VIDEO_TOKEN, th?.ext) ? { width: size(th.width), height: size(th.height), ext: th.ext } : null,
    description: answer.description === true,
  };
}

/** Whether a video offers a part. One the instance could not say about
 *  offers everything. */
function videoHas(offer, part) {
  if (!offer.known) return true;
  switch (part) {
    case 'video':
      return offer.videoTracks.length > 0 || offer.qualities.length > 0;
    case 'audio':
      return offer.audio;
    case 'subtitle':
      return offer.subtitles.length > 0 || offer.autoCaptions.length > 0;
    case 'thumbnail':
      return offer.thumbnail !== null;
    default:
      return offer.description;
  }
}

/** The language a subtitle or audio code is in: "en" for "en-orig" or
 *  "en-US". */
function videoLangBase(code) {
  return code.split('-')[0].toLowerCase();
}

/**
 * The subtitles a video's menu offers: every one by hand, and of the
 * generated ones the original ("en-orig") and those in a language the pick
 * asks for, since a host translates its generated subtitles into every
 * language it knows.
 */
function videoSubtitleChoices(offer, pick) {
  const wanted = new Set(pick.subLangs.map(videoLangBase));
  const auto = offer.autoCaptions.filter((c) => c.endsWith('-orig') || wanted.has(c.toLowerCase()));
  return { manual: offer.subtitles, auto };
}

/**
 * A pick made exact for one video, where the menu starts: the parts it has,
 * the best video track under the cap, the best audio track in the preferred
 * codec, the original audio where there are several languages, the wanted
 * subtitle languages it has (by hand before generated, else its original
 * generated ones), and the wanted subtitle format where it has that one.
 */
function videoResolve(pick, offer) {
  if (!offer.known) return { ...pick, subLangs: [...pick.subLangs] };
  const sel = { ...pick, parts: pick.parts.filter((p) => videoHas(offer, p)) };
  if (sel.parts.length === 0) sel.parts = VIDEO_PARTS.filter((p) => videoHas(offer, p)).slice(0, 1);
  const cap = pick.quality === 'best' ? Infinity : parseInt(pick.quality, 10);
  sel.videoTrack = offer.videoTracks.find((k) => parseInt(k, 10) <= cap) ?? offer.videoTracks.at(-1);
  sel.audioTrack =
    (pick.audio === 'best' ? undefined : offer.audioTracks.find((k) => k.split(' ')[0] === pick.audio)) ?? offer.audioTracks[0];
  if (offer.audioLangs.length > 1) sel.audioLang = (offer.audioLangs.find((l) => !l.dubbed) ?? offer.audioLangs[0]).code;
  const { manual, auto } = videoSubtitleChoices(offer, pick);
  const langs = [];
  let fromAuto = false;
  for (const want of pick.subLangs.map(videoLangBase)) {
    const own = manual.find((c) => videoLangBase(c) === want);
    const made = auto.find((c) => c.toLowerCase() === `${want}-orig`) ?? auto.find((c) => c.toLowerCase() === want);
    if (own) langs.push(own);
    else if (made) {
      langs.push(made);
      fromAuto = true;
    }
  }
  if (langs.length === 0) {
    const first = manual[0] ?? auto.find((c) => c.endsWith('-orig'));
    if (first) langs.push(first);
    fromAuto = !manual[0] && !!first;
  }
  sel.subLangs = langs;
  sel.subAuto = fromAuto;
  if (!offer.subtitleFormats.includes(sel.subFormat)) sel.subFormat = 'best';
  for (const k of ['videoTrack', 'audioTrack']) if (!sel[k]) delete sel[k];
  return sel;
}

/** What a video offered, folded into what its site has offered so far
 *  (videoSeen, by site): the resolutions, the audio codecs, the subtitle
 *  languages by hand and generated originals, and the subtitle formats. */
function videoSeenWith(seen, offer) {
  const merge = (old, add, order) => {
    const all = new Set([...(Array.isArray(old) ? old : []), ...add]);
    return order ? order.filter((x) => all.has(x)) : [...all].sort();
  };
  const subLangs = [...offer.subtitles, ...offer.autoCaptions.filter((c) => c.endsWith('-orig'))].map(videoLangBase);
  return {
    qualities: merge(seen?.qualities, offer.qualities, VIDEO_CAPS),
    audio: merge(seen?.audio, offer.audioTracks.map((k) => k.split(' ')[0]), VIDEO_AUDIO_CODECS),
    subLangs: merge(seen?.subLangs, subLangs),
    subFormats: merge(seen?.subFormats, offer.subtitleFormats),
  };
}

/** What a site has offered so far, for the options page to offer as
 *  defaults: never more than the instance has reported there. */
function videoSiteSeen(stored, siteId) {
  const seen = stored?.videoSeen?.[siteId];
  const list = (v, keep) => (Array.isArray(v) ? v.filter(keep) : []);
  return {
    qualities: VIDEO_CAPS.filter((q) => list(seen?.qualities, () => true).includes(q)),
    audio: VIDEO_AUDIO_CODECS.filter((c) => list(seen?.audio, () => true).includes(c)),
    subLangs: list(seen?.subLangs, (l) => videoIs(VIDEO_LANG, l)),
    subFormats: list(seen?.subFormats, (f) => videoIs(VIDEO_TOKEN, f)),
  };
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

/** The keyboard shortcut's name in the manifest's commands. */
const VIDEO_COMMAND = 'send-video';

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
