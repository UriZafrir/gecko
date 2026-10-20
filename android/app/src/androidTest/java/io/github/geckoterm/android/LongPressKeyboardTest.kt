package io.github.geckoterm.android

import android.content.Intent
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat
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
 * A long-press on a machine with the keyboard up opens its menu on the
 * first try, and Make anchor from it works (the owner had to close the
 * keyboard first). This phone lists ubuntu (-e ubuntuLink
 * http://10.0.2.2:PORT/?token=..): a tap on the terminal brings the
 * keyboard up, the drawer opens over it, a real long-press on ubuntu's
 * row. Screenshots in files/longpress (-e shots name); -e noAnchor skips
 * the switch itself and stops at the confirmation.
 */
@RunWith(AndroidJUnit4::class)
class LongPressKeyboardTest {
    private val ctx get() = ApplicationProvider.getApplicationContext<android.content.Context>()
    private val device get() = UiDevice.getInstance(InstrumentationRegistry.getInstrumentation())
    private val args get() = InstrumentationRegistry.getArguments()
    private val anchors get() = AnchorStore(PrefsKeyValue(Gecko.prefs(ctx)))

    private fun phone(): Intent = Intent(ctx, TerminalActivity::class.java).putExtra(TerminalActivity.EXTRA_MACHINE, LocalGecko.ID)
        .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)

    @Test fun longPressAMachineWithTheKeyboardUp() {
        val link = args.getString("ubuntuLink")
        assumeTrue("no ubuntuLink argument", !link.isNullOrEmpty())
        val tag = args.getString("shots") ?: "phone"
        val dir = File(ctx.getExternalFilesDir(null), "longpress").apply { mkdirs() }
        fun shot(n: String) {
            Thread.sleep(500)
            device.takeScreenshot(File(dir, "$tag-$n.png"))
        }
        fun log(s: String) = android.util.Log.i("LongPress", "$tag: $s")
        anchors.set(LocalGecko.ID, "[]")
        ActivityScenario.launch<TerminalActivity>(phone()).use { sc ->
            waitFor(sc, "window.gecko&&gecko.conn.status==='online'&&gecko.firstSync===true&&gecko.hostName", 90)
            val door = js(sc, "gecko.hostName").trim('"')
            js(sc, "gecko.setSetting('theme','tokyo-night')")
            js(sc, "gecko.sessions.size||gecko.newTab({name:'phone-shell'})")
            js(sc, "gecko.hosts.some(h=>h.name==='ubuntu')||gecko.conn.request({t:'addhost',kind:'link',name:'ubuntu',secret:${TerminalActivity.js(link!!)}})")
            waitFor(sc, "gecko.hosts.some(h=>h.name==='ubuntu'&&h.status==='connected')", 60)
            waitFor(sc, "gecko.activePane()&&gecko.activePane().term.element")
            js(sc, "document.body.classList.remove('drawer')")
            Thread.sleep(500)

            // A tap on the terminal: the keyboard comes up.
            val t = rect(sc, "#panes .pane:not([hidden]) .xterm-screen")
            device.click(t.first, t.second)
            waitUntil(15) { keyboardUp(sc) }
            Thread.sleep(800)
            assertTrue("the keyboard did not come up", keyboardUp(sc))
            // What happens from here on, for the log.
            js(sc, "(function(){var L=window.__lp=[],t0=performance.now(),n=function(s){L.push(Math.round(performance.now()-t0)+' '+s)};" +
                "addEventListener('resize',function(){n('resize '+innerWidth+'x'+innerHeight)});" +
                "document.addEventListener('focusout',function(e){n('focusout '+(e.target.className||e.target.tagName))},true);" +
                "document.addEventListener('contextmenu',function(e){n('contextmenu')},true);" +
                "new MutationObserver(function(){n('menu '+!!document.getElementById('ctxmenu')+' drawer '+document.body.classList.contains('drawer'))}).observe(document.body,{childList:true,attributes:true,attributeFilter:['class']});return 1})()")
            // The drawer over it (as the edge swipe opens it: the keyboard stays).
            js(sc, "gecko.toggleSidebar()")
            Thread.sleep(800)
            assertTrue("the keyboard went with the drawer", keyboardUp(sc))
            shot("1-keyboard-drawer")

            // A real long-press on ubuntu's row (scrolled into view: on a
            // small phone the keyboard leaves the drawer little room).
            js(sc, "document.querySelector('#side .host[data-host=\"ubuntu\"]').scrollIntoView({block:'center'})")
            Thread.sleep(400)
            val r = rect(sc, "#side .host[data-host=\"ubuntu\"] .host-main")
            device.swipe(r.first, r.second, r.first, r.second, 150)
            Thread.sleep(1500)
            val menu = js(sc, "!!document.getElementById('ctxmenu')") == "true"
            val drawer = js(sc, "document.body.classList.contains('drawer')") == "true"
            log("after the long-press: menu $menu, drawer $drawer, keyboard ${keyboardUp(sc)}")
            log("events: " + js(sc, "window.__lp.join(' | ')"))
            shot("2-after-longpress")
            assertTrue("no menu after a long-press with the keyboard up", menu)
            assertTrue("Make anchor is not in the menu", js(sc, "[...document.querySelectorAll('#ctxmenu button')].some(b=>b.textContent.includes('Make anchor'))") == "true")

            // Make anchor (a real tap) asks first.
            val m = rect(sc, "#ctxmenu button:nth-of-type(" + js(sc, "[...document.querySelectorAll('#ctxmenu button')].findIndex(b=>b.textContent.includes('Make anchor'))+1") + ")")
            device.click(m.first, m.second)
            waitFor(sc, "document.querySelector('#modal [data-yes]')&&document.querySelector('#modal [data-yes]').textContent==='Make anchor'")
            shot("3-confirm")
            if (args.getString("noAnchor") != null) {
                js(sc, "document.querySelector('#modal [data-no]').click()")
                return
            }
            js(sc, "document.querySelector('#modal [data-yes]').click()")
            waitFor(sc, "window.gecko&&gecko.firstSync&&gecko.hostName==='ubuntu'&&gecko.proxied&&gecko.hosts.length>0", 60)
            assertEquals("[\"ubuntu\"]", anchors.get(LocalGecko.ID))
            js(sc, "gecko.sessions.size||gecko.newTab({name:'on-ubuntu'})")
            waitFor(sc, "document.querySelector('#statusline .sl-anchor')&&document.querySelector('#statusline .sl-anchor').textContent==='via ubuntu'")
            shot("4-ubuntu-anchor")
            // Back to the phone, for the next run.
            js(sc, "gecko.makeAnchor(${TerminalActivity.js(door)})")
            Thread.sleep(1500)
            js(sc, "document.querySelector('#modal [data-yes]')&&document.querySelector('#modal [data-yes]').click()")
        }
        anchors.set(LocalGecko.ID, "[]")
    }

    private fun keyboardUp(sc: ActivityScenario<TerminalActivity>): Boolean {
        var up = false
        val latch = CountDownLatch(1)
        sc.onActivity { a -> up = ViewCompat.getRootWindowInsets(a.window.decorView)?.isVisible(WindowInsetsCompat.Type.ime()) == true; latch.countDown() }
        latch.await(5, TimeUnit.SECONDS)
        return up
    }

    private fun waitUntil(seconds: Int, ok: () -> Boolean) {
        val until = System.currentTimeMillis() + seconds * 1000L
        while (System.currentTimeMillis() < until && !ok()) Thread.sleep(200)
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
