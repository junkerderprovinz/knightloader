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

/** What each site puts after the video's title in its tab title. */
const VIDEO_TITLE_ENDS = {
  youtube: / - YouTube$/,
  vimeo: / (?:\||on) Vimeo$/,
  dailymotion: / - (?:video )?Dailymotion$/,
  twitch: / - Twitch$/,
  bilibili: /_哔哩哔哩.*bilibili$/,
};

/** The resolutions POST /api/links takes as `format` (ytdlp.Qualities). */
const VIDEO_CAPS = ['4320p', '2160p', '1440p', '1080p', '720p', '480p', '360p', '240p', '144p'];

/** The rows a video becomes on the instance (ytdlp.Variants), in the order
 *  the menu and the options page list them. */
const VIDEO_PARTS = ['video', 'audio', 'subtitle', 'thumbnail', 'description'];

/** The codecs an audio track comes in (audioFamilies in
 *  internal/resolver/ytdlp/formats.go), which a site's default can prefer. */
const VIDEO_AUDIO_CODECS = ['aac', 'opus', 'vorbis', 'mp3', 'flac', 'alac'];

/** The containers and codecs a video comes in, in the order the menu and the
 *  options page list them (containerRank in internal/resolver/ytdlp). */
const VIDEO_CONTAINERS = ['mp4', 'webm'];
const VIDEO_CODECS = ['avc1', 'hevc', 'vp9', 'vp8', 'av1'];
// The order yt-dlp ranks codecs in at one resolution and frame rate
// (preferredVideoCodecs), which picks the track where only a quality is.
const VIDEO_PREFERRED = ['av1', 'vp9', 'hevc', 'avc1', 'vp8'];

/** The subtitle formats the menu and the options page offer, SRT first as
 *  the one every player reads. YouTube's own srv1 to srv3 and json3 open in
 *  nothing but its player, so they are not offered. */
const VIDEO_SUB_FORMATS = ['srt', 'vtt', 'ttml'];

// Names that are not the token in capitals.
const VIDEO_FORMAT_NAMES = { avc1: 'H.264', hevc: 'H.265', mp4a: 'AAC', webm: 'WebM', opus: 'Opus', vorbis: 'Vorbis', vtt: 'WebVTT', webp: 'WebP', jpg: 'JPEG' };

/** A container, codec or file type as it is written ("H.264" for avc1),
 *  for what the menu and the options page show; the token stays what is
 *  stored and sent. */
function videoFormatName(token) {
  return VIDEO_FORMAT_NAMES[token] ?? token.toUpperCase();
}

/** What a click downloads on a site nothing is set for: the video at its
 *  best. */
const VIDEO_DEFAULTS = { parts: ['video'], quality: 'best', videoFormat: 'best', audio: 'best', audioRate: 'best', subLangs: [], subFormat: 'srt', thumbFormat: 'jpg' };

// The words the instance's choices are made of, as ytdlp.LinkChoice.Valid
// takes them: a language as yt-dlp reports one, a format, and the keys of a
// video and an audio track (VideoTracks and AudioTracks).
const VIDEO_LANG = /^[A-Za-z]{2,3}(-[A-Za-z0-9]{1,8}){0,2}$/;
const VIDEO_TOKEN = /^[a-z0-9]{1,10}$/;
const VIDEO_TRACK = /^[1-9][0-9]{1,4}p([1-9][0-9]{1,2})? [a-z0-9]{1,10}( [a-z0-9]{1,10})?$/;
const AUDIO_TRACK = /^[a-z0-9]{1,10} [1-9][0-9]{0,3}k$/;
// A video track's container with its codec, and an audio track's bitrate:
// the halves of a track the menu offers apart.
const VIDEO_FORMAT = /^[a-z0-9]{1,10}( [a-z0-9]{1,10})?$/;
const AUDIO_RATE = /^[1-9][0-9]{0,3}k$/;

/** Whether v is a string of the form re describes. A pattern alone would
 *  take undefined, which it reads as the word. */
function videoIs(re, v) {
  return typeof v === 'string' && re.test(v);
}

/**
 * A pick from what is stored or what a page sent, with every value the
 * instance would refuse dropped. The part of it a site's defaults hold says
 * what is wanted wherever a video has it: the parts, a cap on the
 * resolution and a video format, a bitrate and a codec for the audio,
 * subtitle languages in order, a subtitle format and a thumbnail format.
 * Subtitles and thumbnails have no Best, so anything else reads as SRT and
 * JPEG. The menu adds the tracks, the audio language and whether generated
 * subtitles count, from what one video has. A pick with no part left is the
 * defaults.
 */
