package design.halleluja.knightloader.installsource

import android.os.Build
import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.modules.ModuleDefinition

/**
 * The package of the store that installed this app, such as
 * com.android.vending for Google Play, which tells the Version card where its
 * updates come from. A file opened in the package installer leaves no store.
 */
class InstallSourceModule : Module() {
  override fun definition() = ModuleDefinition {
    Name("KnightInstallSource")

    Function("installer") {
      val context = appContext.reactContext ?: return@Function null
      val pm = context.packageManager
      if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
        pm.getInstallSourceInfo(context.packageName).installingPackageName
      } else {
        @Suppress("DEPRECATION")
        pm.getInstallerPackageName(context.packageName)
      }
    }
  }
}
