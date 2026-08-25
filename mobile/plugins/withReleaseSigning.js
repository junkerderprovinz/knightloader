const { withAppBuildGradle } = require('expo/config-plugins');

/**
 * Gives the release build a signing config of its own.
 *
 * `expo prebuild` generates an app/build.gradle whose RELEASE buildType signs
 * with `signingConfigs.debug` - the template even carries the warning:
 *
 *     release {
 *         // Caution! In production, you need to generate your own keystore file.
 *         signingConfig signingConfigs.debug
 *
 * That keystore is `androiddebugkey` with the password `android`, the same one
 * on every Android developer's machine on earth. An APK published with it is
 * not merely "unsigned in spirit": anyone can build an update Android will
 * accept as coming from us, and Play refuses it outright.
 *
 * It also cannot be corrected later without hurting people. Android identifies
 * an app by (package name, signing certificate), so changing the key turns
 * version N+1 into a different app: every installed copy has to be uninstalled
 * first, and its settings and paired instances go with it. There is exactly one
 * safe moment to get this right, and it is before the first release anybody
 * installs.
 *
 * android/ is generated rather than committed, so this cannot be a one-line
 * edit to a checked-in gradle file. A plugin is the version of that edit which
 * survives `prebuild --clean`, and it applies the same way on a laptop as in
 * CI instead of living in one workflow's sed.
 *
 * The credentials come from the environment, never from the repository:
 *
 *   KL_ANDROID_KEYSTORE        path to the keystore (absolute, or relative to android/app)
 *   KL_ANDROID_STORE_PASSWORD  its password
 *   KL_ANDROID_KEY_ALIAS       the key inside it
 *   KL_ANDROID_KEY_PASSWORD    that key's password
 *
 * With none of them set (or set empty) the build falls back to the debug key, which is right
 * for `npm run android` on a laptop and would be quietly wrong for a release.
 * So the fallback is NOT the safety net here: release-mobile.yml refuses to
 * build a tag without the secrets at all, and then verifies the finished APK's
 * certificate is not the debug one. A build that silently produced the wrong
 * artifact is the failure this is guarding against, and an env var being set is
 * not proof that it did not happen - the certificate in the APK is.
 */
module.exports = function withReleaseSigning(config) {
  return withAppBuildGradle(config, (cfg) => {
    if (cfg.modResults.language !== 'groovy') {
      throw new Error(
        `withReleaseSigning: app/build.gradle is ${cfg.modResults.language}, expected groovy - the template changed and this plugin needs rewriting rather than silently doing nothing.`,
      );
    }

    let src = cfg.modResults.contents;

    // Anchored on the generated block rather than on a line number: if the
    // Expo template stops producing it, this throws instead of leaving a
    // release quietly signed with the debug key.
    const debugBlock = `        debug {
            storeFile file('debug.keystore')`;
    if (!src.includes(debugBlock)) {
      throw new Error(
        "withReleaseSigning: could not find the generated debug signingConfig in app/build.gradle - the Expo template changed.",
      );
    }
    src = src.replace(
      debugBlock,
      `        release {
            // Absolute path, or one relative to android/app. Left entirely
            // unset when the environment carries nothing, so configuring this
            // block cannot fail a local build that is never going to use it.
            // Groovy truth, not a null check: the workflow sets this to the
            // EMPTY string on a non-tag build, and "" is not null.
            def ks = System.getenv('KL_ANDROID_KEYSTORE')
            if (ks) {
                storeFile file(ks)
                storePassword System.getenv('KL_ANDROID_STORE_PASSWORD')
                keyAlias System.getenv('KL_ANDROID_KEY_ALIAS')
                keyPassword System.getenv('KL_ANDROID_KEY_PASSWORD')
            }
        }
${debugBlock}`,
    );

    const releaseSign = `        release {
            // Caution! In production, you need to generate your own keystore file.
            // see https://reactnative.dev/docs/signed-apk-android.
            signingConfig signingConfigs.debug`;
    if (!src.includes(releaseSign)) {
      throw new Error(
        "withReleaseSigning: could not find the release buildType's debug signingConfig - the Expo template changed.",
      );
    }
    src = src.replace(
      releaseSign,
      `        release {
            // The real key when the environment carries one, the debug key
            // otherwise. A laptop build stays a one-command build; a release
            // is stopped long before here when the secrets are missing (see
            // .github/workflows/release-mobile.yml), and the APK's own
            // certificate is checked afterwards.
            signingConfig System.getenv('KL_ANDROID_KEYSTORE') ? signingConfigs.release : signingConfigs.debug`,
    );

    cfg.modResults.contents = src;
    return cfg;
  });
};
