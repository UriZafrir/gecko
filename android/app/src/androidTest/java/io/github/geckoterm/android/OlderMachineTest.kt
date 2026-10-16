package io.github.geckoterm.android

import android.content.Intent
import androidx.test.core.app.ActivityScenario
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.assertEquals
import org.junit.Assume.assumeTrue
import org.junit.Test
import org.junit.runner.RunWith
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/**
 * A machine running an older Gecko (-e geckoOldUrl <link>): the app shows
 * its own UI there, with the phone features, and the tabs work.
 */
@RunWith(AndroidJUnit4::class)
class OlderMachineTest {
    private val ctx get() = ApplicationProvider.getApplicationContext<android.content.Context>()

    @Test fun theAppsOwnUiOnAnOlderGecko() {
        val url = InstrumentationRegistry.getArguments().getString("geckoOldUrl")
        assumeTrue("no geckoOldUrl argument", !url.isNullOrEmpty())
        val link = LinkParser.parse(url!!)
        val m = Machine(name = "older", url = link.baseUrl, token = link.token!!)
        Gecko.machines(ctx).save(m)
        val intent = Intent(ctx, TerminalActivity::class.java).putExtra(TerminalActivity.EXTRA_MACHINE, m.id)
        ActivityScenario.launch<TerminalActivity>(intent).use { sc ->
            waitFor(sc, "window.gecko&&gecko.conn.status==='online'&&gecko.firstSync===true", 60)
            assertEquals("served from the app", "true", js(sc, "gecko.bundled"))
            assertEquals("with the touch code", "\"function\"", js(sc, "typeof gecko.touchReport"))
            assertEquals("the machine speaks an older protocol", "true", js(sc, "gecko.daemonVersion<7"))
            assertEquals("no restart nag", "false", js(sc, "!!gecko.outdated"))
            // A tab there works.
            val before = js(sc, "gecko.activeId()")
            js(sc, "gecko.newTab()")
            waitFor(sc, "gecko.activeId()!==$before&&gecko.activePane()&&gecko.activePane().term.buffer.active.getLine(0).translateToString(true).trim().length>0")
            val id = js(sc, "gecko.activeId()")
            js(sc, "gecko.sendInput($id, 'echo older-ok-\$((6*7))\\r')")
            waitFor(sc, "(()=>{const b=gecko.panes.get($id).term.buffer.active;for(let i=0;i<b.length;i++){if((b.getLine(i)?.translateToString(true)||'').includes('older-ok-42'))return true}return false})()")
        }
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
