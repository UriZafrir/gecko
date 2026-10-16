package io.github.geckoterm.android

import android.content.Context
import android.os.Build
import android.util.Log
import org.json.JSONObject
import java.io.File
import java.net.InetAddress
import java.net.InetSocketAddress
import java.net.NetworkInterface
import java.net.ServerSocket
import java.net.Socket
import java.util.concurrent.TimeUnit

/**
 * Gecko's own daemon, running on this phone: the same program as on a
 * computer, built for Android and shipped in the APK as libgecko.so (native
 * libraries are the only files an app may execute). It runs as a child of
 * the app, kept alive by [PhoneService], with its files in the app's
 * private storage and /system/bin/sh as the shell.
 */
object LocalGecko {
    const val ID = "local"
    const val SHARE_PORT = 7682
    private const val TAG = "GeckoLocal"
    private const val PORT = 7681

    @Volatile private var process: Process? = null

    fun binary(ctx: Context) = File(ctx.applicationInfo.nativeLibraryDir, "libgecko.so")
    fun available(ctx: Context) = binary(ctx).canExecute()

    // Canonical paths (/data/data/..., not the /data/user/0 link), as the
    // shell reports its directory, so the prompt can shorten home to ~.
    private fun home(ctx: Context) = File(ctx.filesDir.canonicalFile, "home")
    private fun geckoHome(ctx: Context) = File(ctx.filesDir.canonicalFile, "gecko")
    private fun state(ctx: Context) = File(geckoHome(ctx), "state")
    fun log(ctx: Context) = File(state(ctx), "daemon.log")

    /** The environment shells get: Android's usual PATH, a home and a temp dir the app owns. */
    fun environment(ctx: Context): Map<String, String> {
        val tmp = File(ctx.cacheDir.canonicalFile, "tmp").apply { mkdirs() }
        return mapOf(
            "ENV" to rcFile(ctx).path,
            "HOME" to home(ctx).path,
            "TMPDIR" to tmp.path,
            "GECKO_HOME" to geckoHome(ctx).path,
            "SHELL" to "/system/bin/sh",
            "TERM" to "xterm-256color",
            "LANG" to "en_US.UTF-8",
            "PATH" to (System.getenv("PATH")?.takeIf { it.isNotEmpty() }
                ?: "/product/bin:/apex/com.android.runtime/bin:/apex/com.android.art/bin:/system_ext/bin:/system/bin:/system/xbin:/odm/bin:/vendor/bin:/vendor/xbin"),
        )
    }

    /**
     * The shell's startup file (mksh reads $ENV), written once and then the
     * user's: Android's defaults, and a prompt with the directory, ~ for home.
     */
    private fun rcFile(ctx: Context): File {
        val f = File(home(ctx), ".mkshrc")
        if (!f.exists()) {
            f.parentFile?.mkdirs()
            f.writeText(
                "# Startup file of the shell in Gecko on this phone (mksh). Change it as you like.\n" +
                    "[ -r /system/etc/mkshrc ] && . /system/etc/mkshrc\n" +
                    "HOSTNAME=\$(getprop ro.product.model)\n" +
                    "PS1='\${|\n\tlocal e=\$?\n\t(( e )) && REPLY+=\"\$e|\"\n\treturn \$e\n}\${PWD/#\$HOME/\\~} \$ '\n",
            )
        }
        return f
    }

    private fun command(ctx: Context, vararg args: String): ProcessBuilder {
        home(ctx).mkdirs()
        geckoHome(ctx).mkdirs()
        val pb = ProcessBuilder(listOf(binary(ctx).path) + args).directory(home(ctx))
        pb.environment().putAll(environment(ctx))
        return pb
    }

    /** The local web address once the daemon listens (the daemon writes it to state/web). */
    fun address(ctx: Context): String? {
        val f = File(state(ctx), "web")
        val a = try { f.readText().trim() } catch (e: Exception) { return null }
        return a.ifEmpty { null }
    }

    fun token(ctx: Context): String = try { File(geckoHome(ctx), "token").readText().trim() } catch (e: Exception) { "" }

    /** Whether the daemon answers on its web address. */
    fun running(ctx: Context): Boolean {
        val a = address(ctx) ?: return false
        val host = a.substringBeforeLast(':')
        val port = a.substringAfterLast(':').toIntOrNull() ?: return false
        return try {
            Socket().use { it.connect(InetSocketAddress(host, port), 300); true }
        } catch (e: Exception) {
            false
        }
    }

