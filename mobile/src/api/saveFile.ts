import AsyncStorage from '@react-native-async-storage/async-storage';
import { Directory, FileMode } from 'expo-file-system';
import { KnightWatch } from '../../modules/watch';
import { fetchPlayable } from './client';
import { fileReader } from './play';
import { copyFile, saveName, saveType } from './saveCopy';
import type { ServerConnection, Task } from './types';

// The folder picked last, which the picker opens at next time. A preference
// rather than a secret, so AsyncStorage.
const FOLDER_KEY = 'knightloader-save-folder';

/** What saveTask reports while it copies, and once it has. */
export interface SaveProgress {
  name: string;
  size: number;
  done: number;
}

/**
 * saveTask copies task's file into a folder somebody picks in Android's own
 * picker, which grants the app that folder alone, so it needs no storage
 * permission. False when the picker was closed without a folder. The bytes
 * come through the range route a piece at a time, straight from the instance
 * on its own network, so a large file never sits in memory whole.
 */
export async function saveTask(
  conn: ServerConnection,
  task: Task,
  title: string,
  signal: AbortSignal,
  onProgress: (p: SaveProgress) => void,
  base = '/api',
): Promise<boolean> {
  const playable = await fetchPlayable(conn, task.id, base);
  const name = saveName(task);
  const folder = await pickFolder();
  if (!folder) return false;
  const file = folder.createFile(name, saveType(playable.type));
  const handle = file.open(FileMode.WriteOnly);
  const read = await fileReader(conn, task.id, playable.file, base);
  onProgress({ name, size: playable.size, done: 0 });
  // A copy outlives the app going to the background, where Android would
  // freeze it halfway without a foreground service.
  KnightWatch?.serve(title, name);
  try {
    await copyFile({
      size: playable.size,
      read,
      target: {
        write: (bytes) => handle.writeBytes(bytes),
        close: () => handle.close(),
        remove: () => {
          // The provider may refuse, and the error that stopped the copy is
          // the one worth showing.
          try {
            file.delete();
          } catch {}
        },
      },
      signal,
      onProgress: (done) => onProgress({ name, size: playable.size, done }),
    });
  } finally {
    KnightWatch?.endServe();
  }
  return true;
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
