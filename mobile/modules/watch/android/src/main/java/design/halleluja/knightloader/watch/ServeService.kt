package design.halleluja.knightloader.watch

import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat

/**
 * ServeService keeps the app in the foreground while a player app reads a file
 * from it on 127.0.0.1. Once the player is in front, Android counts the app as
 * cached and freezes it, and the player waits for bytes that never come.
 */
class ServeService : Service() {
  override fun onBind(intent: Intent?): IBinder? = null

  override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
    val title = intent?.getStringExtra(EXTRA_TITLE).orEmpty()
    val text = intent?.getStringExtra(EXTRA_TEXT).orEmpty()
    try {
      ServiceCompat.startForeground(
        this,
        Notices.SERVE_ID,
        Notices.serving(this, title, text),
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
          ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE
        } else {
          0
        },
      )
    } catch (e: RuntimeException) {
      stopSelf()
    }
    return START_NOT_STICKY
  }

  companion object {
    private const val EXTRA_TITLE = "knightloader.serve.title"
    private const val EXTRA_TEXT = "knightloader.serve.text"

    fun start(context: Context, title: String, text: String) {
      val intent = Intent(context, ServeService::class.java)
        .putExtra(EXTRA_TITLE, title)
        .putExtra(EXTRA_TEXT, text)
      ContextCompat.startForegroundService(context, intent)
    }

    fun stop(context: Context) {
      context.stopService(Intent(context, ServeService::class.java))
    }
  }
}
