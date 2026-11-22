import { KnightInstallSource } from '../modules/install-source';

/** The F-Droid clients that install from F-Droid's repository. */
const FDROID = new Set(['org.fdroid.fdroid', 'org.fdroid.basic', 'com.looker.droidify', 'com.machiav3lli.fdroid', 'eu.bubu1.fdroidclassic']);

/** The package installers a downloaded APK passes through, which no store is. */
const BY_HAND = new Set(['com.google.android.packageinstaller', 'com.android.packageinstaller']);

/** Where an app's updates come from, in the Version card's terms. */
export type InstallSource = { kind: 'store'; store?: string } | { kind: 'direct' };

/**
 * sourceOf names where the updates of an app installed by `installer` come
 * from: Google Play or F-Droid by name, another store without one, or the
 * release page for an APK installed by hand.
 */
export function sourceOf(installer: string | null | undefined): InstallSource {
  if (installer === 'com.android.vending') return { kind: 'store', store: 'Google Play' };
  if (installer && FDROID.has(installer)) return { kind: 'store', store: 'F-Droid' };
  if (!installer || BY_HAND.has(installer)) return { kind: 'direct' };
  return { kind: 'store' };
}

/** Where this copy came from, read once from Android's package manager. */
export const installSource = sourceOf(KnightInstallSource?.installer());
