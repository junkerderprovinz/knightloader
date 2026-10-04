package design.halleluja.knightloader.watch

import android.content.ActivityNotFoundException
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.PowerManager
import android.provider.Settings

/**
 * The settings that decide whether Android lets the watch run in the
 * background.
 *
 * The app opens Android's battery optimisation list rather than asking for the
 * exemption directly. The direct request needs REQUEST_IGNORE_BATTERY_OPTIMIZATIONS,
 * which Google Play allows only for a short list of app kinds, and one build
 * goes to both stores.
 */
object Background {
  fun exempt(context: Context): Boolean =
    (context.getSystemService(Context.POWER_SERVICE) as PowerManager)
      .isIgnoringBatteryOptimizations(context.packageName)

  fun openBatterySettings(context: Context) {
    open(context, listOf(Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS)))
  }

  /** The maker whose own background rules a phone carries, by the name to show,
   *  or null for one that follows Android's. */
  fun vendor(): String? = current()?.name

  /** Opens the maker's page for background activity or autostart, else this
   *  app's details. Many of these pages move between versions, so each is tried
   *  in turn. */
  fun openVendorSettings(context: Context) {
    val pages = current()?.pages.orEmpty()
    open(context, pages.map { (pkg, cls) -> Intent().setComponent(ComponentName(pkg, cls)) })
  }

  private fun current(): Vendor? = Build.MANUFACTURER.lowercase().let { m -> VENDORS.firstOrNull { m in it.brands } }

  private fun open(context: Context, intents: List<Intent>) {
    val details =
      Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.parse("package:${context.packageName}"))
    for (intent in intents + details) {
      try {
        context.startActivity(intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        return
      } catch (e: ActivityNotFoundException) {
        continue
      } catch (e: SecurityException) {
        continue
      }
    }
  }

  private class Vendor(val name: String, val brands: Set<String>, val pages: List<Pair<String, String>>)

  // From dontkillmyapp.com and the pages its readers report, newest first.
  private val VENDORS =
    listOf(
      Vendor(
        "Xiaomi",
        setOf("xiaomi", "redmi", "poco"),
        listOf(
          "com.miui.securitycenter" to "com.miui.permcenter.autostart.AutoStartManagementActivity",
          "com.miui.powerkeeper" to "com.miui.powerkeeper.ui.HiddenAppsConfigActivity",
        ),
      ),
      Vendor(
        "OnePlus",
        setOf("oneplus"),
        listOf(
          "com.oneplus.security" to "com.oneplus.security.chainlaunch.view.ChainLaunchAppListActivity",
          "com.coloros.safecenter" to "com.coloros.safecenter.startupapp.StartupAppListActivity",
        ),
      ),
      Vendor(
        "OPPO",
        setOf("oppo", "realme"),
        listOf(
          "com.coloros.safecenter" to "com.coloros.safecenter.permission.startup.StartupAppListActivity",
          "com.coloros.safecenter" to "com.coloros.safecenter.startupapp.StartupAppListActivity",
          "com.oppo.safe" to "com.oppo.safe.permission.startup.StartupAppListActivity",
        ),
      ),
      Vendor(
        "vivo",
        setOf("vivo", "iqoo"),
        listOf(
          "com.vivo.permissionmanager" to "com.vivo.permissionmanager.activity.BgStartUpManagerActivity",
          "com.iqoo.secure" to "com.iqoo.secure.ui.phoneoptimize.BgStartUpManager",
        ),
      ),
      Vendor(
        "Huawei",
        setOf("huawei", "honor"),
        listOf(
          "com.huawei.systemmanager" to "com.huawei.systemmanager.startupmgr.ui.StartupNormalAppListActivity",
          "com.huawei.systemmanager" to "com.huawei.systemmanager.optimize.process.ProtectActivity",
        ),
      ),
      Vendor(
        "Samsung",
        setOf("samsung"),
        listOf(
          "com.samsung.android.lool" to "com.samsung.android.sm.battery.ui.BatteryActivity",
          "com.samsung.android.lool" to "com.samsung.android.sm.ui.battery.BatteryActivity",
        ),
      ),
      Vendor("Asus", setOf("asus"), listOf("com.asus.mobilemanager" to "com.asus.mobilemanager.entry.FunctionActivity")),
    )
}
