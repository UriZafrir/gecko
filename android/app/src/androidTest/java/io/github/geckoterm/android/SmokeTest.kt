package io.github.geckoterm.android

import android.content.Intent
import androidx.test.core.app.ActivityScenario
import androidx.test.core.app.ApplicationProvider
import androidx.test.espresso.Espresso.onView
import androidx.test.espresso.Espresso.pressBack
import androidx.test.espresso.action.ViewActions.click
import androidx.test.espresso.assertion.ViewAssertions.matches
import androidx.test.espresso.matcher.ViewMatchers.isDisplayed
import androidx.test.espresso.matcher.ViewMatchers.withContentDescription
import androidx.test.espresso.matcher.ViewMatchers.withId
import androidx.test.espresso.matcher.ViewMatchers.withText
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.assertEquals
import org.junit.Assume.assumeTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/**
 * Smoke test on a device or emulator. The second half needs a Gecko daemon:
 * pass its link with
 *   -Pandroid.testInstrumentationRunnerArguments.geckoUrl=http://10.0.2.2:7810/?token=...
 */
@RunWith(AndroidJUnit4::class)
class SmokeTest {
    private val ctx get() = ApplicationProvider.getApplicationContext<android.content.Context>()

    @Before fun clean() {
        val store = Gecko.machines(ctx)
        for (m in store.list()) store.remove(m.id)
    }

    @Test fun connectScreenStartsEmpty() {
        ActivityScenario.launch(ConnectActivity::class.java).use {
            onView(withText(R.string.empty_title)).check(matches(isDisplayed()))
            onView(withId(R.id.scan)).check(matches(isDisplayed()))
            onView(withId(R.id.paste)).check(matches(isDisplayed()))
        }
    }

    @Test fun tokensSurviveTheKeystore() {
        val box = KeystoreBox("gecko-test")
        val sealed = box.seal("secret-token-123")
        assert(!sealed.contains("secret"))
        assertEquals("secret-token-123", box.open(sealed))
    }

    @Test fun opensGeckoAndUsesTheExtraKeys() {
        val url = InstrumentationRegistry.getArguments().getString("geckoUrl")
        assumeTrue("no geckoUrl argument: skipping the live part", !url.isNullOrEmpty())
        val link = LinkParser.parse(url!!)
        val m = Machine(name = "test", url = link.baseUrl, token = link.token!!)
        Gecko.machines(ctx).save(m)
        val intent = Intent(ctx, TerminalActivity::class.java).putExtra(TerminalActivity.EXTRA_MACHINE, m.id)
        ActivityScenario.launch<TerminalActivity>(intent).use { sc ->
            waitFor(sc, "window.gecko&&gecko.conn.status==='online'&&gecko.firstSync===true")
            assertEquals("true", js(sc, "document.documentElement.classList.contains('android')"))
            assertEquals("\"none\"", js(sc, "getComputedStyle(document.getElementById('keybar')).display"))
            onView(withId(R.id.keys)).check(matches(isDisplayed()))

            // Sticky Ctrl: the native key turns on the web UI's modifier.
            onView(withText("ctrl")).perform(click())
            waitFor(sc, "gecko.mods.ctrl===true")
            onView(withText("ctrl")).perform(click())
            waitFor(sc, "gecko.mods.ctrl===false")

            // Back closes the drawer first and stays on the machine.
            js(sc, "document.body.classList.add('drawer')")
            pressBack()
            waitFor(sc, "!document.body.classList.contains('drawer')")
            onView(withContentDescription(R.string.key_menu)).check(matches(isDisplayed()))
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
