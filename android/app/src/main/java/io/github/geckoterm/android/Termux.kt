package io.github.geckoterm.android

import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import android.util.Log
import androidx.core.content.ContextCompat
import java.net.InetSocketAddress
import java.net.Socket
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger

/**
 * Termux (https://termux.dev), a Linux userland with its own packages, as
 * a place to run Gecko: the app starts `gecko daemon` there with Termux's
 * RUN_COMMAND intent (the user grants the permission and sets
 * allow-external-apps in Termux), asks it for its link (`gecko url`, whose
 * output comes back in a result PendingIntent) and hands that link to This
 * phone's Gecko, which connects to it on 127.0.0.1 as one of its machines
 * (see PhoneTermux).
 */
object Termux {
    const val PACKAGE = "com.termux"
    const val PERMISSION = "com.termux.permission.RUN_COMMAND"
    const val ID = "termux"
    private const val SERVICE = "com.termux.app.RunCommandService"
    private const val ACTION = "com.termux.RUN_COMMAND"
    private const val PREFIX = "/data/data/com.termux/files/usr"
    private const val HOME = "/data/data/com.termux/files/home"
    private const val TAG = "GeckoTermux"

    const val FDROID = "https://f-droid.org/packages/com.termux/"
    const val GITHUB = "https://github.com/termux/termux-app/releases"

    fun installed(ctx: Context) = try {
        ctx.packageManager.getPackageInfo(PACKAGE, 0); true
    } catch (e: PackageManager.NameNotFoundException) {
        false
    }

    /** Termux from Google Play: a separate build, not the F-Droid or GitHub one Gecko is tested with. */
    fun fromPlay(ctx: Context): Boolean = try {
        val installer = if (Build.VERSION.SDK_INT >= 30) ctx.packageManager.getInstallSourceInfo(PACKAGE).installingPackageName
        else @Suppress("DEPRECATION") ctx.packageManager.getInstallerPackageName(PACKAGE)
        installer == "com.android.vending"
    } catch (e: Exception) {
        false
    }

    fun permitted(ctx: Context) = ContextCompat.checkSelfPermission(ctx, PERMISSION) == PackageManager.PERMISSION_GRANTED

