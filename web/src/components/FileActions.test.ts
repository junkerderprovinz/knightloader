import { describe, expect, it } from 'vitest';

import { type Task } from '../lib/api';
import { playsAsMedia } from './FileActions';

function task(over: Partial<Task>): Task {
  return {
    id: '1',
    url: 'https://host.example/film.mkv',
    name: 'film.mkv',
    package: 'Film',
    resolver: 'direct',
    size: 0,
    loaded: 0,
    speed: 0,
    status: 'running',
    createdAt: '2026-09-25T10:00:00Z',
    priority: 0,
    position: 0,
    enabled: true,
    ...over,
  };
}

// The server plays what its allowlist serves as audio or video, and picks the
// file itself in a torrent of several.
describe('playsAsMedia', () => {
  it('offers Play for audio and video', () => {
    expect(playsAsMedia(task({ name: 'film.mkv' }))).toBe(true);
    expect(playsAsMedia(task({ name: 'song.FLAC' }))).toBe(true);
  });

  it('offers no Play for anything else', () => {
    expect(playsAsMedia(task({ name: 'setup.exe' }))).toBe(false);
    expect(playsAsMedia(task({ name: 'cover.jpg' }))).toBe(false);
  });

  it('offers Play for a torrent of several files, whatever it is called', () => {
    expect(playsAsMedia(task({ name: 'Season 1', torrentFileCount: 12 }))).toBe(true);
    expect(playsAsMedia(task({ name: 'Season 1', torrentFileCount: 1 }))).toBe(false);
  });
});
