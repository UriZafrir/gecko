package io.github.geckoterm.android

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okio.ByteString.Companion.toByteString
import java.util.concurrent.TimeUnit

/** An answer button on an agent's notification: sends that key to its tab, without opening the app. */
class AnswerReceiver : BroadcastReceiver() {
    override fun onReceive(ctx: Context, intent: Intent) {
        val machine = intent.getStringExtra(TerminalActivity.EXTRA_MACHINE)?.let { Gecko.machines(ctx).get(it) } ?: return
        val session = intent.getStringExtra(TerminalActivity.EXTRA_SESSION) ?: return
        val key = intent.getStringExtra(EXTRA_KEY) ?: return
        if (machine.token.isEmpty()) return
        val done = goAsync()
        Notifier.cancel(ctx, machine.id, session)
        val client = OkHttpClient.Builder().callTimeout(8, TimeUnit.SECONDS).build()
        val req = Request.Builder().url(LinkParser.wsUrl(machine.url)).header("Authorization", "Bearer " + machine.token).build()
        Thread {
            if (!machine.url.startsWith("https:") && !NetPolicy.cleartextAllowedResolving(machine.host)) {
                done.finish()
                return@Thread
            }
            client.newWebSocket(req, object : WebSocketListener() {
                override fun onOpen(webSocket: WebSocket, response: Response) {
                    webSocket.send("""{"t":"hello","client":"android"}""")
                    webSocket.send(Wire.inputFrame(session, Wire.keyText(key)).toByteString())
                    webSocket.close(1000, null)
                }

                override fun onClosed(webSocket: WebSocket, code: Int, reason: String) = done.finish()
                override fun onClosing(webSocket: WebSocket, code: Int, reason: String) = done.finish()
                override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) = done.finish()
            })
        }.start()
    }

    companion object {
        const val EXTRA_KEY = "key"
    }
}
