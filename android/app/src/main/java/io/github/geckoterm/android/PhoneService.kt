package io.github.geckoterm.android

import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import android.util.Log
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
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
        ServiceCompat.startForeground(this, NOTE, notification(getString(R.string.phone_starting)), type)
        watchNetwork()
        Thread {
            val ok = LocalGecko.start(this)
            var shared = false
            if (ok && Gecko.prefs(this).getBoolean(ALLOW_OTHERS, false)) {
                shared = try { LocalGecko.share(this, true).on } catch (e: Exception) { Log.w(TAG, "share: ${e.message}"); false }
            }
            val text = when {
                !ok -> getString(R.string.phone_failed)
                shared -> getString(R.string.phone_running_shared)
                else -> getString(R.string.phone_running)
            }
            try {
                NotificationManagerCompat.from(this).notify(NOTE, notification(text))
            } catch (e: SecurityException) {
                // no notification permission: the service runs regardless
            }
            if (!ok) stopSelf()
        }.start()
        return START_STICKY
    }

    private var netCallback: android.net.ConnectivityManager.NetworkCallback? = null

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

    private fun notification(text: String): android.app.Notification {
        val open = PendingIntent.getActivity(
            this, 2,
            Intent(this, TerminalActivity::class.java).putExtra(TerminalActivity.EXTRA_MACHINE, LocalGecko.ID),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        val stop = PendingIntent.getService(this, 3, Intent(this, PhoneService::class.java).setAction(ACTION_STOP), PendingIntent.FLAG_IMMUTABLE)
        return NotificationCompat.Builder(this, Notifier.SERVICE)
            .setSmallIcon(R.drawable.ic_notify)
            .setContentTitle(getString(R.string.phone_title))
            .setContentText(text)
            .setOngoing(true)
            .setContentIntent(open)
            .addAction(0, getString(R.string.phone_stop), stop)
            .setPriority(NotificationCompat.PRIORITY_LOW)
            .setGroup("phone")
            .build()
    }

    companion object {
        private const val TAG = "GeckoPhone"
        private const val NOTE = 7
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
