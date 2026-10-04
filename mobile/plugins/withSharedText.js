const { withMainActivity } = require('expo/config-plugins');

const CALL = 'fillShareExtras(intent)';

// The same text as UNREADABLE_FILE in src/screens/ShareScreen.tsx.
const UNREADABLE_FILE = 'knightloader:unreadable-shared-file';

const IMPORTS = [
  'android.content.ContentResolver',
  'android.content.Intent',
  'android.content.pm.PackageManager',
  'android.net.Uri',
  'android.os.Bundle',
  'java.io.IOException',
];

const HELPERS = `
  override fun onNewIntent(intent: Intent) {
    ${CALL}
    super.onNewIntent(intent)
  }

  // expo-share-intent reads a text share from EXTRA_TITLE and EXTRA_TEXT only.
  // Browsers and ShareCompat send the page title as EXTRA_SUBJECT, and a file
  // manager sends a text file as text/plain with the file in EXTRA_STREAM.
  private fun fillShareExtras(intent: Intent?) {
    if (intent?.action != Intent.ACTION_SEND) return
    if (intent.getCharSequenceExtra(Intent.EXTRA_TITLE).isNullOrBlank()) {
      intent.getCharSequenceExtra(Intent.EXTRA_SUBJECT)?.let { intent.putExtra(Intent.EXTRA_TITLE, it) }
    }
    if (intent.type?.startsWith("text/plain") == true && intent.getStringExtra(Intent.EXTRA_TEXT).isNullOrBlank()) {
      val uri = sharedUri(intent) ?: return
      // Without text the library hands the app nothing at all, so a file that
      // gives none is passed on as a marker the share screen explains.
      intent.putExtra(Intent.EXTRA_TEXT, sharedFileText(uri)?.takeIf { it.isNotBlank() } ?: UNREADABLE_FILE)
    }
  }

  private fun sharedUri(intent: Intent): Uri? =
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
      intent.getParcelableExtra(Intent.EXTRA_STREAM, Uri::class.java)
    } else {
      @Suppress("DEPRECATION") intent.getParcelableExtra<Uri>(Intent.EXTRA_STREAM)
    }

  // Only another app's content is read: a file path or one of this app's own
  // providers would let any app have this one show its private files.
  private fun sharedFileText(uri: Uri): String? {
    if (uri.scheme != ContentResolver.SCHEME_CONTENT) return null
    @Suppress("DEPRECATION")
    val own = packageManager.getPackageInfo(packageName, PackageManager.GET_PROVIDERS).providers.orEmpty()
    if (own.any { uri.authority in it.authority.orEmpty().split(';') }) return null
    return try {
      contentResolver.openInputStream(uri)?.use { input ->
        val bytes = ByteArray(SHARED_TEXT_LIMIT + 1)
        var n = 0
        while (n < bytes.size) {
          val read = input.read(bytes, n, bytes.size - n)
          if (read < 0) break
          n += read
        }
        val text = String(bytes, 0, minOf(n, SHARED_TEXT_LIMIT), Charsets.UTF_8)
        if (n > SHARED_TEXT_LIMIT) text.substringBeforeLast('\\n') else text
      }
    } catch (e: IOException) {
      null
    } catch (e: SecurityException) {
      null
    }
  }

  // The text travels on in the intent, which the library passes to
  // startActivity when the share reaches an activity that is not the task's
  // root, and an intent near 1 MB fails there.
  private companion object {
    const val SHARED_TEXT_LIMIT = 256 * 1024
    const val UNREADABLE_FILE = "${UNREADABLE_FILE}"
  }
`;

/** addShareFallbacks rewrites MainActivity.kt so a share's subject and a shared text file reach the library as title and text. */
function addShareFallbacks(src) {
  if (src.includes(CALL)) return src;
  const superOnCreate = /^([ \t]*)super\.onCreate\(/m;
  if (!superOnCreate.test(src) || !/^import android\.os\.Bundle$/m.test(src) || src.includes('fun onNewIntent(') || src.includes('companion object')) {
    throw new Error('withSharedText: MainActivity.kt no longer has the shape this plugin edits');
  }
  return src
    .replace(/^import android\.os\.Bundle$/m, IMPORTS.map((name) => `import ${name}`).join('\n'))
    .replace(superOnCreate, `$1${CALL}\n$1super.onCreate(`)
    .replace(/\}\s*$/, `${HELPERS}}\n`);
}

module.exports = function withSharedText(config) {
  return withMainActivity(config, (cfg) => {
    if (cfg.modResults.language !== 'kt') throw new Error('withSharedText: MainActivity is not Kotlin');
    cfg.modResults.contents = addShareFallbacks(cfg.modResults.contents);
    return cfg;
  });
};
module.exports.addShareFallbacks = addShareFallbacks;
