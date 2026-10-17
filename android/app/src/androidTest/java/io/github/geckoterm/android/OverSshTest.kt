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
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/**
 * Gestures through the phone terminal's own ssh to a real sshd
 * (-e sshTarget user@host, which must accept this phone's key): the remote
 * shell's scrollback, tmux with the mouse on and off, and a full-screen
 * program with mouse reporting (less --mouse). Real touches (UiAutomator).
 */
@RunWith(AndroidJUnit4::class)
class OverSshTest {
    private val ctx get() = ApplicationProvider.getApplicationContext<android.content.Context>()
    private val device get() = UiDevice.getInstance(InstrumentationRegistry.getInstrumentation())

    @Test fun gesturesOverSsh() {
        val target = InstrumentationRegistry.getArguments().getString("sshTarget")
        assumeTrue("no sshTarget argument", !target.isNullOrEmpty())
        val intent = Intent(ctx, TerminalActivity::class.java).putExtra(TerminalActivity.EXTRA_MACHINE, LocalGecko.ID)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        ActivityScenario.launch<TerminalActivity>(intent).use { sc ->
            waitFor(sc, "window.gecko&&gecko.conn.status==='online'&&gecko.firstSync===true", 60)
            val before = js(sc, "gecko.activeId()")
            js(sc, "gecko.newTab()")
            waitFor(sc, "gecko.activeId()!==$before&&gecko.activeSession()&&!gecko.activeSession().exited")
            Thread.sleep(1500)
            val id = js(sc, "gecko.activeId()")
            waitFor(sc, "(gecko.focusSession($id), gecko.activeId()===$id)")
            val p = "gecko.panes.get($id)"
            fun send(s: String) = js(sc, "gecko.sendInput($id, ${TerminalActivity.js(s)})")
            send("ssh -o StrictHostKeyChecking=accept-new -o BatchMode=yes $target\r")
            send("echo ssh-ok-\$((6*7))\r")
            waitFor(sc, "screen().includes('ssh-ok-42')", 30)
            assertEquals("\"ssh\"", js(sc, "gecko.sessions.get($id).kind"))

            js(sc, "document.activeElement.blur()")
            val web = device.findObject(UiSelector().className("android.webkit.WebView")).bounds
            val cx = web.centerX()
            val cy = web.centerY()
            fun settle(expr: String): String {
                var last = ""
                for (i in 0 until 50) {
                    Thread.sleep(400)
                    val now = js(sc, expr)
                    if (now == last) return now
                    last = now
                }
                return last
            }

            // (a) The remote shell's scrollback: a fast flick goes further.
            send("clear; seq 1 3000\r")
            waitFor(sc, "$p.term.buffer.active.baseY>2900", 30)
            Thread.sleep(500)
            val y = "$p.term.buffer.active.viewportY"
            fun scrollback(steps: Int): Int {
                js(sc, "$p.term.scrollToBottom()")
                Thread.sleep(400)
                val b = js(sc, y).toInt()
                device.swipe(cx, cy - 250, cx, cy + 250, steps)
                return b - settle(y).toInt()
            }
            val aSlow = scrollback(80)
            val aFast = scrollback(6)
            android.util.Log.i("OverSshTest", "(a) scrollback: slow $aSlow, fast $aFast")
            assertTrue("(a) scrollback: slow $aSlow, fast $aFast", aSlow > 0 && aFast > aSlow * 2)

            // (b) tmux with the mouse on: the swipe enters copy mode, and a
            // fast flick goes further; sideways moves the cursor in the pane.
            send("clear; tmux -L gtest -f /dev/null new -s t \\; set -g mouse on\r")
            waitFor(sc, "$p.term.buffer.active.type==='alternate'&&$p.term.modes.mouseTrackingMode!=='none'", 20)
            send("seq 1 3000\r")
            Thread.sleep(2000)
            val pos = "(($p.term.buffer.active.getLine($p.term.buffer.active.viewportY).translateToString(true).match(/\\[(\\d+)\\/\\d+\\]/)||[0,0])[1])"
            fun copyMode(steps: Int): Int {
                device.swipe(cx, cy - 250, cx, cy + 250, steps)
                val n = settle("String($pos)").trim('"').toInt()
                send("q")
                Thread.sleep(800)
                return n
            }
            val bSlow = copyMode(80)
            val bFast = copyMode(6)
            android.util.Log.i("OverSshTest", "(b) tmux copy mode: slow $bSlow, fast $bFast")
            assertTrue("(b) tmux copy mode: slow $bSlow, fast $bFast", bSlow > 0 && bFast > bSlow * 2)
            send("\u0015echo abcdefghijklmnop")
            Thread.sleep(1200)
            val cx0 = js(sc, "$p.term.buffer.active.cursorX").toInt()
            device.swipe(cx + 300, cy, cx - 100, cy, 60)
            Thread.sleep(1200)
            val cx1 = js(sc, "$p.term.buffer.active.cursorX").toInt()
            assertTrue("(b) sideways in tmux: cursor $cx0 -> $cx1", cx0 - cx1 >= 8)

            // (c) tmux with the mouse off: a swipe sends nothing (arrow keys
            // would page through the shell's history), and says so.
            send("\u0015tmux -L gtest set -g mouse off; clear\r")
            waitFor(sc, "$p.term.modes.mouseTrackingMode==='none'", 10)
            waitFor(sc, "!!gecko.sessions.get($id).remoteTmux", 15)
            Thread.sleep(800)
            val shown = "JSON.stringify([...Array($p.term.rows).keys()].map(i=>$p.term.buffer.active.getLine($p.term.buffer.active.viewportY+i).translateToString(true)))"
            val s0 = js(sc, shown)
            device.swipe(cx, cy - 250, cx, cy + 250, 30)
            Thread.sleep(1500)
            assertEquals("(c) nothing sent", s0, js(sc, shown))
            assertTrue("(c) says why", js(sc, "[...document.querySelectorAll('.toast')].map(t=>t.textContent).join()").contains("mouse on"))
            send("tmux -L gtest kill-server\r")
            waitFor(sc, "$p.term.buffer.active.type==='normal'", 10)

            // (d) A full-screen program with mouse reporting (less --mouse):
            // a fast flick scrolls it further, step by step.
            send("clear; seq -f 'less line %g' 1 5000 | less --mouse\r")
            waitFor(sc, "$p.term.modes.mouseTrackingMode!=='none'", 10)
            Thread.sleep(800)
            val top = "(+(($p.term.buffer.active.getLine($p.term.buffer.active.viewportY).translateToString(true).match(/less line (\\d+)/)||[0,0])[1]))"
            fun less(steps: Int): Int {
                val b = js(sc, top).toInt()
                device.swipe(cx, cy + 250, cx, cy - 250, steps)
                return settle(top).toInt() - b
            }
            val dSlow = less(80)
            val dFast = less(6)
            android.util.Log.i("OverSshTest", "(d) less --mouse: slow $dSlow, fast $dFast")
            assertTrue("(d) less --mouse: slow $dSlow, fast $dFast", dSlow > 0 && dFast > dSlow * 2)
            val stats = js(sc, "JSON.stringify(gecko.touchStats)")
            assertTrue("(d) wheel steps, spaced out: $stats", stats.contains("wheelSteps"))
            send("q")
            send("exit\r")
        }
    }

    private fun js(sc: ActivityScenario<TerminalActivity>, expr: String): String {
        val latch = CountDownLatch(1)
        var out = ""
        sc.onActivity { a ->
            a.web.evaluateJavascript(
                """(function(){var screen=function(){var t=gecko.activePane()&&gecko.activePane().term;if(!t)return '';var b=t.buffer.active,o=[];for(var i=0;i<b.length;i++){var l=b.getLine(i);if(l)o.push(l.translateToString(true))}return o.join('\n')};return ($expr)})()""",
            ) { out = it ?: ""; latch.countDown() }
        }
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
