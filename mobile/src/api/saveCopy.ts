// Copying a task's file onto the phone piece by piece, apart from the file
// system and the transport so check-save.mjs runs it in node. It imports
// nothing at run time.

import type { Task } from './types';

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
 * Whether a row offers to save its file: one that has finished, is a file of
 * its own rather than a torrent of several, and lies on the instance rather
 * than on a JDownloader somewhere else. Until the link is resolved its name is
 * the link.
 */
export function canSave(task: Task): boolean {
  return (
    task.status === 'done' &&
    (task.torrentFileCount ?? 0) <= 1 &&
    task.resolver !== 'jd' &&
    task.name !== '' &&
    task.name !== task.url
  );
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
