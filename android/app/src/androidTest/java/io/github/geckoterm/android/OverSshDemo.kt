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
 * Not a test: gestures over ssh from the phone terminal, for the demo video
 * (11-over-ssh.mp4): the remote shell, tmux with the mouse on and off,
 * less --mouse, then tmux in a tab of a machine running an older Gecko.
 * Runs only with -e demo ssh, -e sshTarget user@host, -e geckoOldUrl <link>.
 */
@RunWith(AndroidJUnit4::class)
class OverSshDemo {
    private val ctx get() = ApplicationProvider.getApplicationContext<android.content.Context>()
    private val device get() = UiDevice.getInstance(InstrumentationRegistry.getInstrumentation())

    @Test fun demo() {
        val args = InstrumentationRegistry.getArguments()
        assumeTrue(args.getString("demo") == "ssh")
        val target = args.getString("sshTarget")!!
        val phone = Intent(ctx, TerminalActivity::class.java).putExtra(TerminalActivity.EXTRA_MACHINE, LocalGecko.ID)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        ActivityScenario.launch<TerminalActivity>(phone).use { sc ->
            waitFor(sc, "window.gecko&&gecko.conn.status==='online'&&gecko.firstSync===true", 60)
            val id = newTab(sc)
            js(sc, GestureDemo.TOUCH_DOTS)
            val r = device.findObject(UiSelector().className("android.webkit.WebView")).bounds
            val cx = r.centerX()
            val cy = r.centerY()
            fun send(s: String) = js(sc, "gecko.sendInput($id, ${TerminalActivity.js(s)})")

            // ssh from the phone's own terminal.
            send("ssh -o StrictHostKeyChecking=accept-new $target\r")
            Thread.sleep(2500)
            send("clear; seq -f 'remote line %g' 1 3000\r")
            Thread.sleep(2000)
            js(sc, "document.activeElement.blur()")
            Thread.sleep(800)

            // (a) The remote shell's scrollback: a slow flick, a fast one.
            fingerFlick(cx, cy - 250, 380, 170)
            Thread.sleep(2500)
            fingerFlick(cx, cy - 300, 650, 64)
            Thread.sleep(4500)

            // (b) tmux with the mouse on: flicks scroll it (copy mode), slowing down.
            send("clear; tmux -L demo -f /dev/null new -s demo \\; set -g mouse on\r")
            Thread.sleep(2000)
            send("seq -f 'tmux line %g' 1 3000\r")
            Thread.sleep(2000)
            js(sc, "document.activeElement.blur()")
            fingerFlick(cx, cy - 250, 380, 170)
            Thread.sleep(2500)
            fingerFlick(cx, cy - 300, 650, 64)
            Thread.sleep(6000)
            send("q")
            Thread.sleep(800)
            // Sideways: the cursor in the pane.
            send("echo the cursor moves in tmux too")
            device.click(cx, r.top + 250)
            Thread.sleep(1500)
            hDrag(cx + 300, r.top + 380, cx - 150, 1600, hold = 300)
            Thread.sleep(1200)
            sideFlick(cx - 200, r.top + 380, 420)
            Thread.sleep(1500)
            send("\u0015")
            js(sc, "document.activeElement.blur()")
            Thread.sleep(800)

            // (c) tmux with the mouse off: the swipe sends nothing, and says why.
            send("tmux -L demo set -g mouse off; clear\r")
            Thread.sleep(1500)
            drag(cx, cy - 250, cy + 250, 700, hold = 200)
            Thread.sleep(4500)
            send("tmux -L demo kill-server\r")
            Thread.sleep(1500)

            // (d) less --mouse (full screen, mouse reporting): slow, then fast.
            send("clear; seq -f 'less line %g' 1 5000 | less --mouse\r")
            Thread.sleep(2000)
            js(sc, "document.activeElement.blur()")
            fingerFlick(cx, cy + 250, -380, 170)
            Thread.sleep(2500)
            fingerFlick(cx, cy + 300, -650, 64)
            Thread.sleep(5000)
            send("q")
            send("exit\r")
            Thread.sleep(1000)
        }

        // (e) A machine running an older Gecko, with the app's UI: tmux there.
        val link = LinkParser.parse(args.getString("geckoOldUrl")!!)
        val m = Machine(name = "older Gecko", url = link.baseUrl, token = link.token!!)
        Gecko.machines(ctx).save(m)
        val older = Intent(ctx, TerminalActivity::class.java).putExtra(TerminalActivity.EXTRA_MACHINE, m.id)
        ActivityScenario.launch<TerminalActivity>(older).use { sc ->
            waitFor(sc, "window.gecko&&gecko.conn.status==='online'&&gecko.firstSync===true", 60)
            val id = newTab(sc)
            js(sc, GestureDemo.TOUCH_DOTS)
            val r = device.findObject(UiSelector().className("android.webkit.WebView")).bounds
            val cx = r.centerX()
            val cy = r.centerY()
            fun send(s: String) = js(sc, "gecko.sendInput($id, ${TerminalActivity.js(s)})")
            send("clear; tmux -L demo2 -f /dev/null new -s older \\; set -g mouse on\r")
            Thread.sleep(2000)
            send("seq -f 'older gecko line %g' 1 3000\r")
            Thread.sleep(2000)
            js(sc, "document.activeElement.blur()")
            fingerFlick(cx, cy - 250, 380, 170)
            Thread.sleep(2500)
            fingerFlick(cx, cy - 300, 650, 64)
            Thread.sleep(6000)
            send("q")
            send("tmux -L demo2 kill-server\r")
            Thread.sleep(1000)
        }
    }

    private fun newTab(sc: ActivityScenario<TerminalActivity>): String {
        val before = js(sc, "gecko.activeId()")
        js(sc, "gecko.newTab()")
        waitFor(sc, "gecko.activeId()!==$before&&gecko.activePane()&&gecko.activePane().term.buffer.active.getLine(0).translateToString(true).trim().length>0")
        val id = js(sc, "gecko.activeId()")
        Thread.sleep(1500)
        waitFor(sc, "(gecko.focusSession($id), gecko.activeId()===$id)")
        return id
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
