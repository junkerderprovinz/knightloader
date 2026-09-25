import { describe, expect, it } from 'vitest';

import { ApiError, type Task } from '../lib/api';
import { hasFiles, linkRenameNote, renameRefusal } from './RenameDialog';

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
    status: 'collected',
    createdAt: '2026-09-25T10:00:00Z',
    priority: 0,
    position: 0,
    enabled: true,
    ...over,
  };
}

// The window says beforehand what app.renameLocked will do, so the two have
// to agree state by state.
describe('linkRenameNote', () => {
  it('lets a link that has not started take the name at once', () => {
    expect(linkRenameNote(task({ status: 'collected' }))).toBeNull();
    expect(linkRenameNote(task({ status: 'paused', loaded: 100 }))).toBeNull();
  });

  it('lets a finished file on this machine be renamed on disk', () => {
    expect(linkRenameNote(task({ status: 'done' }))).toBeNull();
  });

  it('says a running download takes the name when it finishes', () => {
    expect(linkRenameNote(task({ status: 'running' }))).toEqual({ key: 'rename.whenDone', blocked: false });
  });

  it('refuses what the server refuses', () => {
    expect(linkRenameNote(task({ status: 'extracting' }))?.blocked).toBe(true);
    expect(linkRenameNote(task({ infoHash: 'c12fe1c06bba254a9dc9f519b335aa7c1367a88a' }))?.blocked).toBe(true);
    expect(linkRenameNote(task({ status: 'done', name: 'film.part2.rar', archivePart: 2 }))?.blocked).toBe(true);
  });

  // JDownloader names its files itself, so the settle path never reaches them,
  // whatever state the link is in.
  it('refuses a JDownloader link before it has finished too', () => {
    for (const status of ['collected', 'queued', 'running', 'done'] as const) {
      expect(linkRenameNote(task({ status, resolver: 'jd' }))).toEqual({ key: 'rename.remote', blocked: true });
    }
  });
});

// One such link keeps a renamed package in its folder (app.hasFilesLocked).
describe('hasFiles', () => {
  it('counts a link with bytes on disk or on their way', () => {
    expect(hasFiles(task({ status: 'running' }))).toBe(true);
    expect(hasFiles(task({ status: 'done' }))).toBe(true);
    expect(hasFiles(task({ status: 'paused', loaded: 1 }))).toBe(true);
  });

  it('does not count a link nothing has been fetched for', () => {
    expect(hasFiles(task({ status: 'collected' }))).toBe(false);
    expect(hasFiles(task({ status: 'queued' }))).toBe(false);
    expect(hasFiles(task({ status: 'error' }))).toBe(false);
  });
});

// A refusal the window cannot see coming, such as a part of an archive nothing
// has numbered yet, is still said in the reader's language.
describe('renameRefusal', () => {
  const t = (key: string, vars?: Record<string, string | number>) => (vars ? `${key} ${vars.name}` : key);

  it('words a refusal by the code the server gave it', () => {
    const volume = new ApiError('film.part2.rar is one part of a multi-volume archive', 'volume', { name: 'film.part2.rar' }, 400);
    expect(renameRefusal(volume, t)).toBe('rename.volume film.part2.rar');
    expect(renameRefusal(new ApiError('not renamed', 'exists', { name: 'film.mkv' }, 400), t)).toBe('rename.exists film.mkv');
  });

  it('leaves a refusal without a code it knows to the server', () => {
    expect(renameRefusal(new ApiError('not renamed: permission denied', undefined, undefined, 400), t)).toBeNull();
    expect(renameRefusal(new ApiError('something newer', 'newer', {}, 400), t)).toBeNull();
    expect(renameRefusal(new Error('offline'), t)).toBeNull();
  });
});
