// The variant pickers: the pairs of dropdowns a yt-dlp row's format, quality
// and bitrate are chosen with. A video row chooses a format and then a quality
// within it, an audio row a format and then a bitrate, and a host preset offers
// the same two pairs. Each pair goes back into the one pick the server stores
// after the colon of core.Task.Variant (the grammar is
// internal/resolver/ytdlp/formats.go's):
//
//   video   best, custom, 1080p         no format chosen, at most that height
//           1080p60 webm vp9, 720p avi  one track
//           webm vp9, webm vp9 1080p    a preset's format no probe has resolved
//   audio   best, m4a, mp3              a format, its best track or a conversion
//           opus 160k                   one track
//
// An audio row keeps a bitrate beside a format the source has no track in,
// which is what a conversion encodes to (core.Task.AudioBitrate).

import type { ApiOptions, YtdlpHosterPreset } from '../lib/api';
import type { TranslationKey } from '../lib/i18n';
import { Dropdown, type DropdownWidth } from './Dropdown';

type Translate = (key: TranslationKey, vars?: Record<string, string | number>) => string;

/** What one picker shows and offers, built by videoPickers and audioPickers. */
export interface PickerProps {
  value: string;
  /** Every choice in menu order, which is what the wheel steps through. */
  options: string[];
  /** The same choices in the runs the menu separates with a hairline. */
  groups: string[][];
  /** What this picker chooses: its accessible name and its tooltip. */
  label: string;
  /** How one option reads on screen; the raw value is what is sent. */
  render: (option: string) => string;
  onPick: (value: string) => void;
}

/**
 * VariantDropdown draws one picker as the app's dropdown. The pickers stand
 * without a caption, on a list row and in a table cell alike, so the label is
 * their hover bubble too.
 */
export function VariantDropdown({
  picker,
  width = 'value',
  disabled,
  shake,
}: {
  picker: PickerProps;
  /** `value` on a list row, `widest` where a column should not move. */
  width?: DropdownWidth;
  disabled?: boolean;
  /** The caller's failure counter; see Dropdown. */
  shake?: number;
}) {
  const option = (o: string) => ({ value: o, label: picker.render(o) });
  return (
    <Dropdown
      value={picker.value}
      options={picker.options.map(option)}
      groups={picker.groups.filter((g) => g.length > 0).map((g) => g.map(option))}
      onChange={picker.onPick}
      label={picker.label}
      tip={picker.label}
      look="dense"
      width={width}
      disabled={disabled}
      shake={shake}
    />
  );
}

// Labels by the quality id the server sends; an id without one shows raw.
export const QUALITY_KEYS: Record<string, TranslationKey> = {
  best: 'settings.resolvers.quality.best',
  '4320p': 'settings.resolvers.quality.4320p',
  '2160p': 'settings.resolvers.quality.2160p',
  '1440p': 'settings.resolvers.quality.1440p',
  '1080p': 'settings.resolvers.quality.1080p',
  '720p': 'settings.resolvers.quality.720p',
  '480p': 'settings.resolvers.quality.480p',
  '360p': 'settings.resolvers.quality.360p',
  '240p': 'settings.resolvers.quality.240p',
  '144p': 'settings.resolvers.quality.144p',
  custom: 'settings.resolvers.quality.custom',
};

/** How a height cap reads where there is room: "Up to 1080p", "Best available". */
export const capLabel = (q: string, t: Translate): string => (QUALITY_KEYS[q] ? t(QUALITY_KEYS[q]) : q);

/**
 * How the quality picker reads a value: Auto for best, as the format picker
 * beside it does, and a height or track by its id. The picker has no room for
 * "Up to", and a cap and a track of one height rarely download different files.
 */
function qualityLabel(q: string, t: Translate): string {
  if (q === 'best') return t('columns.variant.auto');
  return q === 'custom' ? capLabel(q, t) : q;
}

/** How a format reads: "mp4 (avc1)", "avi", "opus", and Auto for best. */
export function formatLabel(format: string, t: Translate): string {
  if (format === 'best') return t('columns.variant.auto');
  const [ext, codec] = format.split(' ');
  return codec ? `${ext} (${codec})` : ext;
}

/** How a bitrate reads: "160 kbit/s", and Auto for none. */
export const bitrateLabel = (b: string, t: Translate): string =>
  b ? t('columns.variant.kbps', { kbps: b }) : t('columns.variant.auto');

const VIDEO_TRACK = /^(\d+)p(\d*) (.+)$/;
const CAP = /^\d+p$/;
const AUDIO_TRACK = /^([a-z0-9]+) (\d+)k$/;

const heightOf = (q: string): number => Number(/^(\d+)p/.exec(q)?.[1] ?? 0);

/** A stored value the menu does not list stays on it, or picking would lose it. */
const withValue = (list: string[], value: string): string[] => (list.includes(value) ? list : [...list, value]);

/**
 * Every format menu starts with best, a server that sent none included. aac
 * reads as m4a, as the server folds it: yt-dlp writes both into an .m4a file.
 */
