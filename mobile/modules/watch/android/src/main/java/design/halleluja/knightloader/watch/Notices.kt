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

  const val CHANNEL_CAPTCHA = "captcha"
  const val CHANNEL_FINISHED = "finished"
  const val CHANNEL_FAILED = "failed"
  const val CHANNEL_WATCH = "watch"

  private const val EXTRA_OPEN = "knightloader.open"
  private const val EXTRA_CONNECTION = "knightloader.connection"

  /** Creates the channels, or renames them after a change of language. */
  fun channels(context: Context, captcha: String, finished: String, failed: String, watch: String) {
    val manager = NotificationManagerCompat.from(context)
    manager.createNotificationChannelsCompat(
      listOf(
        NotificationChannelCompat.Builder(CHANNEL_CAPTCHA, NotificationManagerCompat.IMPORTANCE_HIGH)
          .setName(captcha)
          .build(),
        NotificationChannelCompat.Builder(CHANNEL_FINISHED, NotificationManagerCompat.IMPORTANCE_DEFAULT)
          .setName(finished)
          .build(),
        NotificationChannelCompat.Builder(CHANNEL_FAILED, NotificationManagerCompat.IMPORTANCE_DEFAULT)
          .setName(failed)
          .build(),
        NotificationChannelCompat.Builder(CHANNEL_WATCH, NotificationManagerCompat.IMPORTANCE_LOW)
          .setName(watch)
          .setShowBadge(false)
          .build(),
      ),
    )
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
      .setContentText(text)
      .setContentIntent(openIntent(context, WATCH_ID, OpenRequest("downloads", "")))
      .setOngoing(true)
      .setSilent(true)
      .setShowWhen(false)
      .setCategory(NotificationCompat.CATEGORY_SERVICE)
      .setPriority(NotificationCompat.PRIORITY_LOW)
      .build()

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
