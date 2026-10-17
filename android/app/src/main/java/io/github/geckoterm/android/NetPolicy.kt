package io.github.geckoterm.android

import java.net.Inet4Address
import java.net.Inet6Address
import java.net.InetAddress

/**
 * Where plain http is allowed. Gecko's link carries its access token, so it
 * may travel unencrypted only inside a private network: this device,
 * a LAN (10/8, 172.16/12, 192.168/16, link-local), a Tailscale tailnet
 * (100.64/10, fd7a:115c:a1e0::/48) or IPv6 unique local addresses.
 * Anywhere else needs https.
 *
 * Android's network security config can't express address ranges, so the
 * platform allows cleartext and the app checks every machine here before it
 * connects (see res/xml/network_security_config.xml).
 */
object NetPolicy {
    private val PRIVATE_SUFFIXES = listOf(".local", ".lan", ".home", ".home.arpa", ".internal", ".localdomain", ".ts.net", ".localhost")

    /** True when http to host is fine without looking the name up. */
    fun cleartextAllowed(host: String): Boolean {
        val h = host.lowercase().trim().removePrefix("[").removeSuffix("]").trimEnd('.')
        if (h.isEmpty()) return false
        if (h == "localhost") return true
        parseIpv4(h)?.let { return privateV4(it) }
        if (h.contains(':')) return parseIpv6(h)?.let { privateV6(it) } ?: false
        // A single-label name (a LAN host, Tailscale MagicDNS) or a private suffix.
        if (!h.contains('.')) return true
        return PRIVATE_SUFFIXES.any { h.endsWith(it) }
    }

    /**
     * For other names: allowed when every address it resolves to is private.
     * Blocking (DNS); call it off the main thread.
     */
    fun cleartextAllowedResolving(host: String, resolve: (String) -> List<InetAddress> = { InetAddress.getAllByName(it).toList() }): Boolean {
        if (cleartextAllowed(host)) return true
        val addrs = try { resolve(host) } catch (e: Exception) { return false }
        return addrs.isNotEmpty() && addrs.all { privateAddress(it) }
    }

    fun privateAddress(a: InetAddress): Boolean = when (a) {
        is Inet4Address -> privateV4(a.address)
        is Inet6Address -> privateV6(a.address)
        else -> false
    }

    private fun privateV4(b: ByteArray): Boolean {
        val x = b.map { it.toInt() and 0xff }
        return x[0] == 127 || x[0] == 10 ||
            (x[0] == 172 && x[1] in 16..31) ||
            (x[0] == 192 && x[1] == 168) ||
            (x[0] == 169 && x[1] == 254) ||
            (x[0] == 100 && x[1] in 64..127) // CGNAT: Tailscale
    }

    private fun privateV6(b: ByteArray): Boolean {
        val first = b[0].toInt() and 0xff
        val second = b[1].toInt() and 0xff
        if (b.take(15).all { it.toInt() == 0 } && b[15].toInt() == 1) return true // ::1
        if (first and 0xfe == 0xfc) return true // fc00::/7, Tailscale included
        if (first == 0xfe && second and 0xc0 == 0x80) return true // fe80::/10
        // IPv4-mapped ::ffff:a.b.c.d
        if (b.take(10).all { it.toInt() == 0 } && (b[10].toInt() and 0xff) == 0xff && (b[11].toInt() and 0xff) == 0xff) {
            return privateV4(b.copyOfRange(12, 16))
        }
        return false
    }

    fun parseIpv4(s: String): ByteArray? {
        val parts = s.split('.')
        if (parts.size != 4) return null
        val out = ByteArray(4)
        for ((i, p) in parts.withIndex()) {
            if (p.isEmpty() || p.length > 3 || !p.all { it.isDigit() }) return null
            val n = p.toInt()
            if (n > 255) return null
            out[i] = n.toByte()
        }
        return out
    }

    /** IPv6 literal to 16 bytes, without DNS; null if it isn't one. */
    fun parseIpv6(s0: String): ByteArray? {
        val s = s0.substringBefore('%') // zone id
        if (!s.all { it.isDigit() || it in 'a'..'f' || it in 'A'..'F' || it == ':' || it == '.' }) return null
        return try {
            val a = InetAddress.getByName(s) // a literal never goes to DNS
            when (a) {
                is Inet6Address -> a.address
                is Inet4Address -> ByteArray(10) + byteArrayOf(-1, -1) + a.address
                else -> null
            }
        } catch (e: Exception) {
            null
        }
    }
}
