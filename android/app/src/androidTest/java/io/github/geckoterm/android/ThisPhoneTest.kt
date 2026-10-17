package io.github.geckoterm.android

import android.content.Intent
import android.os.Build
import androidx.test.core.app.ActivityScenario
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.uiautomator.UiDevice
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import java.net.HttpURLConnection
import java.net.URL
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/** Gecko running on the device itself: a shell, its survival in the background, and sharing. */
@RunWith(AndroidJUnit4::class)
class ThisPhoneTest {
    private val ctx get() = ApplicationProvider.getApplicationContext<android.content.Context>()

    @After fun stop() {
        runCatching { LocalGecko.share(ctx, false) }
    }

    @Test fun aShellOnThePhoneSurvivesTheBackground() {
        val intent = Intent(ctx, TerminalActivity::class.java).putExtra(TerminalActivity.EXTRA_MACHINE, LocalGecko.ID)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        var id: String
        ActivityScenario.launch<TerminalActivity>(intent).use { sc ->
            waitFor(sc, "window.gecko&&gecko.conn.status==='online'&&gecko.firstSync===true", 60)
            js(sc, "gecko.newTab()")
            waitFor(sc, "gecko.activePane()&&gecko.activeSession()&&!gecko.activeSession().exited")
            id = js(sc, "gecko.activeId()").trim('"')
            js(sc, "gecko.sendInput(gecko.activeId(), 'uname -a; id; getprop ro.product.model; ls /sdcard; echo marker-\$((6*7))\\r')")
            waitFor(sc, "screen().includes('marker-42')")
            val text = js(sc, "screen()")
            assertTrue(text, text.contains("Linux"))
            assertTrue(text, text.contains("uid="))
            assertTrue(text, text.contains(Build.MODEL))
            assertTrue(text, text.contains("Download"))
        }
        // Leave the app; the service keeps Gecko and the shell running.
        val device = UiDevice.getInstance(InstrumentationRegistry.getInstrumentation())
        device.pressHome()
        Thread.sleep(15_000)
        assertTrue(LocalGecko.running(ctx))
        ActivityScenario.launch<TerminalActivity>(intent).use { sc ->
            waitFor(sc, "window.gecko&&gecko.conn.status==='online'&&gecko.sessions.has('$id')", 60)
            js(sc, "gecko.focusSession('$id')")
            waitFor(sc, "gecko.activeId()==='$id'&&screen().includes('marker-42')")
        }
    }

    @Test fun otherDevicesOnlyWhileAllowed() {
        assertTrue(LocalGecko.start(ctx))
        val st = LocalGecko.share(ctx, true)
        assertTrue(st.on)
        assertEquals(200, get("http://127.0.0.1:${st.port}/api/info", st.token))
        assertEquals(401, get("http://127.0.0.1:${st.port}/api/info", "not-the-token-0000"))
        LocalGecko.share(ctx, false)
        assertEquals(-1, get("http://127.0.0.1:${st.port}/api/info", st.token))
        val again = LocalGecko.share(ctx, true)
        assertTrue(again.token != st.token)
        assertEquals(401, get("http://127.0.0.1:${again.port}/api/info", st.token))
    }

    private fun get(url: String, token: String): Int = try {
        val c = URL(url).openConnection() as HttpURLConnection
        c.setRequestProperty("Authorization", "Bearer $token")
        c.connectTimeout = 3000
        c.responseCode.also { c.disconnect() }
    } catch (e: Exception) {
        -1
    }

    private val screenJs = "(function(){var b=gecko.activePane().term.buffer.active,o=[];for(var i=0;i<b.length;i++)o.push(b.getLine(i).translateToString(true));return o.join('\\n')})()"

    private fun js(sc: ActivityScenario<TerminalActivity>, expr: String): String {
        val latch = CountDownLatch(1)
        var out = ""
        val e = expr.replace("screen()", screenJs)
        sc.onActivity { a -> a.web.evaluateJavascript(e) { out = it ?: ""; latch.countDown() } }
        latch.await(10, TimeUnit.SECONDS)
        return if (out.startsWith("\"")) org.json.JSONArray("[$out]").getString(0) else out
    }

    private fun waitFor(sc: ActivityScenario<TerminalActivity>, cond: String, seconds: Int = 30) {
        val until = System.currentTimeMillis() + seconds * 1000L
        while (System.currentTimeMillis() < until) {
            if (js(sc, "!!($cond)") == "true") return
            Thread.sleep(300)
        }
        throw AssertionError("timed out waiting for $cond")
    }
}
