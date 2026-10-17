package io.github.geckoterm.android

import android.content.Intent
import android.os.SystemClock
import android.view.InputDevice
import android.view.MotionEvent
import androidx.test.core.app.ActivityScenario
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.uiautomator.By
import androidx.test.uiautomator.UiDevice
import androidx.test.uiautomator.UiSelector
import androidx.test.uiautomator.Until
import org.junit.Assume.assumeTrue
import org.junit.Test
import org.junit.runner.RunWith
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/**
 * Not a test: the fixes from testing on a real phone, for the demo video
 * (10-real-device-fixes.mp4), on a machine running an older Gecko.
 * Runs only with -e demo real and -e geckoOldUrl <link>.
 */
@RunWith(AndroidJUnit4::class)
class RealDeviceDemo {
    private val ctx get() = ApplicationProvider.getApplicationContext<android.content.Context>()
    private val device get() = UiDevice.getInstance(InstrumentationRegistry.getInstrumentation())

    @Test fun demo() {
        val args = InstrumentationRegistry.getArguments()
        assumeTrue(args.getString("demo") == "real")
        val link = LinkParser.parse(args.getString("geckoOldUrl")!!)
        val m = Machine(name = "older Gecko", url = link.baseUrl, token = link.token!!)
        Gecko.machines(ctx).save(m)
        val intent = Intent(ctx, TerminalActivity::class.java).putExtra(TerminalActivity.EXTRA_MACHINE, m.id)
        ActivityScenario.launch<TerminalActivity>(intent).use { sc ->
            waitFor(sc, "window.gecko&&gecko.conn.status==='online'&&gecko.firstSync===true", 60)
            val before = js(sc, "gecko.activeId()")
            js(sc, "gecko.newTab()")
            waitFor(sc, "gecko.activeId()!==$before&&gecko.activePane()&&gecko.activePane().term.buffer.active.getLine(0).translateToString(true).trim().length>0")
            val id = js(sc, "gecko.activeId()")
            Thread.sleep(1500)
            waitFor(sc, "(gecko.focusSession($id), gecko.activeId()===$id)")
            js(sc, "gecko.sendInput($id, 'clear; seq -f \"line %g of 3000\" 1 3000\\r')")
            waitFor(sc, "gecko.panes.get($id).term.buffer.active.baseY>2900")
            js(sc, "document.activeElement.blur()")
            js(sc, GestureDemo.TOUCH_DOTS)
            Thread.sleep(2500) // the toast: an older Gecko there, tabs work
            val r = device.findObject(UiSelector().className("android.webkit.WebView")).bounds
            val cx = r.centerX()
            val cy = r.centerY()

            // Flicks the way a finger makes them: a slow one, then a fast one.
            fingerFlick(cx, cy - 250, 380, 160)
            Thread.sleep(3000)
            fingerFlick(cx, cy - 300, 650, 70)
            Thread.sleep(4500)

            // The thumb: to the middle, then the bottom.
            device.swipe(cx, cy, cx, cy - 60, 30)
            Thread.sleep(300)
            val geo = js(sc, "(()=>{const p=gecko.panes.get($id).el,r=p.getBoundingClientRect(),i=p.querySelector('.fastscroll i').getBoundingClientRect(),d=devicePixelRatio;return [r.right*d-30,i.top*d+i.height*d/2,r.top*d,r.bottom*d].join(',')})()")
                .trim('"').split(",").map { it.toDouble() }
            val tx = r.left + geo[0].toInt()
            val top = r.top + geo[2].toInt()
            val bottom = r.top + geo[3].toInt()
            drag(tx, r.top + geo[1].toInt(), (top + bottom) / 2, 1200, hold = 600)
            Thread.sleep(1200)
            drag(tx, (top + bottom) / 2, bottom - 20, 1000, hold = 600)
            Thread.sleep(1500)

            // Sideways moves the cursor: back along a command, then a flick to its start.
            js(sc, "gecko.sendInput($id, 'clear\\r')")
            Thread.sleep(600)
            js(sc, "gecko.sendInput($id, 'git commit -m \"fix the flaky test\"')")
            device.click(cx, r.top + 250) // a tap: the keyboard, and the cursor shows
            Thread.sleep(1800)
            val hy = r.top + 380
            hDrag(cx + 300, hy, cx - 100, 1800, hold = 300)
            Thread.sleep(1200)
            hDrag(cx - 200, hy, cx + 50, 1300, hold = 300)
            Thread.sleep(1200)
            sideFlick(cx + 250, hy, -420)
            Thread.sleep(2500)
            js(sc, "gecko.sendInput($id, '\\u0005\\u0015')") // end, clear the line
            js(sc, "document.activeElement.blur()")
            Thread.sleep(1000)

            // Claude working there: its logo moves, on the tab and the workspace.
            js(sc, "gecko.sendInput($id, '~/run/fakebin/claude 14 30\\r')")
            Thread.sleep(5000)
            device.swipe(r.left + 4, cy, r.left + 650, cy, 40) // the drawer
            Thread.sleep(4500)
            device.swipe(r.left + 650, cy, r.left + 50, cy, 30)
            Thread.sleep(3500)

            // ⋮ > Touch diagnostics: what that last gesture looked like.
            device.findObject(By.desc("More options"))?.click()
            device.wait(Until.findObject(By.text("Touch diagnostics")), 3000)?.click()
            Thread.sleep(5000)
            device.findObject(By.text("OK"))?.click()
            Thread.sleep(1500)
        }
    }