    /** Opens Termux itself. */
    fun open(ctx: Context): Boolean {
        val i = ctx.packageManager.getLaunchIntentForPackage(PACKAGE) ?: return false
        ctx.startActivity(i.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        return true
    }

    /** Gecko in Termux's link, once it has run (kept apart from the machine list: it is a machine of This phone). */
    fun machine(ctx: Context): Machine? = Gecko.termux(ctx).get(ID)

    /** Whether something listens at the saved machine's address. Blocking. */
    fun listening(m: Machine?): Boolean {
        val link = try { LinkParser.parse(m?.url ?: return false) } catch (e: Exception) { return false }
        val port = Regex(":(\\d+)/$").find(link.baseUrl)?.groupValues?.get(1)?.toIntOrNull() ?: 80
        return portOpen(link.host, port)
    }

    fun portOpen(host: String, port: Int, timeoutMs: Int = 300) = try {
        Socket().use { it.connect(InetSocketAddress(host, port), timeoutMs); true }
    } catch (e: Exception) {
        false
    }

    /** Saves Gecko in Termux's link, to hand to This phone's Gecko (see PhoneTermux). */
    fun save(ctx: Context, link: GeckoLink): Machine {
        val store = Gecko.termux(ctx)
        val m = (store.get(ID) ?: Machine(id = ID, name = ctx.getString(R.string.termux), url = link.baseUrl, token = ""))
            .copy(url = link.baseUrl, token = link.token ?: "")
        store.save(m)
        return m
    }

    // Answers to commands, by request id, filled in by ResultReceiver.
    private val waiting = ConcurrentHashMap<Int, Pair<CountDownLatch, Array<TermuxResult?>>>()
    private val ids = AtomicInteger((System.currentTimeMillis() % 100_000).toInt() * 10)

    /**
     * Runs script with Termux's sh, in the background (no terminal session).
     * With timeoutMs > 0 it waits that long for the result: null if none
     * came. Throws SecurityException without the permission. Blocking.
     */
    fun run(ctx: Context, script: String, label: String, timeoutMs: Long = 20_000): TermuxResult? {
        val id = ids.incrementAndGet()
        val latch = CountDownLatch(1)
        val slot = arrayOfNulls<TermuxResult>(1)
        if (timeoutMs > 0) waiting[id] = latch to slot
        try {
            send(ctx, script, label, background = true, resultId = if (timeoutMs > 0) id else null)
            if (timeoutMs <= 0) return null
            latch.await(timeoutMs, TimeUnit.MILLISECONDS)
            return slot[0]
        } finally {
            waiting.remove(id)
        }
    }

    /**
     * Starts script in the background and returns at once; [onExit] gets
     * its result when it ends (never, for a daemon that keeps running).
     */
    fun spawn(ctx: Context, script: String, label: String, onExit: (TermuxResult) -> Unit) {
        val id = ids.incrementAndGet()
        exits[id] = onExit
        try {
            send(ctx, script, label, background = true, resultId = id)
        } catch (e: Exception) {
            exits.remove(id)
            throw e
        }
    }
    private val exits = ConcurrentHashMap<Int, (TermuxResult) -> Unit>()

    /** Runs script in a new Termux session the user sees, and brings Termux up. */
    fun runInSession(ctx: Context, script: String, label: String) {
        send(ctx, script, label, background = false, resultId = null)
        // Android 10 and newer don't let Termux open its window from the
        // background; this app is on screen, so it can.
        open(ctx)
    }

    private fun send(ctx: Context, script: String, label: String, background: Boolean, resultId: Int?) {
        val i = Intent(ACTION).setClassName(PACKAGE, SERVICE)
            .putExtra("com.termux.RUN_COMMAND_PATH", "$PREFIX/bin/sh")
            .putExtra("com.termux.RUN_COMMAND_ARGUMENTS", arrayOf("-c", script))
            .putExtra("com.termux.RUN_COMMAND_WORKDIR", HOME)
            .putExtra("com.termux.RUN_COMMAND_BACKGROUND", background)
            // A new session, shown at once.
            .putExtra("com.termux.RUN_COMMAND_SESSION_ACTION", "0")
            .putExtra("com.termux.RUN_COMMAND_COMMAND_LABEL", label)
            .putExtra("com.termux.RUN_COMMAND_COMMAND_DESCRIPTION", ctx.getString(R.string.termux_command_description))
        if (resultId != null) {
            val back = Intent(ctx, ResultReceiver::class.java).putExtra(EXTRA_ID, resultId)
            // Mutable: Termux adds the result to it.
            val flags = PendingIntent.FLAG_ONE_SHOT or (if (Build.VERSION.SDK_INT >= 31) PendingIntent.FLAG_MUTABLE else 0)
            i.putExtra("com.termux.RUN_COMMAND_PENDING_INTENT", PendingIntent.getBroadcast(ctx, resultId, back, flags))
        }
        // Termux's service puts itself in the foreground at once.
        if (Build.VERSION.SDK_INT >= 26) ctx.startForegroundService(i) else ctx.startService(i)
    }

    private const val EXTRA_ID = "termuxRequest"

    /** The values in Termux's result bundle. */
    fun resultOf(b: Bundle?): TermuxResult {
        if (b == null) return TermuxResult(err = 2, errmsg = "Termux sent no result")
        val m = HashMap<String, Any?>()
        for (k in listOf("stdout", "stderr", "errmsg")) m[k] = b.getString(k)
        if (b.containsKey("exitCode")) m["exitCode"] = b.getInt("exitCode")
        if (b.containsKey("err")) m["err"] = b.getInt("err")
        return TermuxResult.fromMap(m)
    }

    /** Where Termux's answers arrive. */
    class ResultReceiver : BroadcastReceiver() {
        override fun onReceive(context: Context, intent: Intent) {
            val id = intent.getIntExtra(EXTRA_ID, -1)
            val r = resultOf(intent.getBundleExtra("result"))
            Log.i(TAG, "result $id: exit ${r.exitCode} err ${r.err} ${r.errmsg.take(200)}")
            waiting[id]?.let { (latch, slot) -> slot[0] = r; latch.countDown() }
            exits.remove(id)?.invoke(r)
        }
    }
}
