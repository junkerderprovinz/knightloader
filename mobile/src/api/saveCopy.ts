// Copying a task's file onto the phone piece by piece, apart from the file
// system and the transport so check-save.mjs runs it in node. It imports
// nothing at run time.

import type { Task, TorrentFileView } from './types';

/** The most one call to the range route sends, over the relay as on the local network. */
export const SAVE_PIECE = 4 << 20;

/** Where the copy goes: a file opened for writing, which removes itself when
 *  the copy does not finish. */
export interface SaveTarget {
  write(bytes: Uint8Array): Promise<void>;
  close(): void;
  remove(): void;
}

/** A copy somebody called off. */
export class SaveStopped extends Error {
  constructor() {
    super('saving was stopped');
  }
}

/**
 * Whether a row offers to save what it downloaded: one that has finished and
 * lies on the instance rather than on a JDownloader somewhere else. Until the
 * link is resolved its name is the link. A torrent of several files saves
 * them all, in a folder of its name.
 */
export function canSave(task: Task): boolean {
  return task.status === 'done' && task.resolver !== 'jd' && task.name !== '' && task.name !== task.url;
}

/** Whether a task is a torrent of several files, saved as a set. */
export const isFileSet = (task: Task): boolean => (task.torrentFileCount ?? 0) > 1;

/** Whether one file of a finished torrent can be saved: one it fetched, all of it. */
export const canSaveTorrentFile = (task: Task, f: TorrentFileView): boolean =>
  canSave(task) && f.selected && (f.done === undefined || f.done >= f.size);

/** One file a save writes: which file of the task, as the range route's
 *  `file` names it, the folders and name it gets under the picked folder,
 *  its length and its type. */
export interface SaveItem {
  file: string;
  path: string[];
  size: number;
  type: string;
}

const parts = (path: string): string[] => path.split('/').filter((p) => p && p !== '.' && p !== '..');

/**
 * What saving a torrent's files writes: the file at `only`, or every file it
 * fetched under a folder of the torrent's name, each in the folders it has
 * inside the torrent. A file is known to the range route by its place in the
 * list.
 */
export function torrentItems(task: Task, files: TorrentFileView[], only?: number): SaveItem[] {
  return files.flatMap((f, i) => {
    if (only !== undefined ? i !== only : !canSaveTorrentFile(task, f)) return [];
    const inside = parts(f.path);
    const path = only !== undefined ? inside.slice(-1) : [saveName(task), ...inside];
    return [{ file: String(i), path, size: f.size, type: '' }];
  });
}

/** The name the file gets on the phone: the last part of the task's own. */
export const saveName = (task: Task): string => task.name.split('/').filter(Boolean).at(-1) ?? task.name;

/** The type the file is saved as, without its parameters. */
export const saveType = (type: string): string => type.split(';')[0].trim() || 'application/octet-stream';

/**
 * copyFile writes size bytes from read into target, the next piece already on
 * its way while one is written, so only two pieces are ever held. A copy that
 * fails or is stopped removes what it wrote.
 */
export async function copyFile(o: {
  size: number;
  read: (from: number, length: number) => Promise<Uint8Array>;
  target: SaveTarget;
  signal: AbortSignal;
  onProgress: (done: number) => void;
  piece?: number;
}): Promise<void> {
  const piece = o.piece ?? SAVE_PIECE;
  const ask = (from: number) => {
    const p = o.read(from, Math.min(piece, o.size - from));
    // A piece asked for ahead can fail after the copy stopped for another reason.
    p.catch(() => {});
    return p;
  };
  let done = 0;
  let next = o.size > 0 ? ask(0) : null;
  try {
    while (next) {
      if (o.signal.aborted) throw new SaveStopped();
      const bytes = await next;
      if (o.signal.aborted) throw new SaveStopped();
      if (bytes.length === 0) throw new Error('the instance sent no bytes');
      done += bytes.length;
      next = done < o.size ? ask(done) : null;
      await o.target.write(bytes);
      o.onProgress(done);
    }
  } catch (e) {
    o.target.close();
    o.target.remove();
    throw e;
  }
  o.target.close();
}

/**
 * copyAll copies each item in turn into the target `open` makes for it, and
 * says how far it has got over all of them. A copy that fails or is stopped
 * removes the file it was writing and leaves the ones before it to the caller.
 */
export async function copyAll(o: {
  items: SaveItem[];
  read: (item: SaveItem, from: number, length: number) => Promise<Uint8Array>;
  open: (item: SaveItem) => SaveTarget;
  signal: AbortSignal;
  onProgress: (done: number, total: number) => void;
  piece?: number;
}): Promise<void> {
  const total = o.items.reduce((n, i) => n + i.size, 0);
  let before = 0;
  for (const item of o.items) {
    if (o.signal.aborted) throw new SaveStopped();
    await copyFile({
      size: item.size,
      read: (from, length) => o.read(item, from, length),
      target: o.open(item),
      signal: o.signal,
      onProgress: (done) => o.onProgress(before + done, total),
      piece: o.piece,
    });
    before += item.size;
    o.onProgress(before, total);
  }
}
