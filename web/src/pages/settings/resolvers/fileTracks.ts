import type { MediaTrack } from '../../../components/pictures/TrackMerge';
import { capLabel, formatLabel } from '../../../components/VariantPicker';
import type { YtdlpOptions } from '../../../lib/api';
import type { TranslationKey } from '../../../lib/i18n';

type Translate = (key: TranslationKey, vars?: Record<string, string | number>) => string;

// What buildArgs in internal/resolver/ytdlp asks for when no languages are set.
const DEFAULT_SUBTITLE_LANGS = 'en';

/**
 * fileTracks are the tracks a video row puts into its file with these
 * settings: picture and sound always, and whatever an embed switch muxes in
 * while it is on. A custom selector picks picture and sound alike.
 */
export function fileTracks(ytdlp: YtdlpOptions, t: Translate): MediaTrack[] {
  const { embed } = ytdlp;
  const custom = ytdlp.quality === 'custom';
  const format = !custom && ytdlp.videoFormat !== 'best' ? formatLabel(ytdlp.videoFormat, t) : '';
  const picture = [format, capLabel(ytdlp.quality, t)].filter(Boolean).join(', ');
  const tracks: MediaTrack[] = [
    {
      id: 'video',
      name: t('columns.variant.video'),
      value: (custom && ytdlp.customFormat.trim()) || picture,
    },
    { id: 'audio', name: t('columns.variant.audio'), value: capLabel(custom ? 'custom' : 'best', t) },
  ];
  if (embed.subs) {
    tracks.push({
      id: 'subtitle',
      name: t('columns.variant.subtitle'),
      value: ytdlp.subtitleLangs.trim() || DEFAULT_SUBTITLE_LANGS,
    });
  }
  if (embed.thumbnail) {
    tracks.push({ id: 'thumbnail', name: t('columns.variant.thumbnail'), value: t('picture.tracks.cover') });
  }
  if (embed.metadata) {
    tracks.push({ id: 'metadata', name: t('picture.tracks.metadata'), value: t('picture.tracks.tags') });
  }
  if (embed.chapters) {
    tracks.push({ id: 'chapters', name: t('picture.tracks.chapters'), value: t('picture.tracks.marks') });
  }
  return tracks;
}

/**
 * mergedExtension is the container buildArgs merges a video row without a
 * format of its own into: the instance's video format where it names mp4 or
 * webm, mkv otherwise. yt-dlp cannot embed a cover into webm, so webm with a
 * cover becomes mkv, and a custom selector ignores the format.
 */
export function mergedExtension(ytdlp: YtdlpOptions): string {
  if (ytdlp.quality === 'custom') return 'mkv';
  if (ytdlp.videoFormat === 'mp4') return 'mp4';
  if (ytdlp.videoFormat === 'webm' && !ytdlp.embed.thumbnail) return 'webm';
  return 'mkv';
}