    /** Starts the daemon unless it runs already, and waits until it listens. Blocking. */
    @Synchronized
    fun start(ctx: Context): Boolean {
        if (running(ctx)) return true
        if (!available(ctx)) {
            Log.e(TAG, "no gecko binary for ${Build.SUPPORTED_ABIS.joinToString()}")
            return false
        }
        File(state(ctx), "web").delete()
        // 7681 like on a computer, so the web UI's saved state (per address)
        // stays; another port if something else has it.
        val port = if (free(PORT)) PORT else 0
        val logFile = log(ctx).apply { parentFile?.mkdirs() }
        if (logFile.length() > 1_000_000) logFile.delete()
        val p = command(ctx, "daemon", "--listen", "127.0.0.1:$port")
            .redirectErrorStream(true)
            .redirectOutput(ProcessBuilder.Redirect.appendTo(logFile))
            .start()
        process = p
        val until = System.currentTimeMillis() + 10_000
        while (System.currentTimeMillis() < until) {
            if (!p.isAlive) return false
            if (running(ctx)) return true
            Thread.sleep(100)
        }
        return running(ctx)
    }

    private fun free(port: Int) = try {
        ServerSocket(port, 1, InetAddress.getByName("127.0.0.1")).close()
        true
    } catch (e: Exception) {
        false
    }

    /** Ends the daemon and with it every shell on the phone. */
    @Synchronized
    fun stop(ctx: Context) {
        try {
            command(ctx, "stop").redirectErrorStream(true).start().waitFor(5, TimeUnit.SECONDS)
        } catch (e: Exception) {
            Log.w(TAG, "stop: ${e.message}")
        }
        process?.let { p ->
            if (!p.waitFor(3, TimeUnit.SECONDS)) p.destroyForcibly()
        }
        process = null
        File(state(ctx), "web").delete()
    }

    /** This phone as a machine for the web view; null until the daemon runs. */
    fun machine(ctx: Context): Machine? {
        val a = address(ctx) ?: return null
        val t = token(ctx).ifEmpty { return null }
        return Machine(id = ID, name = ctx.getString(R.string.this_phone), url = "http://$a/", token = t)
    }

    /** The listener for other devices (`gecko share`). Blocking. */
    fun share(ctx: Context, on: Boolean): Share {
        val args = if (on) arrayOf("share", "--json", "on", "0.0.0.0:$SHARE_PORT") else arrayOf("share", "--json", "off")
        val p = command(ctx, *args).start()
        val out = p.inputStream.bufferedReader().readText()
        val err = p.errorStream.bufferedReader().readText()
        p.waitFor(10, TimeUnit.SECONDS)
        if (p.exitValue() != 0) throw IllegalStateException(err.trim().removePrefix("gecko: ").ifEmpty { "gecko share failed" })
        return Share.parse(out)
    }

    fun shareState(ctx: Context): Share = try {
        val p = command(ctx, "share", "--json").start()
        val out = p.inputStream.bufferedReader().readText()
        p.waitFor(10, TimeUnit.SECONDS)
        if (p.exitValue() == 0) Share.parse(out) else Share(false, "", "")
    } catch (e: Exception) {
        Share(false, "", "")
    }

    data class Share(val on: Boolean, val listen: String, val token: String) {
        val port: Int get() = listen.substringAfterLast(':').toIntOrNull() ?: SHARE_PORT

        /** Links for each address other devices can reach: Wi-Fi, Ethernet, Tailscale. */
        fun links(addresses: List<String>): List<String> =
            if (!on) emptyList() else addresses.map { "http://$it:$port/?token=$token" }

        companion object {
            fun parse(json: String): Share {
                val o = JSONObject(json.trim())
                return Share(o.optBoolean("on"), o.optString("listen"), o.optString("token"))
            }
        }
    }

    /** This phone's private IPv4 addresses (Wi-Fi, Ethernet, Tailscale), Tailscale last. */
    fun lanAddresses(): List<String> {
        val out = ArrayList<Pair<String, Boolean>>()
        try {
            for (ni in NetworkInterface.getNetworkInterfaces()) {
                if (!ni.isUp || ni.isLoopback) continue
                for (a in ni.inetAddresses) {
                    val ip = a.hostAddress ?: continue
                    if (a !is java.net.Inet4Address || !NetPolicy.privateAddress(a) || ip.startsWith("127.")) continue
                    out += ip to ip.startsWith("100.")
                }
            }
        } catch (e: Exception) {
            Log.w(TAG, "interfaces: ${e.message}")
        }
        return out.sortedBy { it.second }.map { it.first }.distinct()
    }
}
