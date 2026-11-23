import { Linking, Platform } from 'react-native';
import * as IntentLauncher from 'expo-intent-launcher';
import { fetchPlayable, fetchRange } from './client';
import { mediaKind } from './media';
import type { ServerConnection, Task } from './types';
import { StreamBusy, directReader, type RangeReader } from '../play/direct';
import { openLoopback } from '../play/loopback';
import { KnightWatch } from '../../modules/watch';

/**
 * playTask opens task's file in a player app. The player reads it from this
 * phone, on 127.0.0.1, and the phone fetches what it reads from the instance
 * through the group, so the player needs no address of the instance.
 */
export async function playTask(conn: ServerConnection, task: Task, title: string, base = '/api'): Promise<void> {
  const playable = await fetchPlayable(conn, task.id, base);
  const relayed: RangeReader = (from, length) => fetchRange(conn, task.id, playable.file, from, length, base);
  // On the instance's own network the file comes as a stream, straight from
  // it. A piece the instance is too busy to stream goes through the relay, and
  // any other failure plays the rest through the relay.
  let direct = await directReader(conn, task.id, playable.file, base).catch(() => null);
  // Once the player is in front, Android freezes this app as cached, and the
  // player would wait for bytes forever. A foreground service keeps it awake
  // for as long as the player reads.
  KnightWatch?.serve(title, task.name);
  const read = async (from: number, length: number) => {
    if (direct) {
      try {
        return await direct(from, length);
      } catch (e) {
        if (!(e instanceof StreamBusy)) direct = null;
      }
    }
    return relayed(from, length);
  };
  const way = await openLoopback({ size: playable.size, type: playable.type, read }, () => KnightWatch?.endServe()).catch(
    (e: unknown) => {
      KnightWatch?.endServe();
      throw e;
    },
  );
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
