import { describe, expect, it } from 'vitest';
import type { YtdlpOptions } from '../../../lib/api';
import { en } from '../../../lib/locales/en';
import { fileTracks, mergedExtension } from './fileTracks';

const t = (key: keyof typeof en) => en[key];

const off = { subs: false, thumbnail: false, metadata: false, chapters: false, splitChapters: false, nfo: false };
const options = (embed: Partial<typeof off> = {}) =>
  ({ quality: '1080p', customFormat: '', videoFormat: 'best', subtitleLangs: 'de', embed: { ...off, ...embed } }) as unknown as YtdlpOptions;

describe('the tracks inside a video file', () => {
  it('holds picture and sound while every embed switch is off', () => {
    expect(fileTracks(options(), t).map((tr) => tr.id)).toEqual(['video', 'audio']);
  });

  it('takes in each track whose switch is on', () => {
    const tracks = fileTracks(options({ subs: true, thumbnail: true, metadata: true, chapters: true }), t);
    expect(tracks.map((tr) => tr.id)).toEqual(['video', 'audio', 'subtitle', 'thumbnail', 'metadata', 'chapters']);
    expect(tracks[2].value).toBe('de');
  });

  it('asks for English subtitles when no language is set', () => {
    const tracks = fileTracks({ ...options({ subs: true }), subtitleLangs: ' ' }, t);
    expect(tracks[2].value).toBe('en');
  });
});

describe('the file a video is merged into', () => {
  const video = (videoFormat: string, quality = '1080p', thumbnail = false) =>
    ({ ...options({ thumbnail }), videoFormat, quality }) as YtdlpOptions;

  it('takes the video format where it names mp4 or webm, and mkv otherwise', () => {
    expect(['best', 'mp4', 'mkv', 'webm'].map((f) => mergedExtension(video(f)))).toEqual(['mkv', 'mp4', 'mkv', 'webm']);
  });

  it('puts webm with an embedded cover into mkv, since webm cannot hold one', () => {
    expect(mergedExtension(video('webm', '1080p', true))).toBe('mkv');
  });

  it('leaves the container to a custom selector', () => {
    expect(mergedExtension(video('mp4', 'custom'))).toBe('mkv');
  });

  it('names the format beside the resolution on the video track', () => {
    expect(fileTracks(video('mp4'), t)[0].value).toBe(`MP4, ${en['settings.resolvers.quality.1080p']}`);
    expect(fileTracks(video('best'), t)[0].value).toBe(en['settings.resolvers.quality.1080p']);
  });
});
