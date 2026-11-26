package design.halleluja.knightloader.watch

import android.annotation.SuppressLint
import android.app.Notification
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import androidx.core.app.NotificationChannelCompat
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat

/** One notification as the JavaScript side describes it. */
data class Notice(
  val id: Int,
  val channel: String,
  val title: String,
  val text: String,
  val detail: String?,
  val sub: String?,
  val silent: Boolean,
  val open: String,
  val connection: String,
)

/** What a tapped notification asks the app to show. */
data class OpenRequest(val open: String, val connection: String) {
  fun toMap(): Map<String, String> = mapOf("open" to open, "connection" to connection)
}

object Notices {
  const val WATCH_ID = 1
  const val SERVE_ID = 2

  const val CHANNEL_CAPTCHA = "captcha"
  const val CHANNEL_FINISHED = "finished"
  const val CHANNEL_FAILED = "failed"
  const val CHANNEL_UNPACKED = "unpacked"
  const val CHANNEL_UNPACK_FAILED = "unpack-failed"
  const val CHANNEL_ACCOUNT_PAUSED = "account-paused"
  const val CHANNEL_ACCOUNT_BACK = "account-back"
  // A channel's importance cannot be lowered once it exists, so the quiet one
  // has a new id and the old one is deleted.
  const val CHANNEL_WATCH = "watch-quiet"
  private const val CHANNEL_WATCH_OLD = "watch"

  private const val EXTRA_OPEN = "knightloader.open"
  private const val EXTRA_CONNECTION = "knightloader.connection"

  // How loud each kind of news is. A captcha holds a download up until somebody
  // answers it, and the rest can wait for a look.
  private val importance =
    mapOf(
      CHANNEL_CAPTCHA to NotificationManagerCompat.IMPORTANCE_HIGH,
      CHANNEL_FINISHED to NotificationManagerCompat.IMPORTANCE_DEFAULT,
      CHANNEL_FAILED to NotificationManagerCompat.IMPORTANCE_DEFAULT,
      CHANNEL_UNPACKED to NotificationManagerCompat.IMPORTANCE_DEFAULT,
      CHANNEL_UNPACK_FAILED to NotificationManagerCompat.IMPORTANCE_DEFAULT,
      CHANNEL_ACCOUNT_PAUSED to NotificationManagerCompat.IMPORTANCE_DEFAULT,
      CHANNEL_ACCOUNT_BACK to NotificationManagerCompat.IMPORTANCE_DEFAULT,
    )

  /** Creates the channels under `names`, keyed by channel id with "watch" for
   *  the service's own, or renames them after a change of language. */
  fun channels(context: Context, names: Map<String, String>) {
    val manager = NotificationManagerCompat.from(context)
    val news =
      importance.mapNotNull { (id, level) ->
        names[id]?.let { NotificationChannelCompat.Builder(id, level).setName(it).build() }
      }
    val watch =
      names["watch"]?.let {
        NotificationChannelCompat.Builder(CHANNEL_WATCH, NotificationManagerCompat.IMPORTANCE_MIN)
          .setName(it)
          .setShowBadge(false)
          .build()
      }
    manager.createNotificationChannelsCompat(news + listOfNotNull(watch))
    manager.deleteNotificationChannel(CHANNEL_WATCH_OLD)
  }

  fun enabled(context: Context): Boolean = NotificationManagerCompat.from(context).areNotificationsEnabled()

