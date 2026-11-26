// The format and quality a video link is added with, from what the instance
// says the video offers (GET /api/ytdlp/link). The menus follow the web's
// pickers (web/src/components/VariantPicker.tsx): a video row picks a format
// and then a quality within it, an audio row a format and then a bitrate, and
// Auto leaves each to the instance. It imports nothing at run time, so
// check-video-pick.mjs runs it in node.

/** The part of the instance's answer the pickers read (app.LinkMenus). */
export interface LinkMenus {
  /** The resolution caps that download a track of their own, highest first. */
  qualities?: string[];
  audio?: boolean;
  /** "1080p60 webm vp9", "720p mp4 avc1", highest first. */
  videoTracks?: string[];
  /** "opus 160k", highest bitrate first. */
  audioTracks?: string[];
  /** What an audio row can be converted to (GET /api/ytdlp/conversions),
   *  absent from an instance without that route. */
  conversions?: Conversions;
}

export interface Conversions {
  /** "best" and the formats, as the web's /api/options lists them. */
  audioFormats: string[];
  /** In kbit/s, "" for the best the encoder gives. */
  audioBitrates: string[];
}

/** What POST /api/links takes as a link's rows and qualities (ytdlp.LinkChoice). */
export interface LinkChoice {
  variants: ('video' | 'audio')[];
  quality?: string;
  videoTrack?: string;
  audioFormat?: string;
  audioBitrate?: string;
  audioTrack?: string;
}

export type Part = 'video' | 'audio';

/**
 * One pick. A video's format is "best" or a container with its codec, and its
 * quality "best", a cap such as "1080p" under best, or a track's resolution
 * and frame rate. An audio's format is "best" or a codec, and its quality a
 * bitrate in kbit/s, "" for the best one.
 */
export interface Pick {
  part: Part;
  format: string;
  quality: string;
}

const VIDEO_TRACK = /^(\d+p\d*(?:hdr)?) ([a-z0-9]+(?: [a-z0-9]+)?)$/;
const AUDIO_TRACK = /^([a-z0-9]+) (\d+)k$/;

const unique = (list: string[]): string[] => [...new Set(list)];

const videoTracks = (m: LinkMenus) =>
  (m.videoTracks ?? []).flatMap((t) => {
    const x = VIDEO_TRACK.exec(t);
    return x ? [{ quality: x[1], format: x[2] }] : [];
  });

const audioTracks = (m: LinkMenus) =>
  (m.audioTracks ?? []).flatMap((t) => {
    const x = AUDIO_TRACK.exec(t);
    return x ? [{ format: x[1], bitrate: x[2] }] : [];
  });

/** The rows a video offers: video where it has pictures, audio where it has sound. */
export function partsOf(m: LinkMenus): Part[] {
  const out: Part[] = [];
  if ((m.qualities ?? []).length > 0 || videoTracks(m).length > 0) out.push('video');
  if (m.audio || audioTracks(m).length > 0) out.push('audio');
  return out;
}

/** Whether there is anything to choose beyond Auto. */
export const offersChoice = (m: LinkMenus): boolean => partsOf(m).length > 0;

function ownFormats(m: LinkMenus, part: Part): string[] {
  const own = part === 'video' ? videoTracks(m).map((t) => t.format) : audioTracks(m).map((t) => t.format);
  return unique(['best', ...own]);
}

/** The audio formats the source has no track in, which yt-dlp converts to with ffmpeg. */
export function convertedFormats(m: LinkMenus): string[] {
  const own = ownFormats(m, 'audio');
  return (m.conversions?.audioFormats ?? []).filter((f) => f !== 'best' && !own.includes(f));
}

/** A format's menu: the source's own, then for audio the ones it converts to, below them as on the web. */
export function formatsOf(m: LinkMenus, part: Part): string[] {
  return part === 'video' ? ownFormats(m, part) : [...ownFormats(m, part), ...convertedFormats(m)];
}

