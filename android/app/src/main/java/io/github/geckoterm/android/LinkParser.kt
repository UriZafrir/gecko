package io.github.geckoterm.android

import java.net.URI
import java.net.URLDecoder

/** A Gecko address: where the web UI is, and the access token if the link had one. */
data class GeckoLink(
    /** scheme://host[:port]/ with nothing after the slash. */
    val baseUrl: String,
    val host: String,
    val token: String?,
) {
    val https: Boolean get() = baseUrl.startsWith("https:")
}

class LinkException(message: String) : Exception(message)

/**
 * Reads what Gecko's "Open on another device" QR code holds, or a pasted
 * link: http://192.168.1.20:7681/?token=..., the lines `gecko url` prints
 * ("http://... (other devices)"), an https Tailscale address, or a bare
 * host:port.
 */
object LinkParser {
    private val TOKEN = Regex("^[A-Za-z0-9._~+/=-]{8,512}$")

    fun parse(input: String): GeckoLink {
        val text = input.trim()
        if (text.isEmpty()) throw LinkException("Paste or scan the link Gecko shows under Open on another device.")
        val word = Regex("""https?://\S+""", RegexOption.IGNORE_CASE).find(text)?.value
            ?: text.split(Regex("\\s+")).first()
        var raw = word.trimEnd('.', ',', ';', ')', '"', '\'', '>')
        if (!Regex("^[a-zA-Z][a-zA-Z0-9+.-]*://").containsMatchIn(raw)) {
            if (raw.contains("://")) throw LinkException("Only http and https links work.")
            raw = "http://$raw"
        }
        val uri = try {
            URI(raw)
        } catch (e: Exception) {
            throw LinkException("That doesn't look like a link.")
        }
        val scheme = uri.scheme?.lowercase()
        if (scheme != "http" && scheme != "https") throw LinkException("Only http and https links work.")
        val host = uri.host?.lowercase()?.removePrefix("[")?.removeSuffix("]")
        if (host.isNullOrEmpty()) throw LinkException("The link has no machine address in it.")
        if (uri.userInfo != null) throw LinkException("Links with a user name in them aren't Gecko links.")
        val token = queryParam(uri.rawQuery, "token")
        if (token != null && !TOKEN.matches(token)) throw LinkException("The token in that link looks damaged. Copy it again.")
        val hostPart = if (host.contains(':')) "[$host]" else host
        val port = if (uri.port == -1 || (scheme == "http" && uri.port == 80) || (scheme == "https" && uri.port == 443)) "" else ":${uri.port}"
        return GeckoLink("$scheme://$hostPart$port/", host, token)
    }

    /** The link to open in the web view: the token goes along once, as a cookie is set from it. */
    fun withToken(baseUrl: String, token: String): String =
        if (token.isEmpty()) baseUrl else baseUrl + "?token=" + java.net.URLEncoder.encode(token, "UTF-8")

    /** The WebSocket address for a base URL. */
    fun wsUrl(baseUrl: String): String =
        (if (baseUrl.startsWith("https:")) "wss:" + baseUrl.removePrefix("https:") else "ws:" + baseUrl.removePrefix("http:")) + "ws"

    /** scheme://host[:port] of a URL, as the web view reports page origins. */
    fun origin(url: String): String? = try {
        val u = URI(url)
        val scheme = u.scheme?.lowercase() ?: return null
        val host = u.host?.lowercase() ?: return null
        val port = if (u.port == -1 || (scheme == "http" && u.port == 80) || (scheme == "https" && u.port == 443)) "" else ":${u.port}"
        "$scheme://$host$port"
    } catch (e: Exception) {
        null
    }

    private fun queryParam(rawQuery: String?, name: String): String? {
        if (rawQuery.isNullOrEmpty()) return null
        for (part in rawQuery.split('&')) {
            val eq = part.indexOf('=')
            val k = if (eq < 0) part else part.substring(0, eq)
            if (k == name) {
                val v = if (eq < 0) "" else URLDecoder.decode(part.substring(eq + 1), "UTF-8")
                return v.ifEmpty { null }
            }
        }
        return null
    }
}