  // The permission is checked through enabled(), which also covers a person who
  // switched the app's notifications off in the system settings.
  @SuppressLint("MissingPermission")
  fun post(context: Context, notice: Notice) {
    if (!enabled(context)) return
    val captcha = notice.channel == CHANNEL_CAPTCHA
    val builder =
      NotificationCompat.Builder(context, notice.channel)
        .setSmallIcon(R.drawable.knightloader_watch)
        .setContentTitle(notice.title)
        .setContentText(notice.text)
        .setSubText(notice.sub)
        .setContentIntent(openIntent(context, notice.id, OpenRequest(notice.open, notice.connection)))
        .setAutoCancel(true)
        .setSilent(notice.silent)
        .setCategory(if (captcha) NotificationCompat.CATEGORY_REMINDER else NotificationCompat.CATEGORY_STATUS)
        .setPriority(if (captcha) NotificationCompat.PRIORITY_HIGH else NotificationCompat.PRIORITY_DEFAULT)
        // Below Android 8 the defaults stand in for the channel, and vibration
        // would need a permission the app does not ask for.
        .setDefaults(NotificationCompat.DEFAULT_SOUND or NotificationCompat.DEFAULT_LIGHTS)
    notice.detail?.let { builder.setStyle(NotificationCompat.BigTextStyle().bigText(it)) }
    NotificationManagerCompat.from(context).notify(notice.id, builder.build())
  }

  fun cancel(context: Context, id: Int) {
    NotificationManagerCompat.from(context).cancel(id)
  }

  /** The quiet notification Android requires while the service runs. */
  fun watching(context: Context, title: String, text: String): Notification =
    NotificationCompat.Builder(context, CHANNEL_WATCH)
      .setSmallIcon(R.drawable.knightloader_watch)
      .setContentTitle(title)
      .setContentText(text.ifEmpty { null })
      .setContentIntent(openIntent(context, WATCH_ID, OpenRequest("downloads", "")))
      .setOngoing(true)
      .setSilent(true)
      .setOnlyAlertOnce(true)
      .setShowWhen(false)
      .setCategory(NotificationCompat.CATEGORY_SERVICE)
      .setPriority(NotificationCompat.PRIORITY_MIN)
      .build()

  /** The quiet notification Android requires while a player reads a file from the app. */
  fun serving(context: Context, title: String, text: String): Notification =
    NotificationCompat.Builder(context, CHANNEL_WATCH)
      .setSmallIcon(R.drawable.knightloader_watch)
      .setContentTitle(title)
      .setContentText(text.ifEmpty { null })
      .setOngoing(true)
      .setSilent(true)
      .setShowWhen(false)
      .setCategory(NotificationCompat.CATEGORY_SERVICE)
      .setPriority(NotificationCompat.PRIORITY_MIN)
      .build()

  /** The words on the service's notification, set by the service while it runs. */
  @Volatile internal var watchingShown: Pair<String, String>? = null

  /** Puts new words on the service's notification while it runs. */
  @SuppressLint("MissingPermission")
  fun updateWatching(context: Context, title: String, text: String) {
    if (!WatchService.running || !enabled(context)) return
    // Some phones pop a notification up again whenever it is posted, however
    // quiet its channel, so the same words are not posted twice.
    if (watchingShown == title to text) return
    watchingShown = title to text
    NotificationManagerCompat.from(context).notify(WATCH_ID, watching(context, title, text))
  }

  fun openRequest(intent: Intent?): OpenRequest? {
    val open = intent?.getStringExtra(EXTRA_OPEN) ?: return null
    return OpenRequest(open, intent.getStringExtra(EXTRA_CONNECTION) ?: "")
  }

  /** Removes the request from an intent the activity keeps, so a recreated
   *  activity does not act on it twice. */
  fun forget(intent: Intent) {
    intent.removeExtra(EXTRA_OPEN)
    intent.removeExtra(EXTRA_CONNECTION)
  }

  // The launch intent rather than a named activity, so this module does not
  // have to know the app's class names. MainActivity is singleTask, so a
  // running app receives it through onNewIntent.
  private fun openIntent(context: Context, requestCode: Int, request: OpenRequest): PendingIntent? {
    val launch = context.packageManager.getLaunchIntentForPackage(context.packageName) ?: return null
    launch.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP)
    launch.putExtra(EXTRA_OPEN, request.open)
    launch.putExtra(EXTRA_CONNECTION, request.connection)
    return PendingIntent.getActivity(
      context,
      requestCode,
      launch,
      PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
    )
  }
}
