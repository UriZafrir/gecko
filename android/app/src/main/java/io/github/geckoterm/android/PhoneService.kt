package io.github.geckoterm.android

import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import android.util.Log
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat

/**
 * Keeps Gecko's daemon running on this phone while you use other apps:
 * Android ends an app's child processes along with the app, unless it runs
 * a foreground service. Stop it from its notification or the app's menu.
 */
class PhoneService : Service() {
    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_STOP) {
            stopEverything()
            return START_NOT_STICKY
        }
        Notifier.channels(this)
        val type = if (Build.VERSION.SDK_INT >= 34) ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE else 0
        StatusNote.phone = getString(R.string.phone_starting)
        ServiceCompat.startForeground(this, StatusNote.ID, StatusNote.build(this), type)
        watchNetwork()
        Thread {
            val ok = LocalGecko.start(this)
            var shared = false
            if (ok && Gecko.prefs(this).getBoolean(ALLOW_OTHERS, false)) {
                shared = try { LocalGecko.share(this, true).on } catch (e: Exception) { Log.w(TAG, "share: ${e.message}"); false }
            }
            val text = when {
                !ok -> getString(R.string.phone_failed)
                shared -> getString(R.string.note_phone_shared)
                else -> getString(R.string.note_phone_running)
            }
            StatusNote.phone = text
            StatusNote.post(this)
            // Watching agents in the background takes in this phone's Gecko too.
            if (ok) android.os.Handler(mainLooper).post { WatchService.instance?.sync() }
            if (ok) keepTermux()
            if (!ok) stopSelf()
        }.start()
        return START_STICKY
    }

    private var netCallback: android.net.ConnectivityManager.NetworkCallback? = null
    @Volatile private var termuxKeeper: Thread? = null

    /**
     * Gecko in Termux is one of This phone's machines once it's set up:
     * hand its link to This phone's Gecko, and start it in Termux whenever
     * it doesn't answer (Termux was ended, the phone restarted), unless it
     * was stopped from the menu. Its link is checked again every minute, in
     * case it was removed from This phone's machines.
     */
    @Synchronized
    private fun keepTermux() {
        if (termuxKeeper?.isAlive == true) return
        termuxKeeper = Thread {
            var n = 0
            try {
                while (!Thread.currentThread().isInterrupted) {
                    if (PhoneTermux.setUp(this)) {
                        if (n % 12 == 0) PhoneTermux.sync(this)
                        PhoneTermux.keep(this)
                    }
                    n++
                    Thread.sleep(5_000)
                }
            } catch (e: InterruptedException) {
                // This phone stopped.
            } catch (e: Exception) {
                Log.w(TAG, "termux: ${e.message}")
            }
        }.apply { isDaemon = true; name = "termux-keeper"; start() }
    }

    /** Keeps the DNS file for ssh current as the phone changes networks. */
    private fun watchNetwork() {
        if (netCallback != null) return
        val cm = getSystemService(android.net.ConnectivityManager::class.java)
        val cb = object : android.net.ConnectivityManager.NetworkCallback() {
            override fun onLinkPropertiesChanged(network: android.net.Network, lp: android.net.LinkProperties) = LocalGecko.writeResolvConf(this@PhoneService)
            override fun onAvailable(network: android.net.Network) = LocalGecko.writeResolvConf(this@PhoneService)
        }
        try {
            cm.registerDefaultNetworkCallback(cb)
            netCallback = cb
        } catch (e: Exception) {
            Log.w(TAG, "network callback: ${e.message}")
        }
    }

    override fun onDestroy() {
        termuxKeeper?.interrupt()
        StatusNote.phone = null
        WatchService.instance?.sync()
        StatusNote.repostSoon(this) // the watching alone, if it's on
        netCallback?.let { runCatching { getSystemService(android.net.ConnectivityManager::class.java).unregisterNetworkCallback(it) } }
        super.onDestroy()
    }

    private fun stopEverything() {
        // Sharing doesn't come back by itself next time.
        Gecko.prefs(this).edit().putBoolean(ALLOW_OTHERS, false).apply()
        Thread {
            LocalGecko.stop(this)
            stopSelf()
        }.start()
    }

    companion object {
        private const val TAG = "GeckoPhone"
        const val ACTION_STOP = "stop"
        const val ALLOW_OTHERS = "allowOtherDevices"

        fun start(ctx: Context) = ContextCompat.startForegroundService(ctx, Intent(ctx, PhoneService::class.java))

        fun stop(ctx: Context) {
            ctx.startService(Intent(ctx, PhoneService::class.java).setAction(ACTION_STOP))
        }

        /** Updates the notification's text after sharing changed. */
        fun refresh(ctx: Context) = start(ctx)
    }
}