function videoPick(raw) {
  const parts = Array.isArray(raw?.parts) ? VIDEO_PARTS.filter((p) => raw.parts.includes(p)) : [];
  if (parts.length === 0) return { ...VIDEO_DEFAULTS, subLangs: [] };
  const pick = {
    parts,
    quality: VIDEO_CAPS.includes(raw.quality) ? raw.quality : 'best',
    videoFormat: videoIs(VIDEO_FORMAT, raw.videoFormat) ? raw.videoFormat : 'best',
    audio: VIDEO_AUDIO_CODECS.includes(raw.audio) ? raw.audio : 'best',
    audioRate: videoIs(AUDIO_RATE, raw.audioRate) ? raw.audioRate : 'best',
    subLangs: Array.isArray(raw.subLangs) ? raw.subLangs.filter((l) => videoIs(VIDEO_LANG, l)) : [],
    subFormat: VIDEO_SUB_FORMATS.includes(raw.subFormat) ? raw.subFormat : 'srt',
    thumbFormat: videoIs(VIDEO_TOKEN, raw.thumbFormat) && raw.thumbFormat !== 'best' ? raw.thumbFormat : 'jpg',
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
 * subtitles by hand still gets its own language's. The thumbnail's format
 * has no place in it, since the instance keeps the picture as the host
 * serves it.
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
 * comes in, its subtitles by hand and generated, the picture the thumbnail
 * row fetches and whether it has a description. `known` is false where
 * there is no answer, or one from an instance that only lists resolutions,
 * and then nothing is ruled out.
 */
function videoOffer(answer) {
  const list = (v, re) => (Array.isArray(v) ? v.filter((x) => videoIs(re, x)) : []);
  const qualities = Array.isArray(answer?.qualities) ? VIDEO_CAPS.filter((q) => answer.qualities.includes(q)) : [];
  if (!Array.isArray(answer?.videoTracks)) {
    return {
      known: false, qualities, audio: answer ? answer.audio === true : true, videoTracks: [], audioTracks: [], audioLangs: [],
      subtitles: [], autoCaptions: [], thumbnail: null, description: true, videoSizes: {}, audioSizes: {},
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
    thumbnail: videoIs(VIDEO_TOKEN, th?.ext) ? { width: size(th.width), height: size(th.height), ext: th.ext } : null,
    description: answer.description === true,
    videoSizes: videoSizes(answer.videoSizes, (k) => k === 'best' || VIDEO_CAPS.includes(k) || videoIs(VIDEO_TRACK, k)),
    audioSizes: videoSizes(answer.audioSizes, (k) => videoIs(VIDEO_TOKEN, k) || videoIs(AUDIO_TRACK, k)),
  };
}

/** The bytes the instance says each pick downloads, of the picks a row
 *  can make. */
function videoSizes(raw, pick) {
  const all = raw && typeof raw === 'object' ? Object.entries(raw) : [];
  return Object.fromEntries(all.filter(([k, n]) => pick(k) && Number.isInteger(n) && n > 0));
}

/** The bytes a pick from the menu downloads of a part, video or audio, or
 *  undefined where the instance does not know. */
function videoPickSize(sel, offer, part) {
  const exact = videoExact(sel, offer);
  return part === 'video' ? offer.videoSizes?.[exact.videoTrack ?? exact.quality] : offer.audioSizes?.[exact.audioTrack ?? exact.audio];
}

/** The bytes a whole pick downloads, or undefined where any part of it has
 *  no size, since a sum of the parts known would pass for the total. */
function videoPickTotal(sel, offer) {
  let total = 0;
  for (const part of sel.parts) {
    const bytes = part === 'video' || part === 'audio' ? videoPickSize(sel, offer, part) : undefined;
    if (!bytes) return undefined;
    total += bytes;
  }
  return total;
}

/** A size in bytes as the menu shows it, in megabytes or from a thousand on
 *  in gigabytes, with a decimal below ten. */
function videoSize(bytes, lang) {
  const mb = Math.max(bytes / 1e6, 0.1);
  const [n, unit] = mb >= 1000 ? [mb / 1000, 'gigabyte'] : [mb, 'megabyte'];
  return new Intl.NumberFormat(lang, { style: 'unit', unit, unitDisplay: 'short', maximumFractionDigits: n < 10 ? 1 : 0 }).format(n);
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

/** The formats a video's thumbnail comes in. The instance names the one
 *  picture the row fetches, which it keeps as the host serves it. */
function videoThumbFormats(offer) {
  return offer.thumbnail ? [offer.thumbnail.ext] : [];
}

/** The thumbnail format a pick gets among those offered: the one it wants,
 *  else JPEG, else the first. */
function videoThumbFormat(want, formats) {
  return [want, 'jpg'].find((f) => formats.includes(f)) ?? formats[0];
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

/** A video track's key in its halves: "1080p60 webm vp9" is the quality
 *  "1080p60", 1080 lines tall, in the format "webm vp9". */
function videoTrackOf(key) {
  const [quality, ...format] = key.split(' ');
  return { quality, height: parseInt(quality, 10), fps: parseInt(quality.split('p')[1], 10) || 0, format: format.join(' ') };
}

/** An audio track's key in its halves: "opus 160k" is the codec "opus" at
 *  the bitrate "160k". */
function audioTrackOf(key) {
  const [codec, rate] = key.split(' ');
  return { codec, rate, kbps: parseInt(rate, 10) };
}

/** Where a value comes in a fixed order, anything the order lacks after it. */
function videoRank(order, v) {
  const i = order.indexOf(v);
  return i === -1 ? order.length : i;
}

/** Video formats in the menu's order: mp4 before webm, and within one the
 *  codecs in the order yt-dlp's family table lists them. */
function videoFormatOrder(a, b) {
  const [ca, ka = ''] = a.split(' ');
  const [cb, kb = ''] = b.split(' ');
  return (
    videoRank(VIDEO_CONTAINERS, ca) - videoRank(VIDEO_CONTAINERS, cb) ||
    ca.localeCompare(cb) ||
    videoRank(VIDEO_CODECS, ka) - videoRank(VIDEO_CODECS, kb) ||
    ka.localeCompare(kb)
  );
}

function audioCodecOrder(a, b) {
  return videoRank(VIDEO_AUDIO_CODECS, a) - videoRank(VIDEO_AUDIO_CODECS, b) || a.localeCompare(b);
}

function audioRateOrder(a, b) {
  return parseInt(b, 10) - parseInt(a, 10);
}

/** The two rows a video's tracks become: its qualities, highest first, and
 *  its formats, each once. */
function videoMenuRows(offer) {
  const tracks = offer.videoTracks.map(videoTrackOf);
  const qualities = [...new Map(tracks.map((t) => [t.quality, t])).values()]
    .sort((a, b) => b.height - a.height || b.fps - a.fps)
    .map((t) => t.quality);
  return { qualities, formats: [...new Set(tracks.map((t) => t.format))].sort(videoFormatOrder) };
}

/** The two rows a video's audio tracks become: the bitrates, highest first,
 *  and the codecs. */
function audioMenuRows(offer) {
  const tracks = offer.audioTracks.map(audioTrackOf);
  return {
    rates: [...new Set(tracks.map((t) => t.rate))].sort(audioRateOrder),
    codecs: [...new Set(tracks.map((t) => t.codec))].sort(audioCodecOrder),
  };
}

/** A quality as the menu names it: with its frame rate only where the video
 *  has its resolution at two. */
function videoQualityName(quality, qualities) {
  const height = parseInt(quality, 10);
  return qualities.filter((q) => parseInt(q, 10) === height).length > 1 ? quality : `${height}p`;
}

/** Whether a video has a format at a quality; at the best quality, whether
 *  it has the format at all. */
function videoTrackOffered(offer, quality, format) {
  return offer.videoTracks.some((k) => {
    const t = videoTrackOf(k);
    return t.format === format && (quality === 'best' || t.quality === quality);
  });
}

function audioTrackOffered(offer, rate, codec) {
  return offer.audioTracks.some((k) => {
    const t = audioTrackOf(k);
    return t.codec === codec && (rate === 'best' || t.rate === rate);
  });
}

/**
 * A pick made exact for one video, where the menu starts. Best stays Best,
 * since that is what was asked for. A quality becomes the video's own, or
 * the highest one under it, and a bitrate the nearest, the higher one on a
 * tie; a format or a codec the video lacks there falls back to Best. The
 * parts are those it has, the audio the original where there are several
 * languages, and the subtitles the wanted languages it has (by hand before
 * generated, else its original generated ones), and the thumbnail in a
 * format the video has it in.
 */
function videoResolve(pick, offer) {
  const { videoTrack, audioTrack, ...rest } = pick;
  const sel = { ...rest, subLangs: [...pick.subLangs] };
  if (!offer.known) return sel;
  sel.parts = pick.parts.filter((p) => videoHas(offer, p));
  if (sel.parts.length === 0) sel.parts = VIDEO_PARTS.filter((p) => videoHas(offer, p)).slice(0, 1);
  if (offer.videoTracks.length) {
    const { qualities } = videoMenuRows(offer);
    if (sel.quality !== 'best' && !qualities.includes(sel.quality)) {
      const cap = parseInt(sel.quality, 10);
      sel.quality = qualities.find((q) => parseInt(q, 10) <= cap) ?? qualities.at(-1);
    }
    if (!videoTrackOffered(offer, sel.quality, sel.videoFormat)) sel.videoFormat = 'best';
  }
  if (offer.audioTracks.length) {
    const tracks = offer.audioTracks.map(audioTrackOf);
    if (!tracks.some((t) => t.codec === sel.audio)) sel.audio = 'best';
    if (sel.audioRate !== 'best') {
      const want = parseInt(sel.audioRate, 10);
      const [near] = tracks
        .filter((t) => sel.audio === 'best' || t.codec === sel.audio)
        .map((t) => t.kbps)
        .sort((a, b) => Math.abs(a - want) - Math.abs(b - want) || b - a);
      sel.audioRate = `${near}k`;
    }
  }
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
  if (offer.thumbnail) sel.thumbFormat = videoThumbFormat(sel.thumbFormat, videoThumbFormats(offer));
  return sel;
}

/**
 * What a pick from the menu sends: the track its quality and format name
 * together, or with Best in one of them the instance's own pick under the
 * cap or in the codec. A resolution the video has at two frame rates, or one
 * no cap names, is a track as well. Where the video's tracks are not known,
 * the cap and the codec alone.
 */
function videoExact(sel, offer) {
  const { videoFormat, audioRate, ...out } = sel;
  out.quality = VIDEO_CAPS.includes(sel.quality) ? sel.quality : 'best';
  if (offer.videoTracks.length && (videoFormat !== 'best' || sel.quality !== 'best')) {
    const height = parseInt(sel.quality, 10);
    // A cap takes a resolution's highest frame rate, the first of it in the
    // row, as Best does at the top.
    const capped = videoMenuRows(offer).qualities.find((q) => parseInt(q, 10) === height) === sel.quality;
    if (videoFormat === 'best' && capped && VIDEO_CAPS.includes(`${height}p`)) {
      out.quality = `${height}p`;
    } else {
      out.quality = 'best';
      const codec = (k) => videoRank(VIDEO_PREFERRED, videoTrackOf(k).format.split(' ')[1]);
      [out.videoTrack] = offer.videoTracks
        .filter((k) => {
          const t = videoTrackOf(k);
          return (videoFormat === 'best' || t.format === videoFormat) && (sel.quality === 'best' || t.quality === sel.quality);
        })
        .sort((a, b) => parseInt(b, 10) - parseInt(a, 10) || videoTrackOf(b).fps - videoTrackOf(a).fps || codec(a) - codec(b));
    }
  }
  if (offer.audioTracks.length && audioRate !== 'best') {
    out.audioTrack = offer.audioTracks.find((k) => {
      const t = audioTrackOf(k);
      return t.rate === audioRate && (sel.audio === 'best' || t.codec === sel.audio);
    });
    out.audio = 'best';
  }
  for (const k of ['videoTrack', 'audioTrack']) if (!out[k]) delete out[k];
  return out;
}

/** What a video offered, folded into what its site has offered so far
 *  (videoSeen, by site): the resolutions, the audio codecs, the subtitle
 *  languages by hand and generated originals, the video formats, the audio
 *  bitrates and the thumbnail formats. */
function videoSeenWith(seen, offer) {
  const merge = (old, add, order) => {
    const all = new Set([...(Array.isArray(old) ? old : []), ...add]);
    return Array.isArray(order) ? order.filter((x) => all.has(x)) : [...all].sort(order);
  };
  const subLangs = [...offer.subtitles, ...offer.autoCaptions.filter((c) => c.endsWith('-orig'))].map(videoLangBase);
  return {
    qualities: merge(seen?.qualities, offer.qualities, VIDEO_CAPS),
    audio: merge(seen?.audio, offer.audioTracks.map((k) => k.split(' ')[0]), VIDEO_AUDIO_CODECS),
    subLangs: merge(seen?.subLangs, subLangs),
    videoFormats: merge(seen?.videoFormats, offer.videoTracks.map((k) => videoTrackOf(k).format), videoFormatOrder),
    audioRates: merge(seen?.audioRates, offer.audioTracks.map((k) => audioTrackOf(k).rate), audioRateOrder),
    thumbFormats: merge(seen?.thumbFormats, videoThumbFormats(offer)),
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
    videoFormats: list(seen?.videoFormats, (f) => f !== 'best' && videoIs(VIDEO_FORMAT, f)),
    audioRates: list(seen?.audioRates, (r) => videoIs(AUDIO_RATE, r)),
    thumbFormats: list(seen?.thumbFormats, (f) => videoIs(VIDEO_TOKEN, f)),
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

/** A task id as the instance makes them, safe to put in a query. */
const VIDEO_TASK_ID = /^[A-Za-z0-9_-]{1,64}$/;

/** The most rows the button follows. A long playlist is left to the web
 *  interface, since asking about all of it every few seconds costs more than
 *  a ring is worth. */
const VIDEO_WATCH_MAX = 50;

/**
 * The rows of a send the button follows, from the instance's answer to POST
 * /api/links with report set: those it created and started. None where it
 * held every row back, left them in the collector, created nothing because
 * the video was there already, or created more than the button follows.
 */
function videoWatchIds(answer) {
  const tasks = Array.isArray(answer) ? answer : answer?.tasks;
  if (!Array.isArray(tasks)) return [];
  const started = tasks.filter((t) => t && !t.skipped && !t.variantOff && t.status !== 'collected' && videoIs(VIDEO_TASK_ID, t.id));
  return started.length <= VIDEO_WATCH_MAX ? started.map((t) => t.id) : [];
}

/**
 * Where the rows of a send stand, from GET /api/tasks?ids=: the rows named,
 * and the other rows of their links, which share a link's address and the
 * moment it was staged. Failed once a row has failed with no try left, done
 * once every row is, gone once none is left, and otherwise running with the
 * share downloaded, which is null while a row's size is unknown.
 */
function videoProgress(tasks, ids) {
  const list = Array.isArray(tasks) ? tasks.filter((t) => t && typeof t === 'object') : [];
  const link = (t) => `${t.url} ${t.createdAt}`;
  const links = new Set(list.filter((t) => ids.includes(t.id)).map(link));
  const rows = list.filter((t) => links.has(link(t)) && !t.skipped && !t.variantOff && t.status !== 'collected');
  if (rows.length === 0) return { state: 'gone' };
  // A zero nextTry comes as the year 1, before 1970.
  const failed = rows.find((t) => t.status === 'error' && !(Date.parse(t.nextTry) > 0));
  if (failed) return { state: 'failed', reason: typeof failed.error === 'string' ? failed.error : '' };
  if (rows.every((t) => t.status === 'done')) return { state: 'done' };
  let size = 0;
  let loaded = 0;
  let known = true;
  for (const t of rows) {
    const total = Number.isFinite(t.size) && t.size > 0 ? t.size : 0;
    const have = Number.isFinite(t.loaded) && t.loaded > 0 ? t.loaded : 0;
    if (t.status === 'done') {
      size += total || have;
      loaded += total || have;
    } else {
      if (!total) known = false;
      size += total;
      loaded += Math.min(have, total);
    }
  }
  return { state: 'running', share: known && size > 0 ? loaded / size : null };
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

/** A tab title without the site's name after it, so it names the video. */
function videoTitle(title, siteId) {
  return title.replace(VIDEO_TITLE_ENDS[siteId], '');
}

/**
 * What to call a video whose page gives no title, such as an embedded player:
 * its site and its id, from the address videoPage settled on. Vimeo puts the
 * key of an unlisted video after the id, the others end with it.
 */
function videoName(url) {
  const u = new URL(url);
  const path = u.pathname.split('/').filter(Boolean);
  const id = u.searchParams.get('v') ?? (u.hostname === 'vimeo.com' ? path[0] : path.at(-1));
  return `${u.hostname.replace(/^www\./, '')} · ${id}`;
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
