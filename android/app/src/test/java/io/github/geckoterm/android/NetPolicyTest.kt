package io.github.geckoterm.android

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.net.InetAddress

class NetPolicyTest {
    @Test fun privateAddresses() {
        for (h in listOf("localhost", "127.0.0.1", "10.0.2.2", "10.255.1.1", "172.16.0.1", "172.31.255.255", "192.168.1.20",
            "169.254.3.4", "100.64.0.1", "100.127.255.254", "::1", "[::1]", "fd7a:115c:a1e0::1", "fe80::1%wlan0", "::ffff:192.168.0.1")) {
            assertTrue(h, NetPolicy.cleartextAllowed(h))
        }
    }

    @Test fun publicAddresses() {
        for (h in listOf("8.8.8.8", "172.32.0.1", "172.15.0.1", "192.169.0.1", "100.128.0.1", "100.63.0.1", "1.2.3.4", "2001:db8::1", "::ffff:8.8.8.8", "", "999.1.1.1")) {
            assertFalse(h, NetPolicy.cleartextAllowed(h))
        }
    }

    @Test fun names() {
        for (h in listOf("devbox", "devbox.local", "nas.lan", "box.home.arpa", "devbox.tail1234.ts.net", "x.internal")) assertTrue(h, NetPolicy.cleartextAllowed(h))
        for (h in listOf("example.com", "evil.ts.net.example.com", "local.example.org")) assertFalse(h, NetPolicy.cleartextAllowed(h))
    }

    @Test fun resolvedNames() {
        val priv = { _: String -> listOf(InetAddress.getByName("192.168.1.9")) }
        val mixed = { _: String -> listOf(InetAddress.getByName("192.168.1.9"), InetAddress.getByName("8.8.8.8")) }
        val none = { _: String -> emptyList<InetAddress>() }
        assertTrue(NetPolicy.cleartextAllowedResolving("gecko.example.com", priv))
        assertFalse(NetPolicy.cleartextAllowedResolving("gecko.example.com", mixed))
        assertFalse(NetPolicy.cleartextAllowedResolving("gecko.example.com", none))
        assertFalse(NetPolicy.cleartextAllowedResolving("gecko.example.com") { throw java.net.UnknownHostException(it) })
    }
}
