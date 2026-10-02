import { Linking, Platform } from 'react-native';
import * as IntentLauncher from 'expo-intent-launcher';
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
 * file's own kind, or video for a torrent of several files, of which the
 * instance plays the largest selected one that is audio or video.
 */
export function mediaKind(task: Task): 'audio' | 'video' | null {
  const kind = MEDIA[extensionOf(task.name)];
  if (kind) return kind;
  return (task.torrentFileCount ?? 0) > 1 ? 'video' : null;
}

/** Whether there is anything to play yet. A running download streams. */
export function hasSomethingToPlay(task: Task): boolean {
  return task.status === 'running' || task.status === 'done' || task.loaded > 0;
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
