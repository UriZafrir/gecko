package io.github.geckoterm.android

import android.content.Context
import android.util.Log
import java.util.concurrent.atomic.AtomicReference

/**
 * Runs [TermuxMachine]'s decisions on the phone: hands Termux's link to
 * This phone's Gecko (its CLI, `gecko host add termux --url -`, with the
 * link on stdin so the token never shows in a process list), and keeps
 * Gecko in Termux running while This phone runs ([PhoneService] calls
 * [keep] every few seconds).
 */
object PhoneTermux {
    private const val TAG = "GeckoTermux"
    private const val PAUSED = "termuxPaused"
    private const val HANDED = "termuxHanded"

    fun saved(ctx: Context): Machine? = Termux.machine(ctx)?.takeIf { it.token.isNotEmpty() }
    fun setUp(ctx: Context) = saved(ctx) != null

    fun paused(ctx: Context) = Gecko.prefs(ctx).getBoolean(PAUSED, false)
    fun setPaused(ctx: Context, on: Boolean) = Gecko.prefs(ctx).edit().putBoolean(PAUSED, on).apply()

    /** For the connect screen. Blocking (looks at Termux's port). */
    fun status(ctx: Context): TermuxMachine.Status {
        val m = saved(ctx)
        return TermuxMachine.status(Termux.installed(ctx), m != null, paused(ctx), m != null && Termux.listening(m))
    }

    /**
     * Brings This phone's Gecko up to date: adds Termux (or its new link),
     * or forgets Termux here when it was removed there. Blocking; This
     * phone's Gecko must be running. Returns an error to show, or null.
     */
    @Synchronized
    fun sync(ctx: Context): String? {
        val m = saved(ctx)
        val handed = Gecko.prefs(ctx).getString(HANDED, null)
        val listed = if (m == null) null else LocalGecko.hosts(ctx)?.contains(TermuxMachine.HOST)
        return when (val step = TermuxMachine.next(m, handed, listed)) {
            TermuxMachine.Step.None -> null
            is TermuxMachine.Step.Add -> {
                val err = LocalGecko.addLinkHost(ctx, TermuxMachine.HOST, step.link)
                if (err == null) {
                    Gecko.prefs(ctx).edit().putString(HANDED, TermuxMachine.fingerprint(m!!)).apply()
                    Log.i(TAG, "Termux is a machine of This phone now")
                } else Log.w(TAG, "adding Termux to This phone: $err")
                err
            }
            TermuxMachine.Step.Forget -> {
                Log.i(TAG, "Termux was removed from This phone's machines; forgetting it")
                forget(ctx, fromPhone = false)
                null
            }
        }
    }

    /** Forgets Gecko in Termux: its link here and (fromPhone) the machine in This phone's Gecko. Blocking. */
    fun forget(ctx: Context, fromPhone: Boolean) {
        Gecko.termux(ctx).remove(Termux.ID)
        Gecko.prefs(ctx).edit().remove(HANDED).remove(PAUSED).apply()
        if (fromPhone && LocalGecko.running(ctx)) LocalGecko.removeHost(ctx, TermuxMachine.HOST)
    }

    @Volatile private var lastTry = 0L
    @Volatile private var failures = 0

    /**
     * Starts Gecko in Termux when it should run and doesn't (Termux was
     * ended, the phone restarted, This phone just started). Blocking.
     */
    fun keep(ctx: Context) {
        val m = saved(ctx) ?: return
        val listening = Termux.listening(m)
        if (listening) failures = 0
        val now = System.currentTimeMillis()
        if (!TermuxMachine.shouldStart(true, paused(ctx), Termux.installed(ctx) && Termux.permitted(ctx), listening, now, lastTry, failures)) return
        lastTry = now
        if (start(ctx)) failures = 0 else failures++
    }

    /**
     * Starts `gecko daemon` in Termux in the background (or finds it
     * running), then asks it for its link again: a new token is handed
     * over. Blocking. True once it answers.
     */
    fun start(ctx: Context): Boolean {
        val port = TermuxFlow.PORT
        val up = Termux.portOpen("127.0.0.1", port) || run {
            val exited = AtomicReference<TermuxResult?>()
            try {
                Termux.spawn(ctx, TermuxFlow.START, "gecko daemon") { exited.set(it) }
            } catch (e: Exception) {
                Log.w(TAG, "starting Gecko in Termux: ${e.message}")
                return false
            }
            val until = System.currentTimeMillis() + 15_000
            while (System.currentTimeMillis() < until) {
                if (Termux.portOpen("127.0.0.1", port)) return@run true
                val ex = exited.get()
                if (ex != null && !TermuxFlow.alreadyRunning(ex)) return@run false
                Thread.sleep(250)
            }
            Termux.portOpen("127.0.0.1", port)
        }
        if (!up) {
            Log.w(TAG, "Gecko in Termux didn't start")
            return false
        }
        val link = try { Termux.run(ctx, TermuxFlow.URL, "gecko url", 15_000) } catch (e: Exception) { null }
            ?.takeIf { it.ran && it.exitCode == 0 }?.let { TermuxFlow.localLink(it.stdout) }
        val m = saved(ctx)
        if (link != null && (m == null || m.url != link.baseUrl || m.token != link.token)) {
            Termux.save(ctx, link)
            if (LocalGecko.running(ctx)) sync(ctx)
        }
        return true
    }
}
