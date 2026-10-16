package io.github.geckoterm.android

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ShareTest {
    @Test fun parsesGeckoShareJson() {
        val s = LocalGecko.Share.parse("""{"on":true,"listen":"0.0.0.0:7682","token":"abc123abc123abc123"}""" + "\n")
        assertTrue(s.on)
        assertEquals(7682, s.port)
        assertEquals(
            listOf("http://192.168.1.7:7682/?token=abc123abc123abc123", "http://100.101.1.2:7682/?token=abc123abc123abc123"),
            s.links(listOf("192.168.1.7", "100.101.1.2")),
        )
        // The links are what Gecko's own QR codes hold, so the app can scan them.
        val l = LinkParser.parse(s.links(listOf("192.168.1.7"))[0])
        assertEquals("http://192.168.1.7:7682/", l.baseUrl)
        assertEquals("abc123abc123abc123", l.token)
    }

    @Test fun offHasNoLinks() {
        val s = LocalGecko.Share.parse("""{"on":false}""")
        assertFalse(s.on)
        assertTrue(s.links(listOf("192.168.1.7")).isEmpty())
    }
}
