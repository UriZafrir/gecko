package io.github.geckoterm.android

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

class LinkParserTest {
    private val tok = "4dd99c1f25e82e4c078bc059dcab8d7f262727a88ea7ae6e"

    @Test fun qrCodeLink() {
        // What openShare() puts in the QR code (server.ShareURLs).
        val l = LinkParser.parse("http://192.168.1.20:7681/?token=$tok")
        assertEquals("http://192.168.1.20:7681/", l.baseUrl)
        assertEquals("192.168.1.20", l.host)
        assertEquals(tok, l.token)
        assertFalse(l.https)
    }

    @Test fun geckoUrlOutput() {
        // `gecko url` prints the local address first, then the others.
        val l = LinkParser.parse("http://100.101.102.103:7681/?token=$tok  (other devices)\n")
        assertEquals("http://100.101.102.103:7681/", l.baseUrl)
        assertEquals(tok, l.token)
    }

    @Test fun linkInsideText() {
        val l = LinkParser.parse("Open this on your phone: http://10.0.0.5:7681/?token=$tok.")
        assertEquals("http://10.0.0.5:7681/", l.baseUrl)
        assertEquals(tok, l.token)
    }

    @Test fun tailscaleHttps() {
        val l = LinkParser.parse("https://devbox.tail1234.ts.net/?token=$tok")
        assertEquals("https://devbox.tail1234.ts.net/", l.baseUrl)
        assertTrue(l.https)
    }

    @Test fun defaultPortsAndPathDropped() {
        assertEquals("https://box.example.com/", LinkParser.parse("https://box.example.com:443/some/path?token=$tok").baseUrl)
        assertEquals("http://box.lan/", LinkParser.parse("http://box.lan:80/?token=$tok").baseUrl)
    }

    @Test fun bareHostGetsHttp() {
        val l = LinkParser.parse("192.168.1.20:7681/?token=$tok")
        assertEquals("http://192.168.1.20:7681/", l.baseUrl)
        assertEquals(tok, l.token)
        assertNull(LinkParser.parse("192.168.1.20:7681").token)
    }

    @Test fun ipv6() {
        val l = LinkParser.parse("http://[fd7a:115c:a1e0::1]:7681/?token=$tok")
        assertEquals("http://[fd7a:115c:a1e0::1]:7681/", l.baseUrl)
        assertEquals("fd7a:115c:a1e0::1", l.host)
    }

    @Test fun tokenAmongOtherParams() {
        assertEquals(tok, LinkParser.parse("http://h:1/?fit=80x24&token=$tok&x=1").token)
        assertEquals("abcd+efg/hij=", LinkParser.parse("http://h:1/?token=abcd%2Befg%2Fhij%3D").token)
    }

    @Test fun rejects() {
        for (bad in listOf("", "   ", "ftp://host/?token=$tok", "javascript:alert(1)", "file:///etc/passwd", "http://user:pw@host/?token=$tok", "http://h/?token=<script>")) {
            try {
                LinkParser.parse(bad)
                fail("accepted $bad")
            } catch (e: LinkException) {
                assertTrue(e.message!!.isNotEmpty())
            }
        }
    }

    @Test fun helpers() {
        assertEquals("http://h:7681/?token=$tok", LinkParser.withToken("http://h:7681/", tok))
        assertEquals("ws://h:7681/ws", LinkParser.wsUrl("http://h:7681/"))
        assertEquals("wss://h.ts.net/ws", LinkParser.wsUrl("https://h.ts.net/"))
        assertEquals("http://h:7681", LinkParser.origin("http://H:7681/x?y"))
        assertEquals("https://h", LinkParser.origin("https://h:443/"))
        assertNull(LinkParser.origin("about:blank"))
    }
}
