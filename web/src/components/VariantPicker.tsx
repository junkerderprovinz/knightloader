// The variant pickers: the dropdown a yt-dlp row's format, quality and bitrate
// are chosen with, and the pairs they come in. A video row chooses a format and
// then a quality within it, an audio row a format and then a bitrate, and a
// host preset offers the same two pairs. Each pair goes back into the one pick
// the server stores after the colon of core.Task.Variant (the grammar is
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

import { useEffect, useRef } from 'react';
import type { ApiOptions, YtdlpHosterPreset } from '../lib/api';
import type { TranslationKey } from '../lib/i18n';
import { IconChevronDown } from '../lib/icons';
import { ContextMenu, anchorBelow, useContextMenu, type MenuItem } from './ContextMenu';
import { useTooltip } from './ui';

type Translate = (key: TranslationKey, vars?: Record<string, string | number>) => string;

// It has to read as a control and not as a word that happens to be clickable:
// a surface2 ground on rows that are themselves surface2 gives the box no edge,
// and without a chevron nothing says "this opens".
//
// hover:bg-carbon-hoverRaised, never hover:bg-carbon-hover: this box is filled
// with surface3, and --carbon-hover is the hover for an element with no fill of
// its own, which sits below surface3 on every ramp and would dim the control at
// the moment somebody is looking straight at it (GlimStone rule 21). One
// template literal rather than concatenated strings, because
// check-hover-ramp.mjs reads one class list per literal.
const VARIANTE_SELECT_CLASS = `shrink-0 inline-flex items-center gap-1 cursor-pointer rounded-[var(--radius-control)]
  bg-carbon-surface3 py-1 ps-2 pe-1.5 text-xs text-carbon-text outline-none transition-shadow
  hover:bg-carbon-hoverRaised focus-visible:shadow-[0_0_0_2px_var(--focus-ring)] disabled:opacity-40`;

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
 * VariantPicker is the dropdown itself: a button and a ContextMenu, never a
 * native <select>.
 *
 * A <select> paints its open list with the operating system's widget, which on
 * Windows is a white panel with an orange focus frame belonging to no theme
 * this app has. `appearance: none` reaches the closed box only; the popup is
 * the browser's and cannot be styled. ContextMenu is the app's own menu
 * surface, keyboard-navigable, dismissed the way every other menu here is, and
 * it draws a checked mark for the value in force.
 */
