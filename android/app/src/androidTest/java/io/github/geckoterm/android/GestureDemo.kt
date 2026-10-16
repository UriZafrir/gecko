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
import org.junit.Assume.assumeTrue
import org.junit.Test
import org.junit.runner.RunWith
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/**
 * Not a test: the gestures, slowly, for the demo video (08-gestures.mp4).
 * Runs only with -e demo 1 and -e geckoUrl <link>.
 */
@RunWith(AndroidJUnit4::class)
class GestureDemo {
    private val ctx get() = ApplicationProvider.getApplicationContext<android.content.Context>()
    private val device get() = UiDevice.getInstance(InstrumentationRegistry.getInstrumentation())

    @Test fun demo() {
        val args = InstrumentationRegistry.getArguments()
        assumeTrue(args.getString("demo") == "1")
        val link = LinkParser.parse(args.getString("geckoUrl")!!)
        val m = Machine(name = "demo", url = link.baseUrl, token = link.token!!)
        Gecko.machines(ctx).save(m)
        val intent = Intent(ctx, TerminalActivity::class.java).putExtra(TerminalActivity.EXTRA_MACHINE, m.id)
        ActivityScenario.launch<TerminalActivity>(intent).use { sc ->
            waitFor(sc, "window.gecko&&gecko.conn.status==='online'&&gecko.firstSync===true", 60)
            js(sc, "gecko.newTab()")
            waitFor(sc, "gecko.activePane()&&gecko.activePane().term.buffer.active.getLine(0).translateToString(true).trim().length>0")
            val first = js(sc, "gecko.activeId()")
            js(sc, "gecko.newTab()")
            waitFor(sc, "gecko.activeId()!==$first")
            Thread.sleep(1500)
            waitFor(sc, "(gecko.focusSession($first), gecko.activeId()===$first)")
            js(sc, "gecko.sendInput($first, 'clear; seq -f \"line %g of 200\" 1 200\\r')")
            waitFor(sc, "gecko.panes.get($first).term.buffer.active.baseY>100")
            js(sc, "document.activeElement.blur()")
            // Android's Show taps doesn't draw injected touches: draw them in the page.
            js(sc, TOUCH_DOTS)
            Thread.sleep(1500)
            val r = device.findObject(UiSelector().className("android.webkit.WebView")).bounds
            val cx = r.centerX()
            val cy = r.centerY()

            // Scroll back, slowly, then a flick, then back down.
            device.swipe(cx, cy - 400, cx, cy + 300, 120)
            Thread.sleep(1200)
            device.swipe(cx, cy - 300, cx, cy + 300, 12)
            Thread.sleep(2500)
            device.swipe(cx, cy + 500, cx, cy - 500, 10)
            Thread.sleep(2500)

            // less with the mouse on: the swipe scrolls less.
            js(sc, "gecko.sendInput($first, 'seq -f \"less line %g\" 1 500 | less --mouse\\r')")
            Thread.sleep(1500)
            device.swipe(cx, cy + 400, cx, cy - 400, 80)
            Thread.sleep(1500)
            device.swipe(cx, cy - 300, cx, cy + 300, 80)
            Thread.sleep(1200)
            js(sc, "gecko.sendInput($first, 'q')")
            Thread.sleep(1000)

            // Pinch out and in: the font size.
            val web = device.findObject(By.clazz("android.webkit.WebView"))
            web.pinchOpen(0.5f, 400)
            Thread.sleep(2000)
            web.pinchClose(0.45f, 400)
            Thread.sleep(2000)

            // Two fingers to the left: the next tab; and back.
            val wv = device.findObject(UiSelector().className("android.webkit.WebView"))
            twoFingerSwipe(wv, r.right - 200, r.left + 200, cy)
            Thread.sleep(2000)
            twoFingerSwipe(wv, r.left + 200, r.right - 200, cy)
            Thread.sleep(2000)

            // A swipe on the tab bar.
            device.swipe(r.right - 200, r.top + 80, r.left + 250, r.top + 80, 30)
            Thread.sleep(2000)
            device.swipe(r.left + 250, r.top + 80, r.right - 200, r.top + 80, 30)
            Thread.sleep(2000)

            // In from the left edge: the drawer; swipe it closed.
            device.swipe(r.left + 4, cy, r.left + 650, cy, 40)
            Thread.sleep(2500)
            device.swipe(r.left + 650, cy, r.left + 50, cy, 30)
            Thread.sleep(2000)
        }
    }

    private val TOUCH_DOTS = """(function(){var s=document.createElement('style');
        s.textContent='.tdot{position:fixed;z-index:99;width:46px;height:46px;margin:-23px 0 0 -23px;border-radius:50%;background:rgba(255,255,255,.45);border:3px solid rgba(40,40,40,.55);pointer-events:none}';
        document.head.appendChild(s);var dots={};
        function draw(e){var seen={};for(var i=0;i<e.touches.length;i++){var t=e.touches[i];var d=dots[t.identifier];
          if(!d){d=dots[t.identifier]=document.createElement('div');d.className='tdot';document.body.appendChild(d);}
          d.style.left=t.clientX+'px';d.style.top=t.clientY+'px';seen[t.identifier]=1;}
          for(var k in dots){if(!seen[k]){dots[k].remove();delete dots[k];}}}
        ['touchstart','touchmove','touchend','touchcancel'].forEach(function(n){window.addEventListener(n,draw,{capture:true,passive:true});});})()"""

    private fun twoFingerSwipe(web: androidx.test.uiautomator.UiObject, fromX: Int, toX: Int, y: Int) {
        val steps = 30
        fun pts(dy: Int) = Array(steps + 1) { i ->
            PointerCoords().apply { x = (fromX + (toX - fromX) * i / steps).toFloat(); this.y = (y + dy).toFloat(); pressure = 1f; size = 1f }
        }
        web.performMultiPointerGesture(pts(-150), pts(150))
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
