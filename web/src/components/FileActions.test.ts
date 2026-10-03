import { describe, expect, it } from 'vitest';

import { type Task } from '../lib/api';
import { hasSomethingToPlay, playsAsMedia } from './FileActions';

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

  it('offers Play for a torrent of several files when the server found media in it', () => {
    expect(playsAsMedia(task({ name: 'Season 1', torrentFileCount: 12, torrentMedia: 'video' }))).toBe(true);
    expect(playsAsMedia(task({ name: 'Album', torrentFileCount: 12, torrentMedia: 'audio' }))).toBe(true);
  });

  it('offers no Play for a torrent of several files without audio or video', () => {
    expect(playsAsMedia(task({ name: 'Software Pack', torrentFileCount: 12 }))).toBe(false);
    expect(playsAsMedia(task({ name: 'Folder.mkv', torrentFileCount: 3 }))).toBe(false);
    expect(playsAsMedia(task({ name: 'Season 1', torrentFileCount: 1 }))).toBe(false);
  });
});

describe('hasSomethingToPlay', () => {
  it('plays a running or a finished download', () => {
    expect(hasSomethingToPlay(task({ status: 'running' }))).toBe(true);
    expect(hasSomethingToPlay(task({ status: 'done', loaded: 10, size: 10 }))).toBe(true);
  });

  it('offers no Play for a download stopped halfway', () => {
    expect(hasSomethingToPlay(task({ status: 'paused', loaded: 5, size: 10 }))).toBe(false);
    expect(hasSomethingToPlay(task({ status: 'error', loaded: 5, size: 10 }))).toBe(false);
  });
});
