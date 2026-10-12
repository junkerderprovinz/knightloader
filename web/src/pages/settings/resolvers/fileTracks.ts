import type { MediaTrack } from '../../../components/pictures/TrackMerge';
import { capLabel } from '../../../components/VariantPicker';
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
  const tracks: MediaTrack[] = [
    {
      id: 'video',
      name: t('columns.variant.video'),
      value: (custom && ytdlp.customFormat.trim()) || capLabel(ytdlp.quality, t),
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
