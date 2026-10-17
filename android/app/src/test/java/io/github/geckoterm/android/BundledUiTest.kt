package io.github.geckoterm.android

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class BundledUiTest {
    @Test fun theUiFilesComeFromTheApp() {
        assertEquals("index.html", BundledUi.assetPath("/", hasToken = false))
        assertEquals("index.html", BundledUi.assetPath("", hasToken = false))
        assertEquals("index.html", BundledUi.assetPath(null, hasToken = false))
        assertEquals("main.js", BundledUi.assetPath("/main.js", hasToken = false))
        assertEquals("assets/x.woff2", BundledUi.assetPath("/assets/x.woff2", hasToken = false))
        assertEquals("themes/ramen.jpg", BundledUi.assetPath("/themes/ramen.jpg", hasToken = false))
    }

    @Test fun theMachineKeepsItsApiSocketAndTokenLink() {
        assertNull(BundledUi.assetPath("/ws", hasToken = false))
        assertNull(BundledUi.assetPath("/api/info", hasToken = false))
        assertNull(BundledUi.assetPath("/api", hasToken = false))
        assertNull(BundledUi.assetPath("/", hasToken = true)) // sets the cookie, then redirects
    }

    @Test fun nothingOutsideTheUi() {
        assertNull(BundledUi.assetPath("/../AndroidManifest.xml", hasToken = false))
        assertNull(BundledUi.assetPath("/assets/../../x", hasToken = false))
        assertNull(BundledUi.assetPath("//evil/x", hasToken = false))
        assertNull(BundledUi.assetPath("/a\\b", hasToken = false))
        assertNull(BundledUi.assetPath("/themes/", hasToken = false))
        assertNull(BundledUi.assetPath("main.js", hasToken = false))
    }

    @Test fun types() {
        assertEquals("text/javascript", BundledUi.mime("main.js"))
        assertEquals("text/html", BundledUi.mime("index.html"))
        assertEquals("font/woff2", BundledUi.mime("a/b.woff2"))
        assertEquals("image/svg+xml", BundledUi.mime("icon.svg"))
        assertEquals("application/manifest+json", BundledUi.mime("manifest.webmanifest"))
        assertEquals("image/jpeg", BundledUi.mime("t/x.JPG"))
        assertEquals("application/octet-stream", BundledUi.mime("README"))
    }
}