    // A finger flick down by dist px over ms: 120 Hz events with their own
    // times, a slow start inside the touch slop, then quick; lifted moving.
    private fun fingerFlick(x: Int, fromY: Int, dist: Int, ms: Long) {
        val t0 = SystemClock.uptimeMillis()
        inject(MotionEvent.ACTION_DOWN, t0, x, fromY, t0, false)
        val n = (ms / 8).toInt().coerceAtLeast(4)
        var t = t0
        for (i in 1..n) {
            t += 8
            while (SystemClock.uptimeMillis() < t) Thread.sleep(1)
            val f = i.toDouble() / n
            inject(MotionEvent.ACTION_MOVE, t0, x, fromY + (dist * f * f).toInt(), t, false)
        }
        inject(MotionEvent.ACTION_UP, t0, x, fromY + dist, t + 4, false)
    }

    private fun sideFlick(fromX: Int, y: Int, dx: Int) {
        val t0 = SystemClock.uptimeMillis()
        inject(MotionEvent.ACTION_DOWN, t0, fromX, y, t0, false)
        var t = t0
        for (i in 1..8) {
            t += 8
            while (SystemClock.uptimeMillis() < t) Thread.sleep(1)
            inject(MotionEvent.ACTION_MOVE, t0, fromX + dx * i / 8, y, t, false)
        }
        inject(MotionEvent.ACTION_UP, t0, fromX + dx, y, t + 4, false)
    }

    // A finger up or down (drag) or sideways (hDrag) over ms, held still
    // for hold ms before lifting.
    private fun drag(x: Int, fromY: Int, toY: Int, ms: Long, hold: Long = 300) = line(x, fromY, x, toY, ms, hold)

    private fun hDrag(fromX: Int, y: Int, toX: Int, ms: Long, hold: Long = 300) = line(fromX, y, toX, y, ms, hold)

    private fun line(x0: Int, y0: Int, x1: Int, y1: Int, ms: Long, hold: Long) {
        val t0 = SystemClock.uptimeMillis()
        inject(MotionEvent.ACTION_DOWN, t0, x0, y0)
        val steps = (ms / 16).toInt()
        for (i in 1..steps) {
            inject(MotionEvent.ACTION_MOVE, t0, x0 + (x1 - x0) * i / steps, y0 + (y1 - y0) * i / steps)
            Thread.sleep(16)
        }
        Thread.sleep(hold)
        inject(MotionEvent.ACTION_UP, t0, x1, y1)
    }

    private fun inject(action: Int, downTime: Long, x: Int, y: Int, at: Long = SystemClock.uptimeMillis(), wait: Boolean = true) {
        val ev = MotionEvent.obtain(downTime, at, action, x.toFloat(), y.toFloat(), 0)
        ev.source = InputDevice.SOURCE_TOUCHSCREEN
        InstrumentationRegistry.getInstrumentation().uiAutomation.injectInputEvent(ev, wait)
        ev.recycle()
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
