import { KnightInstallSource } from '../modules/install-source';

/** The F-Droid clients that install from F-Droid's repository. */
const FDROID = new Set(['org.fdroid.fdroid', 'org.fdroid.basic', 'com.looker.droidify', 'com.machiav3lli.fdroid', 'eu.bubu1.fdroidclassic']);

/**
 * storeOf names where the updates of an app installed by `installer` come
 * from: Google Play, F-Droid, or for an APK installed by hand the releases on
 * GitHub, which is where it came from.
 */
export function storeOf(installer: string | null | undefined): string {
  if (installer === 'com.android.vending') return 'Google Play';
  if (installer && FDROID.has(installer)) return 'F-Droid';
  return 'GitHub Releases';
}

/** The store this copy came from, read once from Android's package manager. */
export const installStore = storeOf(KnightInstallSource?.installer());
