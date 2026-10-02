package design.halleluja.knightloader.watch

import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.os.PowerManager
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import com.facebook.react.ReactApplication
import com.facebook.react.ReactInstanceEventListener
import com.facebook.react.bridge.Arguments
import com.facebook.react.bridge.ReactContext
import com.facebook.react.jstasks.HeadlessJsTaskConfig
import com.facebook.react.jstasks.HeadlessJsTaskContext
import com.facebook.react.jstasks.HeadlessJsTaskEventListener

/**
 * WatchService keeps the app's own connection to its instances while one of
 * them is busy. Each tick runs one pass of the JavaScript watch as a headless
 * task, which looks at every instance, posts what is news, and says when the
 * next tick should come or that the service can stop.
 *
 * Ticks come from a handler rather than a timer inside JavaScript: between
 * passes no task is active, so React Native stops driving its timers on every
 * frame and the process has nothing to do until the next one.
 */
class WatchService : Service(), HeadlessJsTaskEventListener {
  private val handler = Handler(Looper.getMainLooper())
  private val tick = Runnable { runPass() }
  private var wakeLock: PowerManager.WakeLock? = null
  private var tasks: HeadlessJsTaskContext? = null
  private var task: Int? = null

  override fun onBind(intent: Intent?): IBinder? = null

  override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
    val title = intent?.getStringExtra(EXTRA_TITLE).orEmpty()
    val text = intent?.getStringExtra(EXTRA_TEXT).orEmpty()
    ServiceCompat.startForeground(
      this,
      Notices.WATCH_ID,
      Notices.watching(this, title, text),
      if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
        ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE
      } else {
        0
      },
    )
    if (!running) {
      running = true
      handler.post(tick)
    }
    // Not sticky: Android does not let a service restarted from the background
    // go into the foreground, and opening the app starts it again anyway.
    return START_NOT_STICKY
  }

  override fun onDestroy() {
    running = false
    handler.removeCallbacks(tick)
    tasks?.removeTaskEventListener(this)
    tasks = null
    wakeLock?.let { if (it.isHeld) it.release() }
    wakeLock = null
    super.onDestroy()
  }

  private fun runPass() {
    if (!running) return
    // Held for as long as the service runs, because the handler's clock stops
    // while the phone sleeps and a captcha would wait for the screen to come
    // on. Each pass renews it, so it lapses by itself if the passes stop.
    val lock =
      wakeLock
        ?: (getSystemService(Context.POWER_SERVICE) as PowerManager)
          .newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "KnightLoader:watch")
          .apply { setReferenceCounted(false) }
          .also { wakeLock = it }
    lock.acquire(PASS_TIMEOUT_MS + MAX_DELAY_MS * 2)

    val host = (application as? ReactApplication)?.reactHost
    if (host == null) {
      stopSelf()
      return
    }
    val context = host.currentReactContext
    if (context != null) {
      startTask(context)
      return
    }
    // The activity is gone and took the JavaScript runtime with it. Starting
    // the host again brings up the runtime without a screen.
    host.addReactInstanceEventListener(
      object : ReactInstanceEventListener {
        override fun onReactContextInitialized(context: ReactContext) {
          host.removeReactInstanceEventListener(this)
          handler.post { startTask(context) }
        }
      },
    )
    host.start()
  }

  private fun startTask(context: ReactContext) {
    if (!running) return
    val next = HeadlessJsTaskContext.getInstance(context)
    if (tasks !== next) {
      tasks?.removeTaskEventListener(this)
      next.addTaskEventListener(this)
      tasks = next
    }
    try {
      task = next.startTask(HeadlessJsTaskConfig(TASK, Arguments.createMap(), PASS_TIMEOUT_MS, true))
    } catch (e: IllegalStateException) {
      task = null
      scheduleNext()
    }
  }

  override fun onHeadlessJsTaskStart(taskId: Int) = Unit

  override fun onHeadlessJsTaskFinish(taskId: Int) {
    if (taskId != task) return
    task = null
    scheduleNext()
  }

  private fun scheduleNext() {
    if (!running) return
    handler.removeCallbacks(tick)
    handler.postDelayed(tick, nextDelayMs.coerceIn(MIN_DELAY_MS, MAX_DELAY_MS))
  }

  companion object {
    /** The name index.ts registers the pass under. */
    const val TASK = "KnightLoaderWatch"

    private const val EXTRA_TITLE = "title"
    private const val EXTRA_TEXT = "text"
    private const val PASS_TIMEOUT_MS = 60_000L
    private const val MIN_DELAY_MS = 2_000L
    private const val MAX_DELAY_MS = 120_000L

    @Volatile var running = false
      private set

    @Volatile var nextDelayMs = 10_000L

    fun start(context: Context, title: String, text: String) {
      val intent =
        Intent(context, WatchService::class.java)
          .putExtra(EXTRA_TITLE, title)
          .putExtra(EXTRA_TEXT, text)
      ContextCompat.startForegroundService(context, intent)
    }

    fun stop(context: Context) {
      running = false
      context.stopService(Intent(context, WatchService::class.java))
    }
  }
}
