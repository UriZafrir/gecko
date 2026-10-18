package io.github.geckoterm.android

import android.content.Intent
import androidx.test.core.app.ActivityScenario
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.uiautomator.UiDevice
import androidx.test.uiautomator.UiSelector
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Test
import org.junit.runner.RunWith
import java.io.File
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/**
 * Make anchor from This phone (docs/anchor.md): This phone lists ubuntu (a
 * throwaway Gecko on the host, -e ubuntuLink http://10.0.2.2:PORT/?token=..),
 * the anchor icon is on the phone, a long-press on ubuntu and Make anchor
 * moves it there with "via ubuntu", the app reopens on ubuntu, and Back to
 * the phone switches back. Screenshots in files/anchor (-e shots name).
 */
@RunWith(AndroidJUnit4::class)
class AnchorSwitchTest {
    private val ctx get() = ApplicationProvider.getApplicationContext<android.content.Context>()
    private val device get() = UiDevice.getInstance(InstrumentationRegistry.getInstrumentation())
    private val args get() = InstrumentationRegistry.getArguments()
    private val anchors get() = AnchorStore(PrefsKeyValue(Gecko.prefs(ctx)))

    private fun phone(): Intent = Intent(ctx, TerminalActivity::class.java).putExtra(TerminalActivity.EXTRA_MACHINE, LocalGecko.ID)
        .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)

    @Test fun makeAnchorFromThisPhoneAndBack() {
        val link = args.getString("ubuntuLink")
        assumeTrue("no ubuntuLink argument", !link.isNullOrEmpty())
        val tag = args.getString("shots") ?: "phone"
        val dir = File(ctx.getExternalFilesDir(null), "anchor").apply { mkdirs() }
        fun shot(n: String) {
            Thread.sleep(700)
            device.takeScreenshot(File(dir, "$tag-$n.png"))
        }
        anchors.set(LocalGecko.ID, "[]")
        var door = ""
        ActivityScenario.launch<TerminalActivity>(phone()).use { sc ->
            waitFor(sc, "window.gecko&&gecko.conn.status==='online'&&gecko.firstSync===true&&gecko.hostName", 90)
            door = js(sc, "gecko.hostName").trim('"')
            js(sc, "gecko.setSetting('theme','tokyo-night')")
            js(sc, "gecko.sessions.size||gecko.newTab({name:'phone-shell'})")
            // ubuntu becomes one of This phone's machines (Paste a Gecko link).
            js(sc, "gecko.hosts.some(h=>h.name==='ubuntu')||gecko.conn.request({t:'addhost',kind:'link',name:'ubuntu',secret:${TerminalActivity.js(link!!)}})")
            waitFor(sc, "gecko.hosts.some(h=>h.name==='ubuntu'&&h.status==='connected')", 60)
            openDrawer(sc)
            // The anchor is the phone, marked; no "via" on the device itself.
            assertEquals("true", js(sc, "!!document.querySelector('#side .host.anchor[data-host=\"$door\"] .anchor-badge .anc')"))
            assertEquals("0", js(sc, "document.querySelectorAll('#statusline .sl-anchor').length"))
            shot("1-this-phone")

            // A long-press on ubuntu's row: its menu, with Make anchor.
            val r = rect(sc, "#side .host[data-host=\"ubuntu\"] .host-main")
            device.swipe(r.first, r.second, r.first, r.second, 120)
            Thread.sleep(800)
            if (js(sc, "!!document.getElementById('ctxmenu')") != "true") {
                // Older WebViews: the same menu, as right-click opens it.
                js(sc, "document.querySelector('#side .host[data-host=\"ubuntu\"] .host-main').dispatchEvent(new MouseEvent('contextmenu',{bubbles:true,clientX:60,clientY:200}))")
            }
            waitFor(sc, "[...document.querySelectorAll('#ctxmenu button')].some(b=>b.textContent.includes('Make anchor'))")
            shot("2-menu")
            js(sc, "[...document.querySelectorAll('#ctxmenu button')].find(b=>b.textContent.includes('Make anchor')).click()")
            waitFor(sc, "document.querySelector('#modal [data-yes]')&&document.querySelector('#modal [data-yes]').textContent==='Make anchor'")
            shot("3-confirm")
            js(sc, "document.querySelector('#modal [data-yes]').click()")
            waitFor(sc, "window.gecko&&gecko.firstSync&&gecko.hostName==='ubuntu'&&gecko.proxied&&gecko.hosts.length>0", 60)
            assertEquals("[\"ubuntu\"]", anchors.get(LocalGecko.ID))
            js(sc, "gecko.sessions.size||gecko.newTab({name:'on-ubuntu'})")
            waitFor(sc, "document.querySelector('#statusline .sl-anchor')&&document.querySelector('#statusline .sl-anchor').textContent==='via ubuntu'")
            openDrawer(sc)
            assertEquals("true", js(sc, "!!document.querySelector('#side .host.anchor[data-host=\"ubuntu\"] .anchor-badge .anc')"))
            assertEquals("true", js(sc, "!!document.querySelector('#side .prev-anchor[data-prev=\"$door\"]')"))
            shot("4-ubuntu-anchor")
        }

        // Next launch: the app reopens on ubuntu.
        ActivityScenario.launch<TerminalActivity>(phone()).use { sc ->
            waitFor(sc, "window.gecko&&gecko.firstSync&&gecko.hostName==='ubuntu'&&gecko.proxied", 90)
            openDrawer(sc)
            shot("5-relaunched")
            // Back to the phone.
            val r = rect(sc, "#side .prev-anchor[data-prev=\"$door\"] .host-main")
            device.click(r.first, r.second)
            waitFor(sc, "window.gecko&&gecko.firstSync&&gecko.hostName===${TerminalActivity.js(door)}&&!gecko.proxied&&gecko.hosts.length>0", 60)
            assertEquals("", anchors.get(LocalGecko.ID))
            waitFor(sc, "document.querySelectorAll('#statusline .sl-anchor').length===0")
            openDrawer(sc)
            assertEquals("true", js(sc, "!!document.querySelector('#side .host.anchor[data-host=\"$door\"]')"))
            assertTrue(js(sc, "gecko.hosts.map(h=>h.name).join()").contains("ubuntu"))
            shot("6-back-on-phone")
            if (args.getString("keepUbuntu") == null) js(sc, "gecko.conn.request({t:'rmhost',name:'ubuntu'})")
        }
    }

    private fun openDrawer(sc: ActivityScenario<TerminalActivity>) {
        js(sc, "document.activeElement&&document.activeElement.blur()")
        if (js(sc, "getComputedStyle(document.getElementById('side')).position==='fixed'&&!document.body.classList.contains('drawer')") == "true") {
            val r = rect(sc, "#menu-btn")
            device.click(r.first, r.second)
            Thread.sleep(600)
        }
    }

    /** The center of an element on the screen, in pixels. */
    private fun rect(sc: ActivityScenario<TerminalActivity>, sel: String): Pair<Int, Int> {
        val s = js(sc, "(function(){var r=document.querySelector(${TerminalActivity.js(sel)}).getBoundingClientRect();return [r.left+r.width/2,r.top+r.height/2,devicePixelRatio].join(',')})()").trim('"')
        val (x, y, dpr) = s.split(',').map { it.toDouble() }
        val web = device.findObject(UiSelector().className("android.webkit.WebView")).bounds
        return Pair(web.left + (x * dpr).toInt(), web.top + (y * dpr).toInt())
    }

    private fun js(sc: ActivityScenario<TerminalActivity>, expr: String): String {
        val latch = CountDownLatch(1)
        var out = ""
        sc.onActivity { a -> a.web.evaluateJavascript("(function(){return ($expr)})()") { out = it ?: ""; latch.countDown() } }
        latch.await(10, TimeUnit.SECONDS)
        return out
    }

    private fun waitFor(sc: ActivityScenario<TerminalActivity>, cond: String, seconds: Int = 20) {
        val until = System.currentTimeMillis() + seconds * 1000L
        while (System.currentTimeMillis() < until) {
            if (js(sc, "!!($cond)") == "true") return
            Thread.sleep(250)
        }
        throw AssertionError("timed out waiting for $cond")
    }
}
