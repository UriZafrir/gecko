package io.github.geckoterm.android

import android.content.Context
import android.net.Uri
import android.util.Log
import android.webkit.WebResourceResponse

/**
 * Gecko's web UI as shipped in this APK (assets/web, copied from web/dist by
 * `make android-gecko`), served in place of the machine's own copy. The page
 * stays on the machine's address, so its token cookie, API and WebSocket
 * work as before (same origin, nothing for the daemon to allow), but the
 * phone features (gestures, scrolling, the agent animations) come with the
 * app rather than with whatever Gecko that machine runs. Only the UI's own
 * files are replaced: /ws, /api/ and the ?token= link go to the machine.
 */
class BundledUi(private val ctx: Context) {
    /** The files in assets/web (paths relative to it); empty in a build without them. */
    val files: Set<String> by lazy {
        val out = HashSet<String>()
        fun walk(dir: String) {
            val names = try { ctx.assets.list(dir) } catch (e: Exception) { null } ?: return
            for (n in names) {
                val p = "$dir/$n"
                val sub = try { ctx.assets.list(p) } catch (e: Exception) { null }
                if (sub.isNullOrEmpty()) out += p.removePrefix("$DIR/") else walk(p)
            }
        }
        walk(DIR)
        out
    }

    val available: Boolean get() = "index.html" in files

    /** The bundled response for a request to the machine's origin, or null to let it through. */
    fun respond(method: String, url: Uri): WebResourceResponse? {
        if (method != "GET") return null
        val name = assetPath(url.path, url.getQueryParameter("token") != null) ?: return null
        if (name !in files) return null
        return try {
            val stream = ctx.assets.open("$DIR/$name")
            WebResourceResponse(mime(name), if (text(name)) "utf-8" else null, 200, "OK", HEADERS, stream)
        } catch (e: Exception) {
            Log.w("Gecko", "bundled $name: ${e.message}")
            null
        }
    }

    companion object {
        const val DIR = "web"

        /** The same headers the daemon sends with its files. */
        val HEADERS = mapOf(
            "Cache-Control" to "no-cache",
            "X-Content-Type-Options" to "nosniff",
            "Referrer-Policy" to "no-referrer",
            "X-Frame-Options" to "DENY",
        )

        /**
         * Which bundled file a path on the machine stands for: "/" is the
         * page, anything else its own name. Null for what must reach the
         * machine (the WebSocket, the API, the link that sets the token
         * cookie) and for anything that tries to leave assets/web.
         */
        fun assetPath(path: String?, hasToken: Boolean): String? {
            val p = path ?: "/"
            if (p == "/" || p.isEmpty()) return if (hasToken) null else "index.html"
            if (p == "/ws" || p.startsWith("/ws/") || p == "/api" || p.startsWith("/api/")) return null
            if (!p.startsWith("/") || p.contains("..") || p.contains('\\') || p.contains("//")) return null
            return p.removePrefix("/").takeIf { it.isNotEmpty() && !it.endsWith("/") }
        }

        fun mime(name: String): String = when (name.substringAfterLast('.', "").lowercase()) {
            "html" -> "text/html"
            "js", "mjs" -> "text/javascript"
            "css" -> "text/css"
            "json" -> "application/json"
            "webmanifest" -> "application/manifest+json"
            "svg" -> "image/svg+xml"
            "png" -> "image/png"
            "jpg", "jpeg" -> "image/jpeg"
            "webp" -> "image/webp"
            "gif" -> "image/gif"
            "woff2" -> "font/woff2"
            "woff" -> "font/woff"
            "ttf" -> "font/ttf"
            "mp3" -> "audio/mpeg"
            "ogg" -> "audio/ogg"
            "wav" -> "audio/wav"
            "txt" -> "text/plain"
            else -> "application/octet-stream"
        }

        private fun text(name: String) = mime(name).let { it.startsWith("text/") || it.contains("json") || it.contains("svg") }
    }
}
