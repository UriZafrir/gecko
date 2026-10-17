package io.github.geckoterm.android

import android.Manifest
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.content.ContextCompat

/** Notifications for agents that need you, and the background connection's own. */
object Notifier {
    const val AGENTS = "agents"
    const val SERVICE = "service"
    private data class Posted(val title: String, val at: Long, val answers: Boolean)
    private val recent = HashMap<String, Posted>()

    fun channels(ctx: Context) {
        if (Build.VERSION.SDK_INT < 26) return
        val nm = ctx.getSystemService(NotificationManager::class.java)
        nm.createNotificationChannel(NotificationChannel(AGENTS, ctx.getString(R.string.channel_agents), NotificationManager.IMPORTANCE_HIGH).apply {
            description = ctx.getString(R.string.channel_agents_desc)
        })
        nm.createNotificationChannel(NotificationChannel(SERVICE, ctx.getString(R.string.channel_service), NotificationManager.IMPORTANCE_MIN).apply {
            description = ctx.getString(R.string.channel_service_desc)
            setShowBadge(false)
        })
    }

    fun allowed(ctx: Context): Boolean =
        (Build.VERSION.SDK_INT < 33 || ContextCompat.checkSelfPermission(ctx, Manifest.permission.POST_NOTIFICATIONS) == PackageManager.PERMISSION_GRANTED) &&
            NotificationManagerCompat.from(ctx).areNotificationsEnabled()

    private fun key(machineId: String, sessionId: String) = "$machineId/$sessionId"
    private fun id(machineId: String, sessionId: String) = key(machineId, sessionId).hashCode()

    /** Posts (or replaces) the notification for one tab. The web view and the background connection can both report the same moment; once is enough. */
    fun agent(ctx: Context, machine: Machine, sessionId: String, title: String, body: String, choices: List<Pair<String, String>> = emptyList()) {
        if (!allowed(ctx)) return
        val k = key(machine.id, sessionId)
        val now = System.currentTimeMillis()
        synchronized(recent) {
            val last = recent[k]
            // The same moment again, unless this time it comes with answer buttons.
            if (last != null && last.title == title && now - last.at < 20_000 && (last.answers || choices.isEmpty())) return
            recent[k] = Posted(title, now, choices.isNotEmpty())
        }
        val open = Intent(ctx, TerminalActivity::class.java)
            .setAction("open:$k")
            .putExtra(TerminalActivity.EXTRA_MACHINE, machine.id)
            .putExtra(TerminalActivity.EXTRA_SESSION, sessionId)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP)
        val pi = PendingIntent.getActivity(ctx, k.hashCode(), open, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
        val n = NotificationCompat.Builder(ctx, AGENTS)
            .setSmallIcon(R.drawable.ic_notify)
            .setContentTitle(title)
            .setContentText(body.ifEmpty { null })
            .setStyle(NotificationCompat.BigTextStyle().bigText(body))
            .setSubText(machine.name)
            .setColor(0xFF7CCB3A.toInt())
            .setCategory(NotificationCompat.CATEGORY_MESSAGE)
            .setPriority(NotificationCompat.PRIORITY_HIGH)
            .setAutoCancel(true)
            .setContentIntent(pi)
            .setGroup(AGENTS)
        // Answer from the notification: up to three of the choices on screen.
        for ((i, c) in choices.take(3).withIndex()) {
            val answer = Intent(ctx, AnswerReceiver::class.java)
                .setAction("answer:$k:${c.first}")
                .putExtra(TerminalActivity.EXTRA_MACHINE, machine.id)
                .putExtra(TerminalActivity.EXTRA_SESSION, sessionId)
                .putExtra(AnswerReceiver.EXTRA_KEY, c.first)
            val api = PendingIntent.getBroadcast(ctx, (k + i).hashCode(), answer, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
            n.addAction(0, c.second.take(24), api)
        }
        val built = n.build()
        try {
            NotificationManagerCompat.from(ctx).notify(id(machine.id, sessionId), built)
        } catch (e: SecurityException) {
            // Permission taken away meanwhile.
        }
    }

    fun cancel(ctx: Context, machineId: String, sessionId: String) {
        synchronized(recent) { recent.remove(key(machineId, sessionId)) }
        NotificationManagerCompat.from(ctx).cancel(id(machineId, sessionId))
    }
}
