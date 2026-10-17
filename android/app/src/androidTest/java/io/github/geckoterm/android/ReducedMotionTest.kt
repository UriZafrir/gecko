package io.github.geckoterm.android

import android.content.Intent
import android.os.SystemClock
import android.view.InputDevice
import android.view.MotionEvent
import androidx.test.core.app.ActivityScenario
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.uiautomator.UiDevice
import androidx.test.uiautomator.UiSelector
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import java.io.File
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/**
 * With animations removed (Settings > Accessibility > Remove animations,
 * animator scale 0) the page reports reduced motion, and still: a fast
 * flick coasts (also when the phone repeats the last position just before
 * the lift), and a working agent's logo still fades. Needs geckoUrl
 * and, for the agent, a stand-in for Claude Code (-e fakeClaude <path>, as
 * the VM's run/fakebin/claude); with
 * -e shot true it saves files/reduced-motion.png. Settings are put back.
 * Run it on its own (-e class): the web view reads the setting when it
 * first starts in a process, so after other tests it wouldn't see it.
 */
@RunWith(AndroidJUnit4::class)
class ReducedMotionTest {
    private val ctx get() = ApplicationProvider.getApplicationContext<android.content.Context>()
    private val device get() = UiDevice.getInstance(InstrumentationRegistry.getInstrumentation())
    private var saved = listOf<String>()

    private fun sh(cmd: String): String {
        val pfd = InstrumentationRegistry.getInstrumentation().uiAutomation.executeShellCommand(cmd)
        return android.os.ParcelFileDescriptor.AutoCloseInputStream(pfd).use { it.readBytes().decodeToString().trim() }
    }

    @Before fun removeAnimations() {
        saved = listOf(sh("settings get secure accessibility_disable_animations"), sh("settings get global animator_duration_scale"))
        sh("settings put secure accessibility_disable_animations 1")
        sh("settings put global animator_duration_scale 0")
    }

    @After fun putBack() {
        if (saved[0] == "null") sh("settings delete secure accessibility_disable_animations") else sh("settings put secure accessibility_disable_animations ${saved[0]}")
        if (saved[1] == "null") sh("settings delete global animator_duration_scale") else sh("settings put global animator_duration_scale ${saved[1]}")
    }

