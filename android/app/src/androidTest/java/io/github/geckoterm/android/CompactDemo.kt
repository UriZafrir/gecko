package io.github.geckoterm.android

import android.content.Intent
import androidx.test.core.app.ActivityScenario
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.uiautomator.UiDevice
import org.junit.Assume.assumeTrue
import org.junit.Test
import org.junit.runner.RunWith
import java.io.File
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/**
 * Not a test: the phone's top bar and agent dock with a few tabs in a git
 * repo and two agents (one working, one asking), for screenshots
 * (files/compact-<shot>.png). Runs only with -e demo compact, -e shot
 * <name>, -e geckoUrl <link> and -e demoDir (the repo), -e working and
 * -e asking (stand-ins for Claude Code).
 */
@RunWith(AndroidJUnit4::class)
class CompactDemo {
    private val ctx get() = ApplicationProvider.getApplicationContext<android.content.Context>()
    private val device get() = UiDevice.getInstance(InstrumentationRegistry.getInstrumentation())
    private var activity: TerminalActivity? = null

    @Test fun demo() {
        val args = InstrumentationRegistry.getArguments()
        assumeTrue(args.getString("demo") == "compact")
        val shot = args.getString("shot") ?: "shot"
        val dir = args.getString("demoDir")!!
        val link = LinkParser.parse(args.getString("geckoUrl")!!)
        val m = Machine(name = "demo", url = link.baseUrl, token = link.token!!)
        Gecko.machines(ctx).save(m)
        val intent = Intent(ctx, TerminalActivity::class.java).putExtra(TerminalActivity.EXTRA_MACHINE, m.id)
        ActivityScenario.launch<TerminalActivity>(intent).use { sc ->
            waitFor(sc, "window.gecko&&gecko.conn.status==='online'&&gecko.firstSync===true", 60)
            js(sc, "localStorage.removeItem('gecko.dockFolded')")
            // A shell deep in the repo, Claude at work, Claude asking.
            val shell = tab(sc, "$dir/src/payments")
            val work = tab(sc, dir)
            js(sc, "gecko.sendInput($work, '${args.getString("working")} 600 30\\r')")
            val ask = tab(sc, dir)
            js(sc, "gecko.sendInput($ask, '${args.getString("asking")} 600\\r')")
            waitFor(sc, "document.querySelectorAll('#dock .agent').length>=2", 40)
            js(sc, "gecko.focusSession($shell)")
            js(sc, "gecko.sendInput($shell, 'clear; git status --short --branch\\r')")
            js(sc, "document.activeElement.blur()")
            Thread.sleep(2500)
            device.takeScreenshot(File(ctx.getExternalFilesDir(null), "compact-$shot.png"))
            if (args.getString("open") == "true") {
                // The asking agent's chip: its answers.
                js(sc, "document.querySelector('#dock .agent.s-needs-input').click()")
                Thread.sleep(1500)
                device.takeScreenshot(File(ctx.getExternalFilesDir(null), "compact-$shot-open.png"))
                js(sc, "gecko.focusSession($shell)")
                js(sc, "document.querySelector('#dock [data-fold]').click()")
                Thread.sleep(1500)
                device.takeScreenshot(File(ctx.getExternalFilesDir(null), "compact-$shot-folded.png"))
                js(sc, "document.querySelector('#dock [data-fold]').click()")
            }
            for (id in listOf(work, ask)) js(sc, "gecko.sendInput($id, '\\u0003')")
        }
    }

    private fun tab(sc: ActivityScenario<TerminalActivity>, cwd: String): String {
        val before = js(sc, "gecko.activeId()")
        js(sc, "gecko.newTab({cwd: '$cwd'})")
        waitFor(sc, "gecko.activeId()!==$before&&gecko.activePane()&&gecko.activePane().term.buffer.active.getLine(0).translateToString(true).trim().length>0")
        Thread.sleep(500)
        return js(sc, "gecko.activeId()")
    }

    // Not through sc.onActivity, which waits for an idle main thread (never, while a logo fades).
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
