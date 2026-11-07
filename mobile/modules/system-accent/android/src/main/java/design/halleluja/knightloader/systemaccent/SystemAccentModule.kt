package design.halleluja.knightloader.systemaccent

import android.content.ComponentCallbacks
import android.content.res.Configuration
import android.os.Build
import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.modules.ModuleDefinition

/**
 * Material You's primary as Material 3 takes it for the light and the dark
 * look, the source of the wand swatch. Android 12 and newer build the palette
 * from the wallpaper; below 12 there is none.
 */
class SystemAccentModule : Module() {
  // A new wallpaper brings a new palette as a configuration change, which can
  // arrive while the app is in front.
  private val paletteWatch = object : ComponentCallbacks {
    override fun onConfigurationChanged(newConfig: Configuration) {
      sendEvent("onChange")
    }

    @Deprecated("Deprecated in Java")
    override fun onLowMemory() {}
  }

  override fun definition() = ModuleDefinition {
    Name("KnightAccent")

    Events("onChange")

    OnCreate {
      appContext.reactContext?.registerComponentCallbacks(paletteWatch)
    }

    OnDestroy {
      appContext.reactContext?.unregisterComponentCallbacks(paletteWatch)
    }

    // ARGB ints, or null below Android 12.
    Function("tones") {
      val context = appContext.reactContext
      if (context == null || Build.VERSION.SDK_INT < Build.VERSION_CODES.S) {
        null
      } else {
        mapOf(
          "light" to context.getColor(android.R.color.system_accent1_600),
          "dark" to context.getColor(android.R.color.system_accent1_200),
        )
      }
    }
  }
}
