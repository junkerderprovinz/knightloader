const { withMainActivity } = require('expo/config-plugins');

const CALL = 'titleFromSubject(intent)';

const HELPERS = `
  override fun onNewIntent(intent: Intent) {
    ${CALL}
    super.onNewIntent(intent)
  }

  // expo-share-intent names a share from EXTRA_TITLE only, while browsers and
  // ShareCompat send the page title as EXTRA_SUBJECT.
  private fun titleFromSubject(intent: Intent?) {
    if (intent?.action != Intent.ACTION_SEND || !intent.getCharSequenceExtra(Intent.EXTRA_TITLE).isNullOrBlank()) return
    intent.getCharSequenceExtra(Intent.EXTRA_SUBJECT)?.let { intent.putExtra(Intent.EXTRA_TITLE, it) }
  }
`;

/** addSubjectFallback rewrites MainActivity.kt so a share's subject reaches the library as its title. */
function addSubjectFallback(src) {
  if (src.includes(CALL)) return src;
  const superOnCreate = /^([ \t]*)super\.onCreate\(/m;
  if (!superOnCreate.test(src) || !/^import android\.os\.Bundle$/m.test(src) || src.includes('fun onNewIntent(')) {
    throw new Error('withSharedSubject: MainActivity.kt no longer has the shape this plugin edits');
  }
  return (
    src
      .replace(/^import android\.os\.Bundle$/m, 'import android.content.Intent\nimport android.os.Bundle')
      .replace(superOnCreate, `$1${CALL}\n$1super.onCreate(`)
      .replace(/\}\s*$/, `${HELPERS}}\n`)
  );
}

module.exports = function withSharedSubject(config) {
  return withMainActivity(config, (cfg) => {
    if (cfg.modResults.language !== 'kt') throw new Error('withSharedSubject: MainActivity is not Kotlin');
    cfg.modResults.contents = addSubjectFallback(cfg.modResults.contents);
    return cfg;
  });
};
module.exports.addSubjectFallback = addSubjectFallback;
