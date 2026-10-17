package io.github.geckoterm.android

import android.annotation.SuppressLint
import android.app.Activity
import android.app.AlertDialog
import android.content.ActivityNotFoundException
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.PowerManager
import android.provider.Settings

/**
 * Keeping "Notify in the background" alive. Android's battery optimization
 * may hold the connection off while the phone sleeps, and some makers stop
 * apps (foreground services included) far more eagerly than Android does:
 * Xiaomi (MIUI, HyperOS) and the BBK phones (OnePlus, Oppo, Realme, Vivo)
 * need autostart and "no restrictions", Samsung puts unused apps to (deep)
 * sleep, Huawei and Honor have their own launch manager. What Gecko can do
 * is ask for the exemption and point at the right switch; the steps per
 * maker follow dontkillmyapp.com.
 */
object KeepAlive {
    enum class Maker(val site: String, val steps: Int) {
        SAMSUNG("samsung", R.string.keepalive_samsung),
        XIAOMI("xiaomi", R.string.keepalive_xiaomi),
        ONEPLUS("oneplus", R.string.keepalive_oneplus),
        OPPO("oppo", R.string.keepalive_oppo),
        VIVO("vivo", R.string.keepalive_vivo),
        HUAWEI("huawei", R.string.keepalive_huawei),
    }

    /** From Build.MANUFACTURER (and brand: a Redmi says Xiaomi, a POCO too). */
    fun maker(manufacturer: String = Build.MANUFACTURER, brand: String = Build.BRAND): Maker? {
        val m = "$manufacturer $brand".lowercase()
        return when {
            "samsung" in m -> Maker.SAMSUNG
            "xiaomi" in m || "redmi" in m || "poco" in m -> Maker.XIAOMI
            "oneplus" in m -> Maker.ONEPLUS
            "oppo" in m || "realme" in m -> Maker.OPPO
            "vivo" in m || "iqoo" in m -> Maker.VIVO
            "huawei" in m || "honor" in m -> Maker.HUAWEI
            else -> null
        }
    }

    fun exempt(ctx: Context): Boolean =
        ctx.getSystemService(PowerManager::class.java)?.isIgnoringBatteryOptimizations(ctx.packageName) ?: true

    /** Worth asking: not exempt yet, or a maker with its own switches. */
    fun needsHelp(ctx: Context) = !exempt(ctx) || maker() != null

    private const val ASKED = "keepalive_asked"

    /** After turning background notifications on: once, if it helps. */
    fun offerOnce(a: Activity) {
        val prefs = Gecko.prefs(a)
        if (prefs.getBoolean(ASKED, false) || !needsHelp(a)) return
        prefs.edit().putBoolean(ASKED, true).apply()
        show(a)
    }

    /** What to switch, with buttons to the right settings. */
    fun show(a: Activity) {
        val maker = maker()
        val text = buildString {
            append(a.getString(if (exempt(a)) R.string.keepalive_text_exempt else R.string.keepalive_text))
            if (maker != null) append("\n\n").append(a.getString(maker.steps))
        }
        val d = AlertDialog.Builder(a)
            .setTitle(R.string.keepalive_title)
            .setMessage(text)
            .setNegativeButton(R.string.keepalive_later, null)
        if (!exempt(a)) d.setPositiveButton(R.string.keepalive_allow) { _, _ -> askExemption(a) }
        else if (maker != null) d.setPositiveButton(R.string.keepalive_open) { _, _ -> openMakerSettings(a, maker) }
        if (maker != null) d.setNeutralButton(R.string.keepalive_more) { _, _ -> open(a, Intent(Intent.ACTION_VIEW, Uri.parse("https://dontkillmyapp.com/${maker.site}"))) }
        d.show()
    }

    @SuppressLint("BatteryLife") // the user turned on background notifications; this is what keeps them coming
    private fun askExemption(a: Activity) {
        val ask = Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS, Uri.parse("package:${a.packageName}"))
        if (!open(a, ask)) open(a, Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS))
    }

    /** The maker's own autostart or battery screen when there is one, else Gecko's app settings. */
    private fun openMakerSettings(a: Activity, maker: Maker) {
        val tries = when (maker) {
            Maker.XIAOMI -> listOf(ComponentName("com.miui.securitycenter", "com.miui.permcenter.autostart.AutoStartManagementActivity"))
            Maker.OPPO, Maker.ONEPLUS -> listOf(
                ComponentName("com.coloros.safecenter", "com.coloros.safecenter.permission.startup.StartupAppListActivity"),
                ComponentName("com.oplus.safecenter", "com.oplus.safecenter.permission.startup.StartupAppListActivity"),
            )
            Maker.VIVO -> listOf(ComponentName("com.vivo.permissionmanager", "com.vivo.permissionmanager.activity.BgStartUpManagerActivity"))
            Maker.HUAWEI -> listOf(ComponentName("com.huawei.systemmanager", "com.huawei.systemmanager.startupmgr.ui.StartupNormalAppListActivity"))
            Maker.SAMSUNG -> emptyList()
        }
        for (c in tries) if (open(a, Intent().setComponent(c))) return
        open(a, Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.parse("package:${a.packageName}")))
    }

    private fun open(a: Activity, i: Intent): Boolean = try {
        a.startActivity(i)
        true
    } catch (e: ActivityNotFoundException) {
        false
    } catch (e: SecurityException) {
        false
    }
}
