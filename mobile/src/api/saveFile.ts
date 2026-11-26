import AsyncStorage from '@react-native-async-storage/async-storage';
import { Directory, FileMode } from 'expo-file-system';
import { KnightWatch } from '../../modules/watch';
import { fetchPlayable, fetchTorrentFiles } from './client';
import { fileReader } from './play';
import type { RangeReader } from '../play/direct';
import { copyAll, isFileSet, saveName, saveType, torrentItems, type SaveItem } from './saveCopy';
import type { ServerConnection, Task, TorrentFileView } from './types';

// The folder picked last, which the picker opens at next time. A preference
// rather than a secret, so AsyncStorage.
const FOLDER_KEY = 'knightloader-save-folder';

/** What a save reports while it copies, and once it has. */
export interface SaveProgress {
  name: string;
  size: number;
  done: number;
}

type OnProgress = (p: SaveProgress) => void;

/**
 * saveTask copies what task downloaded into a folder somebody picks in
 * Android's own picker, which grants the app that folder alone, so it needs no
 * storage permission: its file, or every file of a torrent of several in a
 * folder of its name. False when the picker was closed without a folder. The
 * bytes come through the range route a piece at a time, straight from the
 * instance on its own network, so a large file never sits in memory whole.
 */
export async function saveTask(
  conn: ServerConnection,
  task: Task,
  title: string,
  signal: AbortSignal,
  onProgress: OnProgress,
  base = '/api',
): Promise<boolean> {
  if (isFileSet(task)) {
    const items = torrentItems(task, await fetchTorrentFiles(conn, task.id, base));
    return saveItems(conn, task, saveName(task), items, title, signal, onProgress, base);
  }
  const playable = await fetchPlayable(conn, task.id, base);
  const item = { file: playable.file, path: [saveName(task)], size: playable.size, type: playable.type };
  return saveItems(conn, task, saveName(task), [item], title, signal, onProgress, base);
}

/** saveTorrentFile copies the file at `index` of a torrent's list, as saveTask does. */
export async function saveTorrentFile(
  conn: ServerConnection,
  task: Task,
  files: TorrentFileView[],
  index: number,
  title: string,
  signal: AbortSignal,
  onProgress: OnProgress,
  base = '/api',
): Promise<boolean> {
  const items = torrentItems(task, files, index);
  return saveItems(conn, task, items[0]?.path.at(-1) ?? saveName(task), items, title, signal, onProgress, base);
}

async function saveItems(
  conn: ServerConnection,
  task: Task,
  name: string,
  items: SaveItem[],
  title: string,
  signal: AbortSignal,
  onProgress: OnProgress,
  base: string,
): Promise<boolean> {
  if (items.length === 0) throw new Error('there is no finished file to save');
  const picked = await pickFolder();
  if (!picked) return false;
  // Each folder of the set once, by its path under the picked one. The first
  // one made holds the whole set, and goes again when the set does not finish.
  const folders = new Map<string, Directory>([['', picked]]);
  const made: { set?: Directory } = {};
  const folderFor = (path: string[]): Directory => {
    let at = picked;
    for (let i = 0; i < path.length - 1; i++) {
      const key = path.slice(0, i + 1).join('/');
      let next = folders.get(key);
      if (!next) {
        next = at.createDirectory(path[i]);
        folders.set(key, next);
        made.set ??= next;
      }
      at = next;
    }
    return at;
  };
  const readers = new Map<string, RangeReader>();
  const readerFor = async (file: string) => {
    let r = readers.get(file);
    if (!r) {
      r = await fileReader(conn, task.id, file, base);
      readers.set(file, r);
    }
    return r;
  };
  const total = items.reduce((n, i) => n + i.size, 0);
  onProgress({ name, size: total, done: 0 });
  // A copy outlives the app going to the background, where Android would
  // freeze it halfway without a foreground service.
  KnightWatch?.serve(title, name);
  try {
    await copyAll({
      items,
      read: async (item, from, length) => (await readerFor(item.file))(from, length),
      open: (item) => {
        const file = folderFor(item.path).createFile(item.path.at(-1)!, saveType(item.type));
        const handle = file.open(FileMode.WriteOnly);
        return {
          write: (bytes) => handle.writeBytes(bytes),
          close: () => handle.close(),
          remove: () => quietly(() => file.delete()),
        };
      },
      signal,
      onProgress: (done) => onProgress({ name, size: total, done }),
    });
  } catch (e) {
    const set = made.set;
    if (set) quietly(() => set.delete());
    throw e;
  } finally {
    KnightWatch?.endServe();
  }
  return true;
}

// The provider may refuse to delete, and the error that stopped the copy is
// the one worth showing.
function quietly(f: () => void): void {
  try {
    f();
  } catch {}
}

async function pickFolder(): Promise<Directory | null> {
  const last = (await AsyncStorage.getItem(FOLDER_KEY).catch(() => null)) ?? undefined;
  try {
    const folder = await Directory.pickDirectoryAsync(last);
    await AsyncStorage.setItem(FOLDER_KEY, folder.uri).catch(() => {});
    return folder;
  } catch (e) {
    if ((e as { code?: string }).code === 'ERR_PICKER_CANCELLED') return null;
    throw e;
  }
}
