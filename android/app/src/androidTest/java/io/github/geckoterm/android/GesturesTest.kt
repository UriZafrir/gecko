package io.github.geckoterm.android

import android.content.Intent
import android.view.MotionEvent.PointerCoords
import androidx.test.core.app.ActivityScenario
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.uiautomator.By
import androidx.test.uiautomator.UiDevice
import androidx.test.uiautomator.UiSelector
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Test
import org.junit.runner.RunWith
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/**
 * Touch gestures in the web UI, with real touches (UiAutomator), against a
 * Gecko daemon (-Pandroid.testInstrumentationRunnerArguments.geckoUrl=...).
 */
@RunWith(AndroidJUnit4::class)
class GesturesTest {
    private val ctx get() = ApplicationProvider.getApplicationContext<android.content.Context>()
    private val device get() = UiDevice.getInstance(InstrumentationRegistry.getInstrumentation())

    @Test fun scrollPinchSwipeAndDrawer() {
        val url = InstrumentationRegistry.getArguments().getString("geckoUrl")
        assumeTrue("no geckoUrl argument", !url.isNullOrEmpty())
        val link = LinkParser.parse(url!!)
        val m = Machine(name = "gestures", url = link.baseUrl, token = link.token!!)
        Gecko.machines(ctx).save(m)
        val intent = Intent(ctx, TerminalActivity::class.java).putExtra(TerminalActivity.EXTRA_MACHINE, m.id)
        ActivityScenario.launch<TerminalActivity>(intent).use { sc ->
            waitFor(sc, "window.gecko&&gecko.conn.status==='online'&&gecko.firstSync===true", 60)
            // A tab with 200 lines of output, and a second tab to swipe to.
            js(sc, "gecko.newTab()")
            waitFor(sc, "gecko.activePane()&&gecko.activePane().term.buffer.active.getLine(0).translateToString(true).trim().length>0")
            val first = js(sc, "gecko.activeId()")
            js(sc, "gecko.sendInput($first, 'seq 1 200\\r')")
            waitFor(sc, "gecko.panes.get($first).term.buffer.active.baseY>100")
            js(sc, "gecko.newTab()")
            waitFor(sc, "gecko.activeId()!==$first")
            Thread.sleep(1500) // the new tab takes focus when it arrives
            waitFor(sc, "(gecko.focusSession($first), gecko.activeId()===$first)")
            js(sc, "gecko.activePane().term.scrollToBottom()")
            waitFor(sc, "gecko.activePane().term.buffer.active.viewportY>100")
            js(sc, "document.activeElement.blur()")
            Thread.sleep(800)
            val web = device.findObject(UiSelector().className("android.webkit.WebView"))
            val r = web.bounds
            val cx = r.centerX()
            val cy = r.centerY()

            // One finger down: back through the scrollback.
            val before = js(sc, "gecko.activePane().term.buffer.active.viewportY").toInt()
            device.swipe(cx, cy - 300, cx, cy + 400, 40)
            Thread.sleep(1500)
            val after = js(sc, "gecko.activePane().term.buffer.active.viewportY").toInt()
            assertTrue("scrolled from $before to $after", after < before - 10)

            // Pinch out: a bigger font; pinch in: smaller.
            val size0 = js(sc, "gecko.termFontSize()").toInt()
            device.findObject(By.clazz("android.webkit.WebView")).pinchOpen(0.6f, 800)
            waitFor(sc, "gecko.termFontSize()>$size0")
            val size1 = js(sc, "gecko.termFontSize()").toInt()
            device.findObject(By.clazz("android.webkit.WebView")).pinchClose(0.6f, 800)
            waitFor(sc, "gecko.termFontSize()<$size1")

            // Two fingers to the left: the next tab (wraps around).
            val tab0 = js(sc, "gecko.activeId()")
            twoFingerSwipe(web, r.right - 250, r.left + 150, cy)
            waitFor(sc, "gecko.activeId()!==$tab0")

            // In from the left edge: the drawer; swipe it back.
            device.swipe(r.left + 4, cy, r.left + 600, cy, 30)
            waitFor(sc, "document.body.classList.contains('drawer')")
            device.swipe(r.left + 600, cy, r.left + 60, cy, 20)
            waitFor(sc, "!document.body.classList.contains('drawer')")
            assertEquals("still on the terminal", "true", js(sc, "!!window.gecko"))
        }
    }

    private fun twoFingerSwipe(web: androidx.test.uiautomator.UiObject, fromX: Int, toX: Int, y: Int) {
        val steps = 20
        fun pts(dy: Int) = Array(steps + 1) { i ->
            PointerCoords().apply { x = (fromX + (toX - fromX) * i / steps).toFloat(); this.y = (y + dy).toFloat(); pressure = 1f; size = 1f }
        }
        web.performMultiPointerGesture(pts(-120), pts(120))
    }

    private fun js(sc: ActivityScenario<TerminalActivity>, expr: String): String {
        val latch = CountDownLatch(1)
        var out = ""
        sc.onActivity { a -> a.web.evaluateJavascript(expr) { out = it ?: ""; latch.countDown() } }
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
