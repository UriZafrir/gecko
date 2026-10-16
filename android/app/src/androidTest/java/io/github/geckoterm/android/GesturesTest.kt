package io.github.geckoterm.android

import android.content.Intent
import android.os.SystemClock
import android.view.InputDevice
import android.view.MotionEvent
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

    // Pixel scrolling, flings that go further when faster, the stretch past
    // the ends springing back, and the fast-scroll thumb.
    @Test fun flingOverscrollAndThumb() {
        val url = InstrumentationRegistry.getArguments().getString("geckoUrl")
        assumeTrue("no geckoUrl argument", !url.isNullOrEmpty())
        val link = LinkParser.parse(url!!)
        val m = Machine(name = "scrolling", url = link.baseUrl, token = link.token!!)
        Gecko.machines(ctx).save(m)
        val intent = Intent(ctx, TerminalActivity::class.java).putExtra(TerminalActivity.EXTRA_MACHINE, m.id)
        ActivityScenario.launch<TerminalActivity>(intent).use { sc ->
            waitFor(sc, "window.gecko&&gecko.conn.status==='online'&&gecko.firstSync===true", 60)
            js(sc, "gecko.newTab()")
            waitFor(sc, "gecko.activePane()&&gecko.activePane().term.buffer.active.getLine(0).translateToString(true).trim().length>0")
            val id = js(sc, "gecko.activeId()")
            js(sc, "gecko.sendInput($id, 'seq 1 3000\\r')")
            waitFor(sc, "gecko.panes.get($id).term.buffer.active.baseY>2900")
            Thread.sleep(1500)
            waitFor(sc, "(gecko.focusSession($id), gecko.activeId()===$id)")
            js(sc, "document.activeElement.blur()")
            Thread.sleep(500)
            val web = device.findObject(UiSelector().className("android.webkit.WebView")).bounds
            val cx = web.centerX()
            val cy = web.centerY()
            val p = "gecko.panes.get($id)"
            val y = "$p.term.buffer.active.viewportY"
            val base = js(sc, "$p.term.buffer.active.baseY").toInt()
            val settled = "!$p.el.querySelector('.xterm-screen').style.transform"

            // A slow drag: by the pixel (the drawing shifted mid-line), then
            // resting on a whole line when the finger lifts.
            js(sc, "$p.term.scrollToBottom()")
            val down = SystemClock.uptimeMillis()
            inject(MotionEvent.ACTION_DOWN, down, cx, cy - 200)
            var shifted = false
            for (i in 1..30) {
                inject(MotionEvent.ACTION_MOVE, down, cx, cy - 200 + i * 7)
                Thread.sleep(25)
                if (js(sc, "$p.el.querySelector('.xterm-screen').style.transform").contains("translateY")) shifted = true
            }
            Thread.sleep(200) // held still: no fling
            inject(MotionEvent.ACTION_UP, down, cx, cy + 10)
            assertTrue("drawn between lines while dragging", shifted)
            waitFor(sc, settled, 5)

            // The same swipe, slow and fast: the fast one goes much further.
            fun flung(steps: Int): Int {
                js(sc, "$p.term.scrollToBottom()")
                Thread.sleep(400)
                device.swipe(cx, cy - 300, cx, cy + 300, steps)
                var last = -1
                for (i in 0 until 40) { // until the fling has stopped
                    Thread.sleep(400)
                    val now = js(sc, y).toInt()
                    if (now == last) break
                    last = now
                }
                waitFor(sc, settled, 5)
                return base - js(sc, y).toInt()
            }
            val slow = flung(80)
            val fast = flung(6)
            android.util.Log.i("GesturesTest", "fling: slow $slow lines, fast $fast lines")
            val state = js(sc, "JSON.stringify([gecko.textViewOpen,gecko.paletteOpen,$y,$p.term.buffer.active.baseY,document.activeElement.className])")
            assertTrue("fast fling $fast lines, slow $slow; $state", fast > slow * 3 && slow > 0)

            // Past the top: it stretches, then springs back.
            js(sc, "$p.term.scrollToTop()")
            Thread.sleep(300)
            val t0 = SystemClock.uptimeMillis()
            inject(MotionEvent.ACTION_DOWN, t0, cx, cy - 300)
            for (i in 1..10) { inject(MotionEvent.ACTION_MOVE, t0, cx, cy - 300 + i * 50); Thread.sleep(16) }
            Thread.sleep(150)
            val stretched = js(sc, "+$p.el.dataset.stretch").toDouble()
            assertTrue("stretched $stretched", stretched > 1.01)
            inject(MotionEvent.ACTION_UP, t0, cx, cy + 200)
            Thread.sleep(1200)
            assertEquals("back at rest", "1", js(sc, "+$p.el.dataset.stretch"))
            assertEquals("still at the top", "0", js(sc, y))
            assertTrue("drawn plainly again", js(sc, settled) == "true")

            // The fast-scroll thumb: drag it to the middle, the top, the bottom.
            device.swipe(cx, cy, cx, cy - 200, 40) // scrolling shows it
            waitFor(sc, "!!$p.el.querySelector('.fastscroll.show')", 5)
            val geo = js(sc, "(()=>{const p=$p.el,r=p.getBoundingClientRect(),i=p.querySelector('.fastscroll i').getBoundingClientRect(),d=devicePixelRatio;return [r.right*d-30,i.top*d+i.height*d/2,r.top*d,r.bottom*d].join(',')})()")
                .trim('"').split(",").map { it.toDouble() }
            val tx = web.left + geo[0].toInt()
            val thumbY = web.top + geo[1].toInt()
            val top = web.top + geo[2].toInt()
            val bottom = web.top + geo[3].toInt()
            val mid = (top + bottom) / 2
            dragTo(tx, thumbY, mid)
            val m1 = js(sc, y).toInt()
            assertTrue("thumb in the middle: line $m1 of $base", m1 > base * 0.35 && m1 < base * 0.65)
            dragTo(tx, mid, top - 40)
            assertEquals("thumb at the top", "0", js(sc, y))
            dragTo(tx, top + 10, bottom + 40)
            assertEquals("thumb at the bottom", "$base", js(sc, y))
            // A tap on the strip jumps there.
            Thread.sleep(300)
            device.click(tx, top + (bottom - top) / 4)
            Thread.sleep(300)
            val q = js(sc, y).toInt()
            assertTrue("tap a quarter down: line $q of $base", q > base * 0.1 && q < base * 0.4)
        }
    }

    private fun dragTo(x: Int, fromY: Int, toY: Int) {
        val t0 = SystemClock.uptimeMillis()
        inject(MotionEvent.ACTION_DOWN, t0, x, fromY)
        for (i in 1..12) { inject(MotionEvent.ACTION_MOVE, t0, x, fromY + (toY - fromY) * i / 12); Thread.sleep(20) }
        Thread.sleep(150)
        inject(MotionEvent.ACTION_UP, t0, x, toY)
        Thread.sleep(250)
    }

    private fun inject(action: Int, downTime: Long, x: Int, y: Int) {
        val ev = MotionEvent.obtain(downTime, SystemClock.uptimeMillis(), action, x.toFloat(), y.toFloat(), 0)
        ev.source = InputDevice.SOURCE_TOUCHSCREEN
        InstrumentationRegistry.getInstrumentation().uiAutomation.injectInputEvent(ev, true)
        ev.recycle()
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