// A conversion promises no more than the best track the source has, as the
// instance narrows a row's bitrates (ytdlp.AvailableAudioBitrates).
function conversionBitrates(m: LinkMenus): string[] {
  const best = Math.max(0, ...audioTracks(m).map((t) => Number(t.bitrate)));
  return (m.conversions?.audioBitrates ?? []).filter((b) => b === '' || best === 0 || Number(b) <= best);
}

/** The qualities a format offers: the caps under best, and a format's own tracks otherwise. */
export function qualitiesOf(m: LinkMenus, part: Part, format: string): string[] {
  if (part === 'video') {
    if (format === 'best') return unique(['best', ...(m.qualities ?? [])]);
    return unique(videoTracks(m).filter((t) => t.format === format).map((t) => t.quality));
  }
  if (format === 'best') return [];
  if (convertedFormats(m).includes(format)) return unique(['', ...conversionBitrates(m)]);
  return unique(['', ...audioTracks(m).filter((t) => t.format === format).map((t) => t.bitrate)]);
}

const lines = (q: string): number => Number(/^(\d+)p/.exec(q)?.[1] ?? 0);

/** The pick a video is added with until somebody chooses: Auto in the first row it has. */
export const firstPick = (m: LinkMenus): Pick => {
  const part = partsOf(m)[0] ?? 'video';
  return { part, format: 'best', quality: part === 'video' ? 'best' : '' };
};

/**
 * The pick after another format is chosen. The quality stays where the new
 * format has it, else drops to the highest one not above it, else the first.
 */
export function withFormat(m: LinkMenus, pick: Pick, format: string): Pick {
  const options = qualitiesOf(m, pick.part, format);
  if (options.includes(pick.quality)) return { ...pick, format };
  const r = lines(pick.quality);
  const under = r > 0 ? options.find((o) => lines(o) > 0 && lines(o) <= r) : undefined;
  return { ...pick, format, quality: under ?? options[0] ?? '' };
}

export const withPart = (part: Part): Pick => ({
  part,
  format: 'best',
  quality: part === 'video' ? 'best' : '',
});

/** The choice POST /api/links takes for a pick. */
export function choiceOf(m: LinkMenus, pick: Pick): LinkChoice {
  if (pick.part === 'audio') {
    if (pick.format === 'best') return { variants: ['audio'] };
    if (!pick.quality) return { variants: ['audio'], audioFormat: pick.format };
    if (convertedFormats(m).includes(pick.format)) {
      return { variants: ['audio'], audioFormat: pick.format, audioBitrate: pick.quality };
    }
    return { variants: ['audio'], audioTrack: `${pick.format} ${pick.quality}k` };
  }
  if (pick.format === 'best') return { variants: ['video'], quality: pick.quality || 'best' };
  return { variants: ['video'], quality: 'best', videoTrack: `${pick.quality} ${pick.format}` };
}

/** Containers and codecs under the names people know them by; every other word reads in capitals. */
const FORMAT_NAMES: Record<string, string> = {
  aac: 'AAC (M4A)',
  opus: 'Opus',
  vorbis: 'Vorbis',
  webm: 'WebM',
  ogg: 'Ogg',
  avc1: 'H.264',
  hevc: 'H.265',
  theora: 'Theora',
};

const formatName = (word: string): string => FORMAT_NAMES[word] ?? word.toUpperCase();

/** How a format reads: "MP4 (H.264)", "Opus", and `auto` for best. */
export function formatLabel(format: string, auto: string): string {
  if (format === 'best') return auto;
  const [container, codec] = format.split(' ');
  return codec ? `${formatName(container)} (${formatName(codec)})` : formatName(container);
}

/** The single link a text is, or null for none or several. */
export function singleLink(text: string): string | null {
  const words = text.trim().split(/\s+/).filter(Boolean);
  return words.length === 1 && /^https?:\/\/\S+$/i.test(words[0]) ? words[0] : null;
}
