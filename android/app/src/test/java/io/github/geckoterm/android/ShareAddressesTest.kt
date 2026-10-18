package io.github.geckoterm.android

import io.github.geckoterm.android.ShareAddresses.Address
import io.github.geckoterm.android.ShareAddresses.Iface
import io.github.geckoterm.android.ShareAddresses.Kind
import io.github.geckoterm.android.ShareAddresses.Transport
import org.junit.Assert.assertEquals
import org.junit.Test

class ShareAddressesTest {
    // The owner's phone on mobile data with Tailscale: the carrier's NAT
    // address (100.119.x on rmnet) looked like Tailscale and was offered.
    @Test fun mobileDataIsNeverOffered() {
        val got = ShareAddresses.pick(listOf(
            Iface("rmnet_data1", "100.119.45.155", Transport.Cellular),
            Iface("wlan0", "192.168.1.244", Transport.Wifi),
            Iface("tun0", "100.76.235.47", Transport.Vpn),
        ))
        assertEquals(listOf(Address("100.76.235.47", Kind.Tunnel), Address("192.168.1.244", Kind.Lan)), got)
    }

    @Test fun byNameWhenAndroidDoesntSay() {
        assertEquals(listOf(Address("100.76.235.47", Kind.Tunnel)),
            ShareAddresses.pick(listOf(Iface("rmnet_data0", "100.119.45.155"), Iface("ccmni0", "10.2.3.4"), Iface("tun0", "100.76.235.47"))))
        // A cellular network Android names oddly is still cellular.
        assertEquals(emptyList<Address>(), ShareAddresses.pick(listOf(Iface("weird0", "100.70.1.1", Transport.Cellular))))
    }

    @Test fun carrierNatOnWifiIsNotTailscale() {
        assertEquals(listOf(Address("192.168.5.2", Kind.Lan)),
            ShareAddresses.pick(listOf(Iface("wlan0", "100.64.12.1", Transport.Wifi), Iface("wlan0", "192.168.5.2", Transport.Wifi))))
    }

    @Test fun publicLoopbackAndLinkLocalLeftOut() {
        assertEquals(listOf(Address("10.0.2.16", Kind.Lan)),
            ShareAddresses.pick(listOf(Iface("lo", "127.0.0.1"), Iface("wlan0", "169.254.1.2"), Iface("eth0", "203.0.113.4"), Iface("wlan0", "10.0.2.16"), Iface("wlan1", "10.0.2.16"))))
    }
}