    @Test fun flingsCoastAndWorkingShows() {
        val url = InstrumentationRegistry.getArguments().getString("geckoUrl")
        assumeTrue("no geckoUrl argument", !url.isNullOrEmpty())
        val link = LinkParser.parse(url!!)
        val m = Machine(name = "reduced", url = link.baseUrl, token = link.token!!)
        Gecko.machines(ctx).save(m)
        val intent = Intent(ctx, TerminalActivity::class.java).putExtra(TerminalActivity.EXTRA_MACHINE, m.id)
        ActivityScenario.launch<TerminalActivity>(intent).use { sc ->
            waitFor(sc, "window.gecko&&gecko.conn.status==='online'&&gecko.firstSync===true", 60)
            waitFor(sc, "matchMedia('(prefers-reduced-motion: reduce)').matches", 10)
            val before = js(sc, "gecko.activeId()")
            js(sc, "gecko.newTab()")
            waitFor(sc, "gecko.activeId()!==$before&&gecko.activePane()&&gecko.activePane().term.buffer.active.getLine(0).translateToString(true).trim().length>0")
            val id = js(sc, "gecko.activeId()")
            js(sc, "gecko.sendInput($id, 'seq 1 3000\\r')")
            waitFor(sc, "gecko.panes.get($id).term.buffer.active.baseY>2900")
            Thread.sleep(1000)
            js(sc, "document.activeElement.blur()")
            val web = device.findObject(UiSelector().className("android.webkit.WebView")).bounds
            val p = "gecko.panes.get($id)"
            val y = "$p.term.buffer.active.viewportY"
            val base = js(sc, "$p.term.buffer.active.baseY").toInt()

            // A fast flick whose last move repeats the position (as real
            // phones send) and is lifted 4 ms later: it coasts.
            js(sc, "$p.term.scrollToBottom()")
            Thread.sleep(400)
            // The lines the finger itself moved (CSS px over a row's height).
            val lifted = js(sc, "Math.round(700/devicePixelRatio/($p.el.querySelector('.xterm-screen').getBoundingClientRect().height/$p.term.rows))").toInt()
            val fingerEnd = flick(web.centerX(), web.centerY() - 300, 700)
            var last = -1
            for (i in 0 until 40) {
                Thread.sleep(400)
                val now = js(sc, y).toInt()
                if (now == last) break
                last = now
            }
            val coasted = base - last
            val diag = js(sc, "JSON.stringify(gecko.touchStats||null)")
            android.util.Log.i("ReducedMotionTest", "finger end $fingerEnd px, $lifted lines, coasted to $coasted; $diag")
            assertTrue("flung with animations off: $coasted lines (the finger $lifted); $diag", coasted > lifted * 3)
            assertEquals("reduced and flung: $diag", "true", js(sc, "gecko.touchStats.reduced&&gecko.touchStats.flung"))
            assertTrue("the report says so", js(sc, "gecko.touchReport()").contains("Reduced motion: yes"))

            // An agent at work: its logo fades in and out (opacity only), no ring.
            val fake = InstrumentationRegistry.getArguments().getString("fakeClaude")
            assumeTrue("no fakeClaude argument: skipping the agent part", !fake.isNullOrEmpty())
            js(sc, "$p.term.scrollToBottom()")
            js(sc, "gecko.sendInput($id, 'clear; $fake 60 30\\r')")
            try { waitFor(sc, "!!document.querySelector('#tabs .alogo.s-working')", 40) } catch (e: AssertionError) {
                throw AssertionError("no working logo; tabs: " + js(sc, "document.querySelector('#tabs').innerHTML.slice(0,1500)") + " sessions: " + js(sc, "JSON.stringify([...gecko.sessions.values()].map(s=>[s.id,s.agent,s.status,s.title]))"))
            }
            Thread.sleep(500)
            // (The tab bar redraws as the title changes: ask until it's there.)
            var style = "null"
            for (i in 0 until 20) {
                style = js(sc, "(()=>{const e=document.querySelector('#tabs .alogo.s-working');if(!e)return null;const s=getComputedStyle(e);return s.animationName+' '+s.outlineStyle})()")
                if (style != "null") break
                Thread.sleep(250)
            }
            assertTrue("a fading logo, no ring: $style", style.contains("logo-fade none"))
            val keys = js(sc, "[...document.styleSheets].flatMap(s=>[...s.cssRules]).filter(r=>r.name==='logo-fade').map(r=>[...r.cssRules].map(k=>[...k.style].join()).join()).join()")
            assertEquals("logo-fade changes only opacity", "\"opacity\"", keys)
            if (InstrumentationRegistry.getArguments().getString("shot") == "true") {
                js(sc, "document.body.classList.add('drawer')")
                Thread.sleep(1500)
                device.takeScreenshot(File(ctx.getExternalFilesDir(null), "reduced-motion.png"))
                js(sc, "document.body.classList.remove('drawer')")
            }
            js(sc, "gecko.sendInput($id, '\\u0003')")
        }
    }

    // Down by dist px at 120 Hz with its own times, then the same spot
    // again and the lift 4 ms after. Returns the finger's travel.
    private fun flick(x: Int, fromY: Int, dist: Int): Int {
        val t0 = SystemClock.uptimeMillis()
        at(MotionEvent.ACTION_DOWN, t0, t0, x, fromY)
        var t = t0
        var yy = fromY
        repeat(10) { yy += 3; t += 8; until(t); at(MotionEvent.ACTION_MOVE, t0, t, x, yy) }
        val start = yy
        for (i in 1..12) {
            t += 8
            until(t)
            at(MotionEvent.ACTION_MOVE, t0, t, x, start + (dist - (start - fromY)) * i * i / 144)
        }
        val end = fromY + dist
        t += 4
        at(MotionEvent.ACTION_MOVE, t0, t, x, end)
        at(MotionEvent.ACTION_UP, t0, t + 4, x, end)
        return dist
    }

    private fun until(t: Long) { while (SystemClock.uptimeMillis() < t) Thread.sleep(1) }

    private fun at(action: Int, downTime: Long, time: Long, x: Int, y: Int) {
        val ev = MotionEvent.obtain(downTime, time, action, x.toFloat(), y.toFloat(), 0)
        ev.source = InputDevice.SOURCE_TOUCHSCREEN
        InstrumentationRegistry.getInstrumentation().uiAutomation.injectInputEvent(ev, false)
        ev.recycle()
    }

    // Not through sc.onActivity: that waits for the main thread to go idle,
    // which it never does while the working logo fades.
    private var activity: TerminalActivity? = null

    private fun js(sc: ActivityScenario<TerminalActivity>, expr: String): String {
        val a = activity ?: run { var x: TerminalActivity? = null; sc.onActivity { x = it }; activity = x; x!! }
        val latch = CountDownLatch(1)
        var out = ""
        a.runOnUiThread { a.web.evaluateJavascript(expr) { out = it ?: ""; latch.countDown() } }
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
