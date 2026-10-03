package design.halleluja.knightloader.watch

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

/** Ends a quiet wait. Android holds the phone awake until onReceive returns,
 *  and the pass takes its own wake lock before that. */
class WatchTick : BroadcastReceiver() {
  override fun onReceive(context: Context, intent: Intent) {
    WatchService.instance?.tickNow()
  }
}

/**
 * Brings "Stay connected" back after a reboot or an update of the app. Both
 * broadcasts are among the cases where Android 14 and 15 still let an app start
 * a foreground service from the background; Android 15's list of types a boot
 * receiver may not start leaves specialUse out.
 */
class WatchBoot : BroadcastReceiver() {
  override fun onReceive(context: Context, intent: Intent) {
    when (intent.action) {
      Intent.ACTION_BOOT_COMPLETED, Intent.ACTION_MY_PACKAGE_REPLACED -> Unit
      else -> return
    }
    if (!WatchService.autostart(context) || !Notices.enabled(context)) return
    try {
      WatchService.startStored(context)
    } catch (e: RuntimeException) {
      // Refused after all, on a phone whose maker added rules of its own. The
      // next time the app is opened starts it.
    }
  }
}