const formatMenu = (list: string[]): string[] => [
  ...new Set(['best', ...list.map((f) => (f === 'aac' ? 'm4a' : f))]),
];

/**
 * A row's probed format list, or undefined where there is none to go by. A
 * probe lists "best" first and then formats; a stored row can still hold one
 * menu of tracks and formats mixed until the server probes it again.
 */
export function probedFormats(list: string[] | undefined): string[] | undefined {
  if (list?.[0] !== 'best' || list.some((f) => VIDEO_TRACK.test(f) || AUDIO_TRACK.test(f))) return undefined;
  return list;
}

/** best in a run of its own above the rest, as every one of these menus has it. */
const bestApart = (list: string[], best: string): string[][] => [
  list.filter((x) => x === best),
  list.filter((x) => x !== best),
];

/** A video pick's two halves: "best" or a format, and a quality. */
export function readVideoPick(pick: string): { format: string; quality: string } {
  const track = VIDEO_TRACK.exec(pick);
  if (track) return { format: track[3], quality: `${track[1]}p${track[2]}` };
  const words = pick.split(' ');
  if (words.length === 3 && CAP.test(words[2])) return { format: `${words[0]} ${words[1]}`, quality: words[2] };
  if (words.length === 2) return { format: pick, quality: 'best' };
  return { format: 'best', quality: pick || 'best' };
}

/**
 * The qualities a format offers. With tracks, a chosen format offers its own
 * tracks' heights and frame rates. Without them, as for a preset or a link no
 * probe has answered for, it offers the height caps, custom aside, since a
 * custom format string already says everything a format would.
 */
function qualitiesFor(format: string, tracks: string[] | null, caps: string[]): string[] {
  if (format === 'best') return caps;
  if (tracks === null) return caps.filter((c) => c !== 'custom');
  const out: string[] = [];
  for (const t of tracks) {
    const m = VIDEO_TRACK.exec(t);
    if (m && m[3] === format) out.push(`${m[1]}p${m[2]}`);
  }
  return out;
}

/** The quality a new format keeps: the same, else the tallest not above it, else the first. */
function qualityIn(options: string[], current: string): string {
  if (options.includes(current)) return current;
  const h = heightOf(current);
  const under = h > 0 ? options.find((o) => heightOf(o) > 0 && heightOf(o) <= h) : undefined;
  return under ?? options[0] ?? 'best';
}

function composeVideo(format: string, quality: string, tracks: string[] | null): string {
  if (format === 'best') return quality;
  if (tracks === null) return quality === 'best' ? format : `${format} ${quality}`;
  return `${quality} ${format}`;
}

/**
 * videoPickers lays out a video pick as its format picker and its quality
 * picker. `tracks` is null where nothing was probed, a preset or a link whose
 * probe has not answered, and the quality is then a height cap in any format.
 */
export function videoPickers(o: {
  pick: string;
  formats: string[];
  tracks: string[] | null;
  caps: string[];
  t: Translate;
  /** The new pick, and which of the two pickers made it. */
  onPick: (pick: string, from: 'format' | 'quality') => void;
}): { format: PickerProps; quality: PickerProps } {
  const { format, quality } = readVideoPick(o.pick || 'best');
  const formats = withValue(formatMenu(o.formats), format);
  const qualities = withValue(qualitiesFor(format, o.tracks, o.caps), quality);
  const caps = format === 'best' || o.tracks === null;
  return {
    format: {
      value: format,
      options: formats,
      groups: bestApart(formats, 'best'),
      label: o.t('settings.resolvers.videoFormat'),
      render: (f) => formatLabel(f, o.t),
      onPick: (f) =>
        o.onPick(composeVideo(f, qualityIn(qualitiesFor(f, o.tracks, o.caps), quality), o.tracks), 'format'),
    },
    quality: {
      value: quality,
      options: qualities,
      groups: caps
        ? [
            qualities.filter((q) => q === 'best'),
            qualities.filter((q) => heightOf(q) > 0),
            qualities.filter((q) => q !== 'best' && heightOf(q) === 0),
          ]
        : [qualities],
      label: o.t('settings.resolvers.quality'),
      render: (q) => qualityLabel(q, o.t),
      onPick: (q) => o.onPick(composeVideo(format, q, o.tracks), 'quality'),
    },
  };
}

/** An audio pick's two halves: "best" or a format, and a bitrate, "" for none. */
export function readAudioPick(pick: string, bitrate = ''): { format: string; bitrate: string } {
  const p = pick === 'aac' ? 'm4a' : pick || 'best';
  const track = AUDIO_TRACK.exec(p);
  if (track) return { format: track[1], bitrate: track[2] };
  return { format: p, bitrate: p === 'best' ? '' : bitrate };
}

/**
 * audioPickers lays out an audio pick as its format picker and its bitrate
 * picker. A format the source has a track in offers those tracks' bitrates and
 * resolves to one of them; any other format is a conversion, offered at
 * `conversions`. `tracks` is null where nothing was probed, and every format
 * is then offered at the conversion bitrates. Best has no bitrate picker.
 */
