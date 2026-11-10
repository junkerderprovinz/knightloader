import type { Task } from './types';

// The audio and video half of inlineTypes in internal/api/routes_files.go,
// which decides what the instance serves as media. web/src/components/
// taskdetail/playable.ts keeps the same list for the browser.
const MEDIA: Record<string, 'audio' | 'video'> = {
  '.mp3': 'audio',
  '.m4a': 'audio',
  '.wav': 'audio',
  '.flac': 'audio',
  '.ogg': 'audio',
  '.mp4': 'video',
  '.m4v': 'video',
  '.webm': 'video',
  '.mkv': 'video',
  '.mov': 'video',
  '.avi': 'video',
};

function extensionOf(name: string): string {
  const dot = name.lastIndexOf('.');
  return dot > name.lastIndexOf('/') ? name.slice(dot).toLowerCase() : '';
}

/**
 * mediaKind is what a task plays as, or null when it has no Play button: its
 * file's own kind, or for a torrent of several files the kind of the one the
 * instance plays (torrentMedia). A torrent's name is its folder's, which says
 * nothing about its files.
 */
export function mediaKind(task: Task): 'audio' | 'video' | null {
  if ((task.torrentFileCount ?? 0) > 1) return task.torrentMedia ?? null;
  return MEDIA[extensionOf(task.name)] ?? null;
}

/**
 * Whether there is anything to play now. A running download streams and a
 * finished one is on disk. A stopped HTTP download's file already has its
 * full size with holes where nothing has arrived, so the instance refuses it.
 */
export function hasSomethingToPlay(task: Task): boolean {
  return task.status === 'running' || task.status === 'done';
}
