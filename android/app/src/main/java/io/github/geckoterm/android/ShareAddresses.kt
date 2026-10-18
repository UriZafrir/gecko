package io.github.geckoterm.android

/**
 * Which of this phone's addresses go in the links for other devices
 * ("Allow other devices"). Tunnels first (Tailscale reaches your devices
 * anywhere), then Wi-Fi and Ethernet. Never mobile data: carriers hand out
 * addresses from 100.64.0.0/10 (carrier-grade NAT), the same range as
 * Tailscale, and no device of yours can reach them. Same rules as Gecko's
 * own (internal/server/addrs.go).
 */
object ShareAddresses {
    enum class Kind { Tunnel, Lan }

    /** How Android sees an interface's network (ConnectivityManager), when it knows. */
    enum class Transport { Vpn, Cellular, Wifi, Ethernet, Other }

    data class Iface(val name: String, val ip: String, val transport: Transport? = null)
    data class Address(val ip: String, val kind: Kind)

    private val CELLULAR = listOf("rmnet", "v4-rmnet", "ccmni", "wwan", "pdp", "clat", "seth_", "rev_rmnet", "usb_rmnet", "qmimux", "mhi_hwip")
    private val TUNNEL = listOf("tailscale", "tun", "utun", "wg")

    fun cellular(i: Iface) = i.transport == Transport.Cellular || CELLULAR.any { i.name.lowercase().startsWith(it) }
    fun tunnel(i: Iface) = i.transport == Transport.Vpn || TUNNEL.any { i.name.lowercase().startsWith(it) }

    private fun v4(ip: String): IntArray? {
        val p = ip.split('.')
        if (p.size != 4) return null
        return IntArray(4) { p[it].toIntOrNull()?.takeIf { n -> n in 0..255 } ?: return null }
    }

    fun pick(ifaces: List<Iface>): List<Address> {
        val tun = ArrayList<Address>()
        val lan = ArrayList<Address>()
        val seen = HashSet<String>()
        for (i in ifaces) {
            val x = v4(i.ip) ?: continue
            if (x[0] == 127 || (x[0] == 169 && x[1] == 254) || cellular(i) || !seen.add(i.ip)) continue
            val cgnat = x[0] == 100 && x[1] in 64..127
            val private = x[0] == 10 || (x[0] == 172 && x[1] in 16..31) || (x[0] == 192 && x[1] == 168)
            when {
                (cgnat || private) && tunnel(i) -> tun += Address(i.ip, Kind.Tunnel)
                private -> lan += Address(i.ip, Kind.Lan)
            }
        }
        return tun + lan
    }
}
