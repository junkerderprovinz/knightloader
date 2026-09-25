import { describe, expect, it } from 'vitest';
import type { YtdlpHosterPreset } from '../lib/api';
import {
  aacNamed,
  audioPickers,
  audioSummary,
  formatLabel,
  hostPresetMenus,
  presetPickers,
  type PresetMenus,
} from './VariantPicker';

const t = (key: string) => key;

const FULL: PresetMenus = {
  videoFormats: ['best', 'mp4 avc1', 'mp4 hevc', 'mp4 av1', 'webm vp9', 'webm av1'],
  qualities: ['best', '1080p', '720p', 'custom'],
  audioFormats: ['best', 'aac', 'alac', 'flac', 'mp3', 'opus', 'vorbis', 'wav'],
  audioConvertTo: [],
  audioBitrates: ['', '128', '192'],
};

const YOUTUBE = {
  videoFormats: ['best', 'mp4 avc1', 'mp4 vp9', 'mp4 av1', 'webm vp9'],
  audioFormats: ['best', 'aac', 'opus'],
  known: true,
};

const YOUTUBE_LACKS = ['alac', 'flac', 'mp3', 'vorbis', 'wav'];

const preset = (fields: Partial<YtdlpHosterPreset>): YtdlpHosterPreset => ({
  variants: ['video', 'audio'],
  videoFormat: 'best',
  quality: 'best',
  audioFormat: 'best',
  audioBitrate: '',
  ...fields,
});

describe('aac', () => {
  it('reads a pick stored under the file extension as aac', () => {
    expect(aacNamed('m4a')).toBe('aac');
    expect(aacNamed('m4a 129k')).toBe('aac 129k');
    expect(aacNamed('aac 129k')).toBe('aac 129k');
    expect(aacNamed('mp3')).toBe('mp3');
  });

  // YouTube's m4a is AAC in an M4A container, and m4a is what people look for.
  it('is named with the container it is written in', () => {
    expect(formatLabel('aac', t)).toBe('AAC (M4A)');
    expect(audioSummary('m4a', '', t)).toBe('AAC (M4A)');
  });

  it('lists a probed row stored with m4a menus as aac', () => {
    const p = audioPickers({
      pick: 'm4a 129k',
      bitrate: '',
      formats: ['best', 'm4a', 'opus'],
      convertTo: [],
      tracks: ['opus 160k', 'm4a 129k', 'm4a 49k'],
      conversions: FULL.audioBitrates,
      t,
      onPick: () => {},
    });
    expect(p.format.value).toBe('aac');
    expect(p.format.options).toEqual(['best', 'aac', 'opus']);
    expect(p.bitrate?.value).toBe('129');
    expect(p.bitrate?.options).toEqual(['', '129', '49']);
  });
});

describe('format names', () => {
  it('reads every format as it is usually written', () => {
    expect(FULL.audioFormats.map((f) => formatLabel(f, t))).toEqual([
      'columns.variant.auto',
      'AAC (M4A)',
      'ALAC',
      'FLAC',
      'MP3',
      'Opus',
      'Vorbis',
      'WAV',
    ]);
    expect(FULL.videoFormats.map((f) => formatLabel(f, t))).toEqual([
      'columns.variant.auto',
      'MP4 (H.264)',
      'MP4 (H.265)',
      'MP4 (AV1)',
      'WebM (VP9)',
      'WebM (AV1)',
    ]);
  });

  it('writes a container or codec it has no name for in capitals', () => {
    expect(formatLabel('avi', t)).toBe('AVI');
    expect(formatLabel('flv vp8', t)).toBe('FLV (VP8)');
  });

  it('gives the variant defaults the names a row shows', () => {
    const p = presetPickers({ preset: preset({ audioFormat: 'opus' }), menus: FULL, t, onChange: () => {} });
    expect(p.video.format.options.map(p.video.format.render)).toEqual(FULL.videoFormats.map((f) => formatLabel(f, t)));
    expect(p.audio.format.render(p.audio.format.value)).toBe('Opus');
  });
});

describe('host preset menus', () => {
  it('offers the host its own formats and keeps the full qualities and bitrates', () => {
    const menus = hostPresetMenus(FULL, YOUTUBE);
    expect(menus.audioFormats).toEqual(['best', 'aac', 'opus']);
    expect(menus.audioConvertTo).toEqual(YOUTUBE_LACKS);
    expect(menus.videoFormats).toEqual(YOUTUBE.videoFormats);
    expect(menus.qualities).toBe(FULL.qualities);
    expect(menus.audioBitrates).toBe(FULL.audioBitrates);
  });

  it('keeps the full menus where the server gave no answer', () => {
    expect(hostPresetMenus(FULL, null)).toBe(FULL);
  });

  it('offers what the host lacks below its own formats, as a conversion', () => {
    const p = presetPickers({ preset: preset({}), menus: hostPresetMenus(FULL, YOUTUBE), t, onChange: () => {} });
    expect(p.audio.format.options).toEqual(['best', 'aac', 'opus', ...YOUTUBE_LACKS]);
    expect(p.audio.format.groups).toEqual([
      { options: ['best'] },
      { options: ['aac', 'opus'] },
      { heading: 'columns.variant.convertTo', options: YOUTUBE_LACKS },
    ]);
  });

  it('keeps a format the preset already names although the host lacks it', () => {
    const p = presetPickers({
      preset: preset({ videoFormat: 'webm av1', quality: '1080p', audioFormat: 'mp3', audioBitrate: '192' }),
      menus: hostPresetMenus(FULL, YOUTUBE),
      t,
      onChange: () => {},
    });
    expect(p.audio.format.value).toBe('mp3');
    expect(p.audio.format.groups[2].options).toContain('mp3');
    expect(p.audio.bitrate?.value).toBe('192');
    expect(p.video.format.value).toBe('webm av1');
    expect(p.video.format.options).toEqual([...YOUTUBE.videoFormats, 'webm av1']);
  });

  it('has no conversions to offer where nothing is known of the host', () => {
    const p = presetPickers({ preset: preset({}), menus: FULL, t, onChange: () => {} });
    expect(p.audio.format.options).toEqual(FULL.audioFormats);
    // An empty run is not drawn, heading and all.
    expect(p.audio.format.groups.find((g) => g.heading)?.options).toEqual([]);
  });
});

describe('a probed audio row', () => {
  const row = (pick: string, onPick: (pick: string, bitrate: string) => void = () => {}) =>
    audioPickers({
      pick,
      bitrate: '',
      formats: ['best', 'aac', 'opus'],
      convertTo: FULL.audioFormats,
      tracks: ['opus 160k', 'aac 129k'],
      conversions: FULL.audioBitrates,
      t,
      onPick,
    });

  it('offers the formats its source lacks under Convert to', () => {
    const p = row('aac 129k');
    expect(p.format.options).toEqual(['best', 'aac', 'opus', ...YOUTUBE_LACKS]);
    expect(p.format.groups[2]).toEqual({ heading: 'columns.variant.convertTo', options: YOUTUBE_LACKS });
  });

  it('picks a conversion at the bitrates a conversion is offered at', () => {
    let picked: [string, string] | null = null;
    row('aac 129k', (pick, bitrate) => {
      picked = [pick, bitrate];
    }).format.onPick('flac');
    expect(picked).toEqual(['flac', '']);
    expect(row('flac').bitrate?.options).toEqual(FULL.audioBitrates);
  });
});
