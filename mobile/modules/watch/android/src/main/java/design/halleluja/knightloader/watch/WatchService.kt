package design.halleluja.knightloader.watch

import android.app.AlarmManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.SharedPreferences
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.os.PowerManager
import android.os.SystemClock
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
 * WatchService keeps the app's own connection to its instances. Each tick runs
 * one pass of the JavaScript watch as a headless task, which looks at every
 * instance, posts what is news, and says when the next tick should come, whether
 * the phone should stay awake until then, or that the service can stop.
 *
 * Ticks come from native code rather than a timer inside JavaScript: between
 * passes no task is active, so React Native stops driving its timers on every
 * frame and the process has nothing to do until the next one. While something
 * runs, a handler times the ticks under a wake lock, since its clock stops while
 * the phone sleeps. While nothing does, the lock goes and an alarm wakes the
 * phone for the next look instead.
 */
class WatchService : Service(), HeadlessJsTaskEventListener {
  private val handler = Handler(Looper.getMainLooper())
  private val tick = Runnable { runPass() }
  private var wakeLock: PowerManager.WakeLock? = null
  private var tasks: HeadlessJsTaskContext? = null
  private var task: Int? = null

  override fun onBind(intent: Intent?): IBinder? = null

  override fun onCreate() {
    super.onCreate()
    instance = this
  }

  override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
    val store = store(this)
    val title = intent?.getStringExtra(EXTRA_TITLE) ?: store.getString(EXTRA_TITLE, null).orEmpty()
    val text = intent?.getStringExtra(EXTRA_TEXT) ?: store.getString(EXTRA_TEXT, null).orEmpty()
    try {
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
    } catch (e: RuntimeException) {
      // A restart Android does on its own, after it ended the process, is not
      // allowed into the foreground unless the app may ignore battery
      // optimisation. The app starts the service again when it is opened.
      stopSelf()
      return START_NOT_STICKY
    }
    if (!running) {
      running = true
      handler.post(tick)
    }
    return if (autostart(this)) START_STICKY else START_NOT_STICKY
  }

  override fun onDestroy() {
    running = false
    instance = null
    handler.removeCallbacks(tick)
    alarms().cancel(tickIntent(this))
    tasks?.removeTaskEventListener(this)
    tasks = null
    wakeLock?.let { if (it.isHeld) it.release() }
    wakeLock = null
    super.onDestroy()
  }

  /** Runs a pass now, for the alarm that ends a quiet wait. */
  internal fun tickNow() {
    if (!running || task != null) return
    handler.removeCallbacks(tick)
    runPass()
  }

  private fun runPass() {
    if (!running) return
    // The lock outlasts the longest wait the handler can be asked for, so a
    // watch whose passes stop lets go of it by itself.
    lock().acquire(PASS_TIMEOUT_MS + MAX_DELAY_MS * 2)

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
    // The activity is gone and took the JavaScript runtime with it, or the
    // phone has just started. Starting the host brings up the runtime without
    // a screen.
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
    val delay = nextDelayMs.coerceIn(MIN_DELAY_MS, MAX_DELAY_MS)
    handler.removeCallbacks(tick)
    alarms().cancel(tickIntent(this))
    if (nextAwake) {
      handler.postDelayed(tick, delay)
      return
    }
    // Inexact and allowed while idle, which needs no exact-alarm permission and
    // gets through Doze, if rarely. Android may deliver an inexact alarm up to
    // three quarters of its delay late, and in practice does, so it is set
    // early enough that the latest delivery lands on the delay asked for.
    alarms().setAndAllowWhileIdle(
      AlarmManager.ELAPSED_REALTIME_WAKEUP,
      SystemClock.elapsedRealtime() + delay * 4 / 7,
      tickIntent(this),
    )
    wakeLock?.let { if (it.isHeld) it.release() }
  }

  private fun lock(): PowerManager.WakeLock =
    wakeLock
      ?: (getSystemService(Context.POWER_SERVICE) as PowerManager)
        .newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "KnightLoader:watch")
        .apply { setReferenceCounted(false) }
        .also { wakeLock = it }

  private fun alarms(): AlarmManager = getSystemService(Context.ALARM_SERVICE) as AlarmManager

  companion object {
    /** The name index.ts registers the pass under. */
    const val TASK = "KnightLoaderWatch"

    private const val EXTRA_TITLE = "title"
    private const val EXTRA_TEXT = "text"
    private const val KEY_AUTOSTART = "autostart"
    private const val PASS_TIMEOUT_MS = 60_000L
    private const val MIN_DELAY_MS = 2_000L
    private const val MAX_DELAY_MS = 120_000L

    @Volatile var running = false
      private set

    @Volatile var nextDelayMs = 10_000L

    @Volatile var nextAwake = true

    @Volatile internal var instance: WatchService? = null
      private set

    // What the service needs when Android starts it without the app: after a
    // reboot, an update, or a restart of its own.
    fun store(context: Context): SharedPreferences =
      context.getSharedPreferences("knightloader.watch", Context.MODE_PRIVATE)

    fun autostart(context: Context): Boolean = store(context).getBoolean(KEY_AUTOSTART, false)

    fun setAutostart(context: Context, on: Boolean) {
      store(context).edit().putBoolean(KEY_AUTOSTART, on).apply()
    }

    fun start(context: Context, title: String, text: String) {
      store(context).edit().putString(EXTRA_TITLE, title).putString(EXTRA_TEXT, text).apply()
      val intent =
        Intent(context, WatchService::class.java)
          .putExtra(EXTRA_TITLE, title)
          .putExtra(EXTRA_TEXT, text)
      ContextCompat.startForegroundService(context, intent)
    }

    /** Starts the service from what the app last stored, for the boot receiver. */
    fun startStored(context: Context) {
      ContextCompat.startForegroundService(context, Intent(context, WatchService::class.java))
    }

    fun stop(context: Context) {
      running = false
      context.stopService(Intent(context, WatchService::class.java))
    }

    private fun tickIntent(context: Context): PendingIntent =
      PendingIntent.getBroadcast(
        context,
        0,
        Intent(context, WatchTick::class.java),
        PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
      )
  }
}
