package io.github.geckoterm.android

import android.content.Intent
import androidx.test.core.app.ActivityScenario
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.uiautomator.By
import androidx.test.uiautomator.UiDevice
import androidx.test.uiautomator.Until
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Test
import org.junit.runner.RunWith
import java.io.File
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/**
 * The build id shows on the connect screen, at the bottom of the ⋮ menu
 * and first in Touch diagnostics, and is BuildConfig's. The menu part needs
 * a Gecko (geckoUrl, as in SmokeTest); with -e shot true it also saves a
 * screenshot of the open menu as files/build-id.png.
 */
@RunWith(AndroidJUnit4::class)
class BuildIdTest {
    private val ctx get() = ApplicationProvider.getApplicationContext<android.content.Context>()
    private val device get() = UiDevice.getInstance(InstrumentationRegistry.getInstrumentation())
    private val id = BuildId.format(BuildConfig.BUILD_COMMIT, BuildConfig.BUILD_TIME)

    @Test fun buildConfigHasOne() {
        assertTrue(BuildConfig.BUILD_COMMIT.isNotEmpty())
        assertTrue(BuildConfig.BUILD_COMMIT != "unknown")
        assertTrue(id.startsWith(BuildConfig.BUILD_COMMIT + " · "))
    }

    @Test fun connectScreenFooter() {
        ActivityScenario.launch(ConnectActivity::class.java).use { sc ->
            var shown = ""
            sc.onActivity { shown = it.findViewById<android.widget.TextView>(R.id.build_id).text.toString() }
            assertEquals(id, shown)
        }
    }

    @Test fun menuAndDiagnostics() {
        val url = InstrumentationRegistry.getArguments().getString("geckoUrl")
        assumeTrue("no geckoUrl argument: skipping the live part", !url.isNullOrEmpty())
        val link = LinkParser.parse(url!!)
        val m = Machine(name = "test", url = link.baseUrl, token = link.token!!)
        Gecko.machines(ctx).save(m)
        val intent = Intent(ctx, TerminalActivity::class.java).putExtra(TerminalActivity.EXTRA_MACHINE, m.id)
        ActivityScenario.launch<TerminalActivity>(intent).use { sc ->
            waitFor(sc, "window.gecko&&gecko.conn.status==='online'&&gecko.firstSync===true")
            Thread.sleep(1500)

            device.findObject(By.desc("More options")).click()
            val item = device.wait(Until.findObject(By.text(id)), 5000)
            assertNotNull("the ⋮ menu shows $id", item)
            if (InstrumentationRegistry.getArguments().getString("shot") == "true") {
                Thread.sleep(800)
                device.takeScreenshot(File(ctx.getExternalFilesDir(null), "build-id.png"))
            }
            item.click()
            // The clipboard reads back once the menu is gone and the app has focus again.
            var clip = ""
            val until = System.currentTimeMillis() + 5000
            while (clip.isEmpty() && System.currentTimeMillis() < until) {
                Thread.sleep(250)
                sc.onActivity { a ->
                    val cb = a.getSystemService(android.content.Context.CLIPBOARD_SERVICE) as android.content.ClipboardManager
                    clip = cb.primaryClip?.getItemAt(0)?.text?.toString() ?: ""
                }
            }
            assertEquals("Gecko for Android $id", clip)

            device.findObject(By.desc("More options")).click()
            device.wait(Until.findObject(By.text("Touch diagnostics")), 5000).click()
            val report = device.wait(Until.findObject(By.textStartsWith("App build: ")), 5000)
            assertNotNull("Touch diagnostics starts with the build", report)
            val lines = report.text.lines()
            assertEquals("App build: $id", lines[0])
            assertTrue(lines[1], lines[1].startsWith("Web UI build: " + BuildConfig.BUILD_COMMIT + " · "))
            device.findObject(By.text("OK")).click()
        }
    }

    private fun js(sc: ActivityScenario<TerminalActivity>, expr: String): String {
        val latch = CountDownLatch(1)
        var out = ""
        sc.onActivity { a -> a.web.evaluateJavascript(expr) { out = it ?: ""; latch.countDown() } }
        latch.await(10, TimeUnit.SECONDS)
        return out
    }

    private fun waitFor(sc: ActivityScenario<TerminalActivity>, cond: String, seconds: Int = 30) {
        val until = System.currentTimeMillis() + seconds * 1000L
        while (System.currentTimeMillis() < until) {
            if (js(sc, "!!($cond)") == "true") return
            Thread.sleep(250)
        }
        throw AssertionError("timed out waiting for $cond")
    }
}
