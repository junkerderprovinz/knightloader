import { Linking, Platform } from 'react-native';
import * as IntentLauncher from 'expo-intent-launcher';
import { fetchPlayable, fetchRange } from './client';
import { mediaKind } from './media';
import type { ServerConnection, Task } from './types';
import { openLoopback } from '../play/loopback';

/**
 * playTask opens task's file in a player app. The player reads it from this
 * phone, on 127.0.0.1, and the phone fetches what it reads from the instance
 * through the group, so the player needs no address of the instance.
 */
export async function playTask(conn: ServerConnection, task: Task, base = '/api'): Promise<void> {
  const playable = await fetchPlayable(conn, task.id, base);
  const way = await openLoopback({
    size: playable.size,
    type: playable.type,
    read: (from, length) => fetchRange(conn, task.id, playable.file, from, length, base),
  });
  try {
    await openInPlayer(way.url, mediaKind(task) ?? 'video');
  } catch (e) {
    way.close();
    throw e;
  }
}

/**
 * openInPlayer hands url to a player app on Android, or to the browser where
 * no player app takes it.
 */
export async function openInPlayer(url: string, kind: 'audio' | 'video'): Promise<void> {
  if (Platform.OS === 'android') {
    // Not awaited: the promise settles only once the player is closed. It
    // fails straight away when no app plays the type, and then the browser
    // gets the link.
    IntentLauncher.startActivityAsync('android.intent.action.VIEW', { data: url, type: `${kind}/*` }).catch(() =>
      Linking.openURL(url).catch(() => undefined),
    );
    return;
  }
  await Linking.openURL(url);
}
