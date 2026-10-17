package io.github.geckoterm.android

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.os.Handler
import android.os.Looper
import android.webkit.JavascriptInterface
import java.util.concurrent.FutureTask
import java.util.concurrent.TimeUnit

/**
 * window.GeckoAndroid in the web UI (see web/src/util.js and main.js). The
 * web view only ever shows the machine's own origin, and each call checks
 * that the page asking is that origin.
 */
class Bridge(private val activity: TerminalActivity) {
    private val main = Handler(Looper.getMainLooper())

    private fun trusted(): Boolean = activity.pageTrusted

    /** Runs on the main thread and waits for it: the bridge is called on a web view thread. */
    private fun <T> onMain(f: () -> T): T? {
        val task = FutureTask(f)
        main.post(task)
        return try { task.get(3, TimeUnit.SECONDS) } catch (e: Exception) { null }
    }

    @JavascriptInterface
    fun copy(text: String) {
        if (!trusted()) return
        onMain {
            val cm = activity.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
            cm.setPrimaryClip(ClipData.newPlainText("Gecko", text))
        }
    }

    @JavascriptInterface
    fun paste(): String? {
        if (!trusted()) return null
        return onMain {
            val cm = activity.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
            cm.primaryClip?.takeIf { it.itemCount > 0 }?.getItemAt(0)?.coerceToText(activity)?.toString()
        }
    }

    @JavascriptInterface
    fun notify(title: String, body: String, sessionId: String) {
        if (!trusted()) return
        main.post { activity.notifyAgent(title, body, sessionId) }
    }

    @JavascriptInterface
    fun enableNotifications() {
        if (!trusted()) return
        main.post { activity.enableNotifications() }
    }

    @JavascriptInterface
    fun mods(ctrl: Boolean, alt: Boolean) {
        if (!trusted()) return
        main.post { activity.keys.setMods(ctrl, alt) }
    }

    /**
     * "Open on another device" in the web UI. On this phone's own Gecko the
     * app shows its link and QR code (or offers to allow other devices);
     * false lets the web UI show its own dialog.
     */
    @JavascriptInterface
    fun shareDevice(): Boolean {
        if (!trusted() || !activity.onThisPhone) return false
        main.post { if (ShareUi.allowed(activity)) ShareUi.showLinks(activity) else ShareUi.turnOn(activity) }
        return true
    }

    /** Whether the page is this phone's own Gecko (not a computer's). */
    @JavascriptInterface
    fun onThisPhone(): Boolean = trusted() && activity.onThisPhone

    /** Whether this UI is the one shipped in the app (see BundledUi). */
    @JavascriptInterface
    fun bundled(): Boolean = trusted() && activity.bundled.available

    @JavascriptInterface
    fun theme(json: String) {
        if (!trusted()) return
        main.post { activity.applyTheme(json) }
    }
}
