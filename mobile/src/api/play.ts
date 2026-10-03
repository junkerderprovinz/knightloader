import { Linking, Platform } from 'react-native';
import * as IntentLauncher from 'expo-intent-launcher';

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