export function audioPickers(o: {
  pick: string;
  bitrate: string;
  formats: string[];
  tracks: string[] | null;
  conversions: string[];
  t: Translate;
  /** The new pick and bitrate, and which of the two pickers made them. */
  onPick: (pick: string, bitrate: string, from: 'format' | 'bitrate') => void;
}): { format: PickerProps; bitrate: PickerProps | null } {
  const current = readAudioPick(o.pick, o.bitrate);
  const offered = formatMenu(o.formats);
  const native = (f: string) => o.tracks !== null && f !== 'best' && offered.includes(f);
  const bitratesFor = (f: string): string[] => {
    if (!native(f)) return o.conversions;
    const own = [''];
    for (const t of o.tracks ?? []) {
      const m = AUDIO_TRACK.exec(t);
      if (m && m[1] === f) own.push(m[2]);
    }
    return own;
  };
  const pick = (f: string, b: string, from: 'format' | 'bitrate') => {
    if (f === 'best') o.onPick('best', '', from);
    else if (!native(f)) o.onPick(f, b, from);
    else o.onPick(b ? `${f} ${b}k` : f, '', from);
  };
  const formats = withValue(offered, current.format);
  const bitrates = withValue(bitratesFor(current.format), current.bitrate);
  return {
    format: {
      value: current.format,
      options: formats,
      groups: bestApart(formats, 'best'),
      label: o.t('settings.resolvers.audioFormat'),
      render: (f) => formatLabel(f, o.t),
      onPick: (f) => pick(f, bitratesFor(f).includes(current.bitrate) ? current.bitrate : '', 'format'),
    },
    bitrate:
      current.format === 'best'
        ? null
        : {
            value: current.bitrate,
            options: bitrates,
            groups: bestApart(bitrates, ''),
            label: o.t('settings.resolvers.audioBitrate'),
            render: (b) => bitrateLabel(b, o.t),
            onPick: (b) => pick(current.format, b, 'bitrate'),
          },
  };
}

/** The menus a host preset offers, which knows no source yet: /api/options' full lists. */
export interface PresetMenus {
  videoFormats: string[];
  qualities: string[];
  audioFormats: string[];
  audioBitrates: string[];
}

export const NO_PRESET_MENUS: PresetMenus = { videoFormats: [], qualities: [], audioFormats: [], audioBitrates: [] };

export const presetMenusOf = (o: ApiOptions): PresetMenus => ({
  videoFormats: o.ytdlpVideoFormats ?? [],
  qualities: o.ytdlpQualities ?? [],
  audioFormats: o.ytdlpAudioFormats ?? [],
  audioBitrates: o.ytdlpAudioBitrates ?? [],
});

/**
 * presetPickers lays out a host preset as the same two pairs a link's rows
 * show. The preset keeps format and quality apart (ytdlp.HosterPreset), so its
 * video half goes through the pick a new link's row would store.
 */
export function presetPickers(o: {
  preset: YtdlpHosterPreset;
  menus: PresetMenus;
  t: Translate;
  onChange: (fields: Partial<YtdlpHosterPreset>) => void;
}): { video: { format: PickerProps; quality: PickerProps }; audio: { format: PickerProps; bitrate: PickerProps | null } } {
  const format = o.preset.videoFormat || 'best';
  const quality = o.preset.quality || 'best';
  let pick = quality;
  if (format !== 'best') pick = quality === 'best' ? format : `${format} ${quality}`;
  return {
    video: videoPickers({
      pick,
      formats: o.menus.videoFormats,
      tracks: null,
      caps: o.menus.qualities,
      t: o.t,
      onPick: (next) => {
        const parts = readVideoPick(next);
        o.onChange({ videoFormat: parts.format, quality: parts.quality });
      },
    }),
    audio: audioPickers({
      pick: o.preset.audioFormat || 'best',
      bitrate: o.preset.audioBitrate ?? '',
      formats: o.menus.audioFormats,
      tracks: null,
      conversions: o.menus.audioBitrates,
      t: o.t,
      onPick: (audioFormat, audioBitrate) => o.onChange({ audioFormat, audioBitrate }),
    }),
  };
}

/** How a video pick reads in one line: "Auto", "Up to 1080p", "1080p60 webm (vp9)". */
export function videoSummary(pick: string, t: Translate): string {
  const { format, quality } = readVideoPick(pick || 'best');
  if (format === 'best') return quality === 'best' ? formatLabel(format, t) : capLabel(quality, t);
  if (VIDEO_TRACK.test(pick)) return `${quality} ${formatLabel(format, t)}`;
  return `${formatLabel(format, t)} ${capLabel(quality, t)}`;
}

/** How an audio pick reads in one line: "Auto", "opus 160 kbit/s", "mp3". */
export function audioSummary(pick: string, bitrate: string, t: Translate): string {
  const { format, bitrate: b } = readAudioPick(pick, bitrate);
  if (format === 'best' || !b) return formatLabel(format, t);
  return `${formatLabel(format, t)} ${bitrateLabel(b, t)}`;
}
