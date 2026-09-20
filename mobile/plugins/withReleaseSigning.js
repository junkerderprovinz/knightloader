const { withAppBuildGradle } = require('expo/config-plugins');

/**
 * Gives the release build a signing config of its own.
 *
 * `expo prebuild` generates an app/build.gradle whose release buildType signs
 * with `signingConfigs.debug`. That keystore is `androiddebugkey` with the
 * password `android`, the same one on every Android developer's machine, so
 * anyone could build an update Android accepts as coming from us, and Play
 * refuses such an APK outright.
 *
 * It cannot be corrected later without hurting people either. Android
 * identifies an app by package name and signing certificate, so changing the
 * key turns version N+1 into a different app: every installed copy has to be
 * uninstalled first, taking its settings and paired instances with it.
 *
 * android/ is generated rather than committed, so this cannot be an edit to a
 * checked-in gradle file. A plugin survives `prebuild --clean` and applies the
 * same way on a laptop as in CI, instead of living in one workflow's sed.
 *
 * The credentials come from the environment, never from the repository:
 *
 *   KL_ANDROID_KEYSTORE        path to the keystore (absolute, or relative to android/app)
 *   KL_ANDROID_STORE_PASSWORD  its password
 *   KL_ANDROID_KEY_ALIAS       the key inside it
 *   KL_ANDROID_KEY_PASSWORD    that key's password
 *
 * With none of them set the build falls back to the debug key, which is right
 * for `npm run android` on a laptop. The fallback is not the safety net for a
 * release: release-mobile.yml refuses to build a tag without the secrets, and
 * then verifies the finished APK's certificate is not the debug one, because an
 * env var being set is no proof of what ended up in the artifact.
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
            // Absolute path, or one relative to android/app. Left unset when
            // the environment carries nothing, so configuring this block cannot
            // fail a local build that is never going to use it. Groovy truth
            // rather than a null check, because the workflow sets this to the
            // empty string on a non-tag build.
            def ks = System.getenv('KL_ANDROID_KEYSTORE')
            if (ks) {
                storeFile file(ks)
                storePassword System.getenv('KL_ANDROID_STORE_PASSWORD')
                keyAlias System.getenv('KL_ANDROID_KEY_ALIAS')
                keyPassword System.getenv('KL_ANDROID_KEY_PASSWORD')
                // AGP enables v2 and leaves v3 off. v3 is the scheme that
                // supports key rotation, the only way a compromised or lost
                // signing key can be replaced without every installed copy
                // having to be uninstalled first. v1 stays off: minSdk is 24,
                // so nothing reads it.
                enableV2Signing true
                enableV3Signing true
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
