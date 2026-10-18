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
 * connection to each saved machine, and to Gecko on this phone while it
 * runs (which reaches the machines added there), and posts a notification
 * when an agent needs input. It only reads the tab list, never terminal
 * output. Its ongoing notification (StatusNote) says what it watches.
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
        instance = this
        if (StatusNote.watch == null) StatusNote.watch = StatusNote.Watch(emptyList(), 0)
        val type = if (Build.VERSION.SDK_INT >= 34) ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE else 0
        ServiceCompat.startForeground(this, StatusNote.ID, StatusNote.build(this), type)
        sync()
        return START_STICKY
    }

    /** Follows the saved machines and this phone's Gecko: new ones join, removed ones stop. */
    fun sync() {
        // Gecko in Termux isn't in the list: it's one of This phone's machines, watched through it.
        val machines = Gecko.machines(this).list().filter { it.token.isNotEmpty() } +
            listOfNotNull(if (StatusNote.phone != null) LocalGecko.machine(this) else null)
        val want = machines.associateBy { it.id }
        for (id in watchers.keys - want.keys) watchers.remove(id)?.stop()
        for ((id, m) in want) {
            val w = watchers[id]
            val anchor = Wire.anchorOf(this, id)
            if (w == null || w.machine != m.copy(lastUsed = w.machine.lastUsed) || w.anchor != anchor) {
                w?.stop()
                watchers[id] = Watcher(m, anchor).also { it.start() }
            }
        }
        show()
    }

    /** What is watched now, in the notification. */
    private fun show() {
        val names = watchers.values.flatMap { it.names() }
        StatusNote.watch = StatusNote.Watch(names, watchers.size)
        StatusNote.post(this)
    }

    override fun onDestroy() {
        if (instance === this) instance = null
        for (w in watchers.values) w.stop()
        watchers.clear()
        main.removeCallbacksAndMessages(null)
        StatusNote.watch = null
        StatusNote.repostSoon(this) // this phone's Gecko alone, if it runs
        super.onDestroy()
    }

    /** anchor: the anchor the page chose for this machine (see [Wire.hello]). */
    private inner class Watcher(val machine: Machine, val anchor: String) {
        private var ws: WebSocket? = null
        private var stopped = false
        private var backoff = 1000L
        private val watch = AgentWatch()
        @Volatile private var open = false

        /** The machines this connection reaches now (none while it is down). */
        fun names(): List<String> {
            if (!open) return emptyList()
            val me = if (anchor.isNotEmpty()) watch.self else if (machine.id == LocalGecko.ID) getString(R.string.this_phone_lower) else machine.name
            val hosts = watch.connected.ifEmpty { listOf(watch.self) }
            return hosts.map { if (it == watch.self || it.isEmpty()) me else it }
        }

        /** The Gecko's own name, once connected. */
        val self: String get() = if (open) watch.self else ""

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
            open = false
            ws?.close(1000, null)
            ws = null
        }

        private fun retry() {
            if (open) { open = false; main.post { if (!stopped) show() } }
            if (stopped) return
            main.postDelayed({ start() }, backoff)
            backoff = (backoff * 2).coerceAtMost(60_000)
        }

        private val listener = object : WebSocketListener() {
            override fun onOpen(webSocket: WebSocket, response: Response) {
                backoff = 1000
                open = true
                main.post { if (!stopped) show() }
                webSocket.send(Wire.hello(anchor))
            }

            override fun onMessage(webSocket: WebSocket, text: String) {
                val all = watch.onMessage(text)
                val gone = watch.resolved.toList()
                val changed = watch.hostsChanged
                main.post {
                    if (stopped) return@post
                    if (changed) show()
                    // Through this phone's Gecko, tabs of a machine also watched
                    // directly are that connection's to tell about.
                    val direct = if (machine.id == LocalGecko.ID) watchers.values.filter { it !== this@Watcher }.map { it.self }.toSet() else emptySet()
                    val events = all.filter { it.host.isEmpty() || it.host !in direct }
                    for (id in gone) Notifier.cancel(this@WatchService, machine.id, id)
                    // The app in front, on this machine: the web UI shows it itself.
                    if (Gecko.foregroundMachine == machine.id) return@post
                    for (e in events) Notifier.agent(this@WatchService, machine, e.sessionId, e.title, e.body, e.choices)
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

        /** The running service, to follow this phone's Gecko starting and stopping. */
        @Volatile var instance: WatchService? = null
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
