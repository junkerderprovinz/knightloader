import { describe, expect, it } from 'vitest';
import type { YtdlpOptions } from '../../../lib/api';
import { en } from '../../../lib/locales/en';
import { fileTracks } from './fileTracks';

const t = (key: keyof typeof en) => en[key];

const off = { subs: false, thumbnail: false, metadata: false, chapters: false, splitChapters: false, nfo: false };
const options = (embed: Partial<typeof off> = {}) =>
  ({ quality: '1080p', customFormat: '', subtitleLangs: 'de', embed: { ...off, ...embed } }) as unknown as YtdlpOptions;

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
