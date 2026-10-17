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
import org.junit.Assume.assumeTrue
import org.junit.Test
import org.junit.runner.RunWith
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/**
 * Not a test: scrolling, slowly, for the demo video (09-scrolling.mp4).
 * Runs only with -e demo scroll and -e geckoUrl <link>.
 */
@RunWith(AndroidJUnit4::class)
class ScrollDemo {
    private val ctx get() = ApplicationProvider.getApplicationContext<android.content.Context>()
    private val device get() = UiDevice.getInstance(InstrumentationRegistry.getInstrumentation())

    @Test fun demo() {
        val args = InstrumentationRegistry.getArguments()
        assumeTrue(args.getString("demo") == "scroll")
        val link = LinkParser.parse(args.getString("geckoUrl")!!)
        val m = Machine(name = "demo", url = link.baseUrl, token = link.token!!)
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
            Thread.sleep(1500)
            val r = device.findObject(UiSelector().className("android.webkit.WebView")).bounds
            val cx = r.centerX()
            val cy = r.centerY()

            // A slow drag: the text stays under the finger, pixel by pixel.
            drag(cx, cy - 350, cy + 350, 2600)
            Thread.sleep(600)
            drag(cx, cy + 350, cy - 150, 2000)
            Thread.sleep(1500)

            // A slow flick, then a fast one: much further.
            flick(cx, cy - 200, cy + 200, 300)
            Thread.sleep(3000)
            flick(cx, cy - 320, cy + 320, 56)
            Thread.sleep(5000)

            // The fast-scroll thumb: to the middle, the top, the bottom.
            device.swipe(cx, cy, cx, cy - 60, 30) // brings it up
            Thread.sleep(300)
            val geo = js(sc, "(()=>{const p=gecko.panes.get($id).el,r=p.getBoundingClientRect(),i=p.querySelector('.fastscroll i').getBoundingClientRect(),d=devicePixelRatio;return [r.right*d-30,i.top*d+i.height*d/2,r.top*d,r.bottom*d].join(',')})()")
                .trim('"').split(",").map { it.toDouble() }
            val tx = r.left + geo[0].toInt()
            val top = r.top + geo[2].toInt()
            val bottom = r.top + geo[3].toInt()
            val mid = (top + bottom) / 2
            drag(tx, r.top + geo[1].toInt(), mid, 1200, hold = 600)
            Thread.sleep(800)
            drag(tx, mid, top + 20, 1000, hold = 600)
            Thread.sleep(1200)

            // At the top: pull past it, it stretches, and springs back.
            drag(cx, cy - 300, cy + 450, 1300, hold = 500)
            Thread.sleep(1500)

            // To the bottom with the thumb, then pull up past the end.
            device.swipe(cx, cy, cx, cy + 60, 30)
            Thread.sleep(300)
            drag(tx, top + 30, bottom - 20, 1200, hold = 600)
            Thread.sleep(1200)
            drag(cx, cy + 300, cy - 450, 1300, hold = 500)
            Thread.sleep(1500)

            // A fling into the end bounces.
            flick(cx, cy - 100, cy + 100, 250)
            Thread.sleep(2000)
            flick(cx, cy + 150, cy - 150, 64)
            Thread.sleep(2500)
        }
    }

    // A finger from (x, fromY) to toY over ms, held still for hold ms before lifting.
    private fun drag(x: Int, fromY: Int, toY: Int, ms: Long, hold: Long = 300) {
        val t0 = SystemClock.uptimeMillis()
        inject(MotionEvent.ACTION_DOWN, t0, x, fromY)
        val steps = (ms / 16).toInt()
        for (i in 1..steps) {
            inject(MotionEvent.ACTION_MOVE, t0, x, fromY + (toY - fromY) * i / steps)
            Thread.sleep(16)
        }
        Thread.sleep(hold)
        inject(MotionEvent.ACTION_UP, t0, x, toY)
    }

    // A flick: from fromY to toY in ms, lifted while still moving. The
    // events carry their planned times and don't wait for each other, so
    // a slow emulator doesn't turn a quick flick into a slow drag.
    private fun flick(x: Int, fromY: Int, toY: Int, ms: Long) {
        val t0 = SystemClock.uptimeMillis()
        val steps = maxOf(4, (ms / 8).toInt())
        inject(MotionEvent.ACTION_DOWN, t0, x, fromY, at = t0, wait = false)
        for (i in 1..steps) {
            val at = t0 + ms * i / steps
            while (SystemClock.uptimeMillis() < at) Thread.sleep(1)
            inject(MotionEvent.ACTION_MOVE, t0, x, fromY + (toY - fromY) * i / steps, at = at, wait = false)
        }
        inject(MotionEvent.ACTION_UP, t0, x, toY, at = t0 + ms + 4, wait = false)
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
