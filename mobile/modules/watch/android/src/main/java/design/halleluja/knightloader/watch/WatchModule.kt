package design.halleluja.knightloader.watch

import android.content.Context
import expo.modules.kotlin.exception.Exceptions
import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.modules.ModuleDefinition
import expo.modules.kotlin.records.Field
import expo.modules.kotlin.records.Record

class NoticeRecord : Record {
  @Field val id: Int = 0
  @Field val channel: String = Notices.CHANNEL_FINISHED
  @Field val title: String = ""
  @Field val text: String = ""
  @Field val detail: String? = null
  @Field val sub: String? = null
  @Field val silent: Boolean = false
  @Field val open: String = "downloads"
  @Field val connection: String = ""

  fun toNotice() = Notice(id, channel, title, text, detail, sub, silent, open, connection)
}

class WatchModule : Module() {
  private val context: Context
    get() = appContext.reactContext ?: throw Exceptions.ReactContextLost()

  // A tap that arrived through onNewIntent, held until JavaScript asks for it.
  @Volatile private var pending: OpenRequest? = null

  override fun definition() = ModuleDefinition {
    Name("KnightWatch")

    Events("onOpen")

    Function("channels") { captcha: String, finished: String, failed: String, watch: String ->
      Notices.channels(context, captcha, finished, failed, watch)
    }

    // False when Android refuses, which it does for an app that is not in front.
    Function("start") { title: String, text: String ->
      try {
        WatchService.start(context, title, text)
        true
      } catch (e: IllegalStateException) {
        false
      } catch (e: SecurityException) {
        false
      }
    }

    Function("stop") {
      WatchService.stop(context)
    }

    Function("running") {
      WatchService.running
    }

    Function("next") { delayMs: Double ->
      WatchService.nextDelayMs = delayMs.toLong()
    }

    Function("notify") { notice: NoticeRecord ->
      Notices.post(context, notice.toNotice())
    }

    Function("cancel") { id: Int ->
      Notices.cancel(context, id)
    }

    Function("enabled") {
      Notices.enabled(context)
    }

    Function("takeOpen") {
      pending?.let {
        pending = null
        return@Function it.toMap()
      }
      val intent = appContext.currentActivity?.intent
      val request = Notices.openRequest(intent) ?: return@Function null
      intent?.let { Notices.forget(it) }
      request.toMap()
    }

    OnNewIntent { intent ->
      val request = Notices.openRequest(intent) ?: return@OnNewIntent
      Notices.forget(intent)
      pending = request
      sendEvent("onOpen")
    }
  }
}
