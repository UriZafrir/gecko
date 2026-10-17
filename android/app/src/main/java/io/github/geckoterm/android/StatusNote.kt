package io.github.geckoterm.android

import android.app.Notification
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.os.Handler
import android.os.Looper
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat

/**
 * The one ongoing notification for Gecko's background work: watching agents
 * (WatchService) and the terminal on this phone (PhoneService). Both
 * services show it under the same id, so it is one notification that says
 * what each is doing.
 */
object StatusNote {
    const val ID = 1

    /** What WatchService watches: the machines it reaches now, of how many it tries. */
    data class Watch(val names: List<String>, val trying: Int)

    data class Text(val title: String, val text: String)

    /** PhoneService's line ("Gecko running on this phone"), null when it isn't running. */
    @Volatile var phone: String? = null

    /** WatchService's state, null when it isn't on. */
    @Volatile var watch: Watch? = null

    /** The notification's lines for these states. */
    fun text(phone: String?, watch: Watch?): Text {
        if (watch == null) return Text("Gecko on this phone", phone ?: "")
        val names = watch.names.distinct()
        val line = when {
            names.size == 1 -> "Watching agents on ${names[0]}"
            names.size > 1 -> "Watching agents on ${names.size} machines"
            watch.trying > 0 -> "Not connected, retrying"
            else -> "No machines to watch"
        }
        val second = phone ?: if (names.isEmpty() && watch.trying == 0) "Add a machine, or start Gecko on this phone." else "You'll be notified when an agent needs you."
        return Text(line, second)
    }

    fun build(ctx: Context): Notification {
        val t = text(phone, watch)
        val open = Intent(ctx, TerminalActivity::class.java)
        if (watch == null) open.putExtra(TerminalActivity.EXTRA_MACHINE, LocalGecko.ID)
        val b = NotificationCompat.Builder(ctx, Notifier.SERVICE)
            .setSmallIcon(R.drawable.ic_notify)
            .setContentTitle(t.title)
            .setContentText(t.text)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setContentIntent(PendingIntent.getActivity(ctx, 0, open, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT))
            .setPriority(NotificationCompat.PRIORITY_LOW)
        if (watch != null) {
            val stop = Intent(ctx, WatchService::class.java).setAction(WatchService.ACTION_STOP)
            b.addAction(0, ctx.getString(R.string.service_stop), PendingIntent.getService(ctx, 1, stop, PendingIntent.FLAG_IMMUTABLE))
        }
        if (phone != null) {
            val stop = Intent(ctx, PhoneService::class.java).setAction(PhoneService.ACTION_STOP)
            b.addAction(0, ctx.getString(R.string.phone_stop_long), PendingIntent.getService(ctx, 3, stop, PendingIntent.FLAG_IMMUTABLE))
        }
        return b.build()
    }

    /** Shows the current state (while either service runs). */
    fun post(ctx: Context) {
        if (phone == null && watch == null) return
        try {
            NotificationManagerCompat.from(ctx).notify(ID, build(ctx))
        } catch (e: SecurityException) {
            // no notification permission: the services run regardless
        }
    }

    /** After one service stopped: the other's notification again, with its text alone. */
    fun repostSoon(ctx: Context) {
        val app = ctx.applicationContext
        Handler(Looper.getMainLooper()).postDelayed({ post(app) }, 500)
    }
}
