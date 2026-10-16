package io.github.geckoterm.android

import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.util.Log
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import java.util.concurrent.TimeUnit

/**
 * "Notify in background": a foreground service that keeps one small
 * connection to each saved machine and posts a notification when an agent
 * there needs input. It only reads the tab list, never terminal output.
 */
class WatchService : Service() {
    private val main = Handler(Looper.getMainLooper())
    private val client = OkHttpClient.Builder()
        .pingInterval(30, TimeUnit.SECONDS)
        .readTimeout(0, TimeUnit.MILLISECONDS)
        .build()
    private val watchers = HashMap<String, Watcher>()

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_STOP) {
            Gecko.prefs(this).edit().putBoolean(Gecko.BACKGROUND, false).apply()
            stopSelf()
            return START_NOT_STICKY
        }
        Notifier.channels(this)
        val machines = Gecko.machines(this).list().filter { it.token.isNotEmpty() }
        val type = if (Build.VERSION.SDK_INT >= 34) ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE else 0
        ServiceCompat.startForeground(this, 1, status(machines.size), type)
        // Follow the saved machines: new ones join, removed ones stop.
        val want = machines.associateBy { it.id }
        for (id in watchers.keys - want.keys) watchers.remove(id)?.stop()
        for ((id, m) in want) {
            val w = watchers[id]
            if (w == null || w.machine != m.copy(lastUsed = w.machine.lastUsed)) {
                w?.stop()
                watchers[id] = Watcher(m).also { it.start() }
            }
        }
        return START_STICKY
    }

    override fun onDestroy() {
        for (w in watchers.values) w.stop()
        watchers.clear()
        main.removeCallbacksAndMessages(null)
        super.onDestroy()
    }

    private fun status(n: Int): android.app.Notification {
        val open = PendingIntent.getActivity(this, 0, Intent(this, ConnectActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
        val stop = PendingIntent.getService(this, 1, Intent(this, WatchService::class.java).setAction(ACTION_STOP), PendingIntent.FLAG_IMMUTABLE)
        return NotificationCompat.Builder(this, Notifier.SERVICE)
            .setSmallIcon(R.drawable.ic_notify)
            .setContentTitle(getString(R.string.service_title))
            .setContentText(resources.getQuantityString(R.plurals.service_text, n, n))
            .setOngoing(true)
            .setContentIntent(open)
            .addAction(0, getString(R.string.service_stop), stop)
            .setPriority(NotificationCompat.PRIORITY_MIN)
            .build()
    }

    private inner class Watcher(val machine: Machine) {
        private var ws: WebSocket? = null
        private var stopped = false
        private var backoff = 1000L
        private val watch = AgentWatch()

        fun start() {
            if (stopped) return
            val req = Request.Builder()
                .url(LinkParser.wsUrl(machine.url))
                .header("Authorization", "Bearer " + machine.token)
                .build()
            Thread {
                // Cleartext only inside a private network (DNS, so off the main thread).
                if (!machine.url.startsWith("https:") && !NetPolicy.cleartextAllowedResolving(machine.host)) {
                    Log.w(TAG, "${machine.name}: not on a private network, needs https")
                    return@Thread
                }
                main.post { if (!stopped) ws = client.newWebSocket(req, listener) }
            }.start()
        }

        fun stop() {
            stopped = true
            ws?.close(1000, null)
            ws = null
        }

        private fun retry() {
            if (stopped) return
            main.postDelayed({ start() }, backoff)
            backoff = (backoff * 2).coerceAtMost(60_000)
        }

        private val listener = object : WebSocketListener() {
            override fun onOpen(webSocket: WebSocket, response: Response) {
                backoff = 1000
                webSocket.send("""{"t":"hello","client":"android"}""")
            }

            override fun onMessage(webSocket: WebSocket, text: String) {
                val events = watch.onMessage(text)
                val gone = watch.resolved.toList()
                main.post {
                    for (id in gone) Notifier.cancel(this@WatchService, machine.id, id)
                    // The app in front, on this machine: the web UI shows it itself.
                    if (Gecko.foregroundMachine == machine.id) return@post
                    for (e in events) Notifier.agent(this@WatchService, machine, e.sessionId, e.title, e.body)
                }
            }

            override fun onClosed(webSocket: WebSocket, code: Int, reason: String) = retry()

            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                Log.i(TAG, "${machine.name}: ${t.message}")
                retry()
            }
        }
    }

    companion object {
        private const val TAG = "GeckoWatch"
        const val ACTION_STOP = "stop"

        fun start(ctx: Context) {
            Gecko.prefs(ctx).edit().putBoolean(Gecko.BACKGROUND, true).apply()
            ContextCompat.startForegroundService(ctx, Intent(ctx, WatchService::class.java))
        }

        fun stop(ctx: Context) {
            Gecko.prefs(ctx).edit().putBoolean(Gecko.BACKGROUND, false).apply()
            ctx.stopService(Intent(ctx, WatchService::class.java))
        }

        /** Picks up added or removed machines, when it's on. */
        fun refresh(ctx: Context) {
            if (Gecko.prefs(ctx).getBoolean(Gecko.BACKGROUND, false)) {
                try {
                    ContextCompat.startForegroundService(ctx, Intent(ctx, WatchService::class.java))
                } catch (e: Exception) {
                    // Not allowed from the background on Android 12+; it refreshes when the app opens.
                }
            }
        }
    }
}