export function VariantPicker({
  value,
  options,
  groups,
  label,
  render,
  onPick,
  disabled,
  shake = 0,
}: PickerProps & {
  disabled?: boolean;
  /**
   * The caller's failure counter. Every bump shakes this trigger once: the
   * value was shown optimistically, the server refused it, and a control that
   * only snaps back says nothing. Keyed on the number rather than toggled as a
   * class, so a second identical refusal gets a fresh DOM node and shakes again.
   */
  shake?: number;
}) {
  const menu = useContextMenu();
  const trigger = useRef<HTMLButtonElement>(null);
  // The house bubble rather than the OS balloon; see Tip. The trigger shows the
  // chosen value, the tooltip says what the picker chooses.
  const tip = useTooltip<HTMLButtonElement>(label);
  // The wheel listener below needs this element too, and one element takes one
  // ref, so both are filled from the same callback.
  const { role: _tipRole, tabIndex: _tipTabIndex, ref: tipRef, ...tipHover } = tip.triggerProps;

  /**
   * The wheel steps the value here as it does on the app's remaining native
   * <select>s: rule 14 gives the wheel to the picker, not to the element the
   * platform happens to draw. Clamped at both ends rather than wrapping, and a
   * value that is not in the list at all steps to the first option.
   *
   * A real listener with `{ passive: false }` and not onWheel, which React
   * registers passive at its root: without preventDefault the list scrolls away
   * under the pointer while the value changes.
   *
   * On a list row, while the pointer rests on it the wheel edits a download
   * instead of scrolling the list, and each notch is a request. If that ever
   * reads as the list refusing to scroll, the answer is a condition on the
   * gesture, not an exemption for this picker.
   */
  useEffect(() => {
    const el = trigger.current;
    if (!el) return;
    const onWheel = (e: WheelEvent) => {
      // A horizontal wheel says nothing about this control, and a trackpad
      // reports fractional deltas, so only the sign of deltaY is read.
      if (disabled || options.length < 2 || e.deltaY === 0) return;
      // This handler is the scroll while the pointer sits on the control.
      e.preventDefault();
      const at = options.indexOf(value);
      if (at < 0) {
        onPick(options[0]);
        return;
      }
      const next = Math.min(options.length - 1, Math.max(0, at + (e.deltaY > 0 ? 1 : -1)));
      if (next === at) return;
      onPick(options[next]);
    };
    el.addEventListener('wheel', onWheel, { passive: false });
    return () => el.removeEventListener('wheel', onWheel);
    // `shake` is a dependency because the shake mechanism replaces this
    // element: it keys the button on the counter, so a refusal unmounts the
    // node this listener is attached to. Without it the control would stop
    // answering the wheel after the first refused change.
  }, [disabled, options, value, onPick, shake]);

  const choice = (o: string): MenuItem => ({
    id: o || 'auto',
    label: render(o),
    checked: o === value,
    onSelect: () => onPick(o),
  });

  return (
    <>
      <button
        key={shake}
        ref={(el) => {
          trigger.current = el;
          tipRef.current = el;
        }}
        type="button"
        disabled={disabled}
        aria-label={label}
        {...tipHover}
        aria-haspopup="menu"
        onClick={(e) => {
          e.stopPropagation();
          menu.openAt(anchorBelow(e.currentTarget));
        }}
        className={`${VARIANTE_SELECT_CLASS} ${shake > 0 ? 'glim-shake' : ''}`}
      >
        <span className="truncate">{render(value)}</span>
        <IconChevronDown width={12} height={12} className="shrink-0 opacity-70" />
      </button>
      {tip.node}
      {menu.anchor && (
        <ContextMenu
          anchor={menu.anchor}
          label={label}
          onClose={menu.close}
          groups={groups.filter((g) => g.length > 0).map((g, i) => ({ id: `variant-${i}`, items: g.map(choice) }))}
        />
      )}
    </>
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

/** How a height cap reads: "Up to 1080p", "Best available". */
export const capLabel = (q: string, t: Translate): string => (QUALITY_KEYS[q] ? t(QUALITY_KEYS[q]) : q);

/** How a format reads: "mp4 (avc1)", "avi", "opus", and Auto for best. */
export function formatLabel(format: string, t: Translate): string {
  if (format === 'best') return t('columns.variant.auto');
  const [ext, codec] = format.split(' ');
  return codec ? `${ext} (${codec})` : ext;
}

/** How a bitrate reads: "160 kbit/s", and Auto for none. */
export const bitrateLabel = (b: string, t: Translate): string => (b ? `${b} kbit/s` : t('columns.variant.auto'));

const VIDEO_TRACK = /^(\d+)p(\d*) (.+)$/;
const CAP = /^\d+p$/;
const AUDIO_TRACK = /^([a-z0-9]+) (\d+)k$/;

const heightOf = (q: string): number => Number(/^(\d+)p/.exec(q)?.[1] ?? 0);

/** A stored value the menu does not list stays on it, or picking would lose it. */
const withValue = (list: string[], value: string): string[] => (list.includes(value) ? list : [...list, value]);

/** Every format menu starts with best, a server that sent none included. */
const withBest = (list: string[]): string[] => (list.includes('best') ? list : ['best', ...list]);

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
  /** "best" first. */
  formats: string[];
  tracks: string[] | null;
  caps: string[];
  t: Translate;
  /** The new pick, and which of the two pickers made it. */
  onPick: (pick: string, from: 'format' | 'quality') => void;
}): { format: PickerProps; quality: PickerProps } {
  const { format, quality } = readVideoPick(o.pick || 'best');
  const formats = withValue(withBest(o.formats), format);
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
        ? [qualities.filter((q) => q === 'best'), qualities.filter((q) => heightOf(q) > 0), qualities.filter((q) => q !== 'best' && heightOf(q) === 0)]
        : [qualities],
      label: o.t('settings.resolvers.quality'),
      // A cap is "up to" a height; a track is that height.
      render: (q) => (caps ? capLabel(q, o.t) : q),
      onPick: (q) => o.onPick(composeVideo(format, q, o.tracks), 'quality'),
    },
  };
}

/** An audio pick's two halves: "best" or a format, and a bitrate, "" for none. */
export function readAudioPick(pick: string, bitrate = ''): { format: string; bitrate: string } {
  // yt-dlp writes both into an .m4a file, so the server reads them as one.
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
  /** "best" first. */
  formats: string[];
  tracks: string[] | null;
  conversions: string[];
  t: Translate;
  /** The new pick and bitrate, and which of the two pickers made them. */
  onPick: (pick: string, bitrate: string, from: 'format' | 'bitrate') => void;
}): { format: PickerProps; bitrate: PickerProps | null } {
  const current = readAudioPick(o.pick, o.bitrate);
  const native = (f: string) => o.tracks !== null && o.formats.includes(f);
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
  const formats = withValue(withBest(o.formats), current.format);
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

/** How a video pick reads in one line: "Up to 1080p", "1080p60 webm (vp9)". */
export function videoSummary(pick: string, t: Translate): string {
  const { format, quality } = readVideoPick(pick || 'best');
  if (format === 'best') return capLabel(quality, t);
  if (VIDEO_TRACK.test(pick)) return `${quality} ${formatLabel(format, t)}`;
  return `${formatLabel(format, t)} ${capLabel(quality, t)}`;
}

/** How an audio pick reads in one line: "Auto", "opus 160 kbit/s", "mp3". */
export function audioSummary(pick: string, bitrate: string, t: Translate): string {
  const { format, bitrate: b } = readAudioPick(pick, bitrate);
  if (format === 'best' || !b) return formatLabel(format, t);
  return `${formatLabel(format, t)} ${bitrateLabel(b, t)}`;
}
