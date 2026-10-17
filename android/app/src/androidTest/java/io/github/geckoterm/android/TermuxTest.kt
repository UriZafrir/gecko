package io.github.geckoterm.android

import android.content.Context
import android.content.Intent
import androidx.test.core.app.ActivityScenario
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.uiautomator.By
import androidx.test.uiautomator.UiDevice
import androidx.test.uiautomator.Until
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Before
import org.junit.FixMethodOrder
import org.junit.Test
import org.junit.runner.RunWith
import org.junit.runners.MethodSorters
import java.io.File
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/**
 * Gecko in a real Termux on the device: the guided setup's checks, starting
 * gecko there with RUN_COMMAND, the link from `gecko url`, and a tab in
 * Termux running its packages. Needs Termux (F-Droid or GitHub build) with
 * allow-external-apps=true and gecko installed (packaging/termux/install.sh);
 * skipped without Termux. Optional arguments: -e termuxTools true (git,
 * tmux and node are installed there, check them), -e shots true (screens to
 * the app's files/termux).
 */
@RunWith(AndroidJUnit4::class)
@FixMethodOrder(MethodSorters.NAME_ASCENDING)
class TermuxTest {
    private val ctx get() = ApplicationProvider.getApplicationContext<Context>()
    private val inst get() = InstrumentationRegistry.getInstrumentation()
    private val args get() = InstrumentationRegistry.getArguments()
    private val device get() = UiDevice.getInstance(inst)

    @Before fun termux() {
        assumeTrue("Termux isn't installed", Termux.installed(ctx))
        shell("pm grant ${ctx.packageName} ${Termux.PERMISSION}")
        shell("pm grant ${ctx.packageName} android.permission.POST_NOTIFICATIONS")
    }

    private fun shell(cmd: String): String {
        val pfd = inst.uiAutomation.executeShellCommand(cmd)
        return android.os.ParcelFileDescriptor.AutoCloseInputStream(pfd).bufferedReader().readText()
    }

    /** Runs a command in Termux and returns what it printed (stdout and stderr). */
    private fun termux(script: String): TermuxResult {
        val r = Termux.run(ctx, script, "test", 30_000)
        assertNotNull("Termux didn't answer: $script", r)
        return r!!
    }

    @Test fun a_probeFindsGecko() {
        val s = TermuxFlow.probed(Termux.run(ctx, TermuxFlow.PROBE, "gecko version", 20_000))
        assertTrue("$s", s is TermuxState.Ready)
        assertTrue((s as TermuxState.Ready).version.startsWith("gecko "))
        // What Gecko sees in Termux: its prefix, no /tmp, Termux's shell.
        val env = termux("echo \"prefix=\$PREFIX tmp=\$TMPDIR\"; ls -d /tmp 2>&1; gecko version").stdout
        assertTrue(env, env.contains("prefix=/data/data/com.termux/files/usr"))
    }

    // Without the permission (Android kills the app when it is revoked, so
    // that check runs from adb: see android/termux-check.sh).

    @Test fun c_geckoMissingShowsTheInstaller() {
        val bin = "\$PREFIX/bin/gecko"
        assertEquals(0, termux("mv $bin $bin.away").exitCode)
        try {
            assertEquals(TermuxState.GeckoMissing, TermuxFlow.probed(Termux.run(ctx, TermuxFlow.PROBE, "gecko version", 20_000)))
            screen(start = false, wait = R.string.termux_gecko_missing_title, shot = "gecko-missing")
        } finally {
            termux("mv $bin.away $bin")
        }
    }

    @Test fun d_externalAppsOffShowsTheLine() {
        // Turned off from inside (the test can still run commands until then);
        // Termux reads the file again for each command.
        val props = "\$HOME/.termux/termux.properties"
        termux("sed -i 's/^allow-external-apps=true/allow-external-apps=false/' $props")
        var restored = false
        try {
            val r = Termux.run(ctx, TermuxFlow.PROBE, "gecko version", 20_000)
            assertEquals("$r", TermuxState.ExternalAppsOff, TermuxFlow.probed(r))
            screen(start = false, wait = R.string.termux_external_title, shot = "external-apps")
        } finally {
            // The GitHub build is debuggable: put it back as Termux's own user.
            shell("run-as com.termux sed -i s/^allow-external-apps=false/allow-external-apps=true/ /data/data/com.termux/files/home/.termux/termux.properties")
            restored = TermuxFlow.probed(Termux.run(ctx, TermuxFlow.PROBE, "gecko version", 20_000)) is TermuxState.Ready
        }
        assertTrue("allow-external-apps couldn't be turned on again", restored)
    }

    @Test fun e_startConnectAndWork() {
        TermuxActivity.stop(ctx)
        waitUntil(10) { !Termux.portOpen("127.0.0.1", TermuxFlow.PORT) }
        // The machine list shows Termux next to This phone.
        val list = ActivityScenario.launch<ConnectActivity>(Intent(ctx, ConnectActivity::class.java))
        assertNotNull("no Termux row", device.wait(Until.findObject(By.text(ctx.getString(R.string.termux))), 10_000))
        shot("list")
        list.close()
        // Tap-equivalent: the setup screen checks, starts gecko daemon in Termux and opens it.
        TermuxActivity.open(ctx, start = true)
        waitUntil(60) { Termux.machine(ctx) != null && Termux.listening(Termux.machine(ctx)) }
        val m = Termux.machine(ctx)!!
        assertEquals("http://127.0.0.1:${TermuxFlow.PORT}/", m.url)
        assertTrue(m.token.length >= 16)

        val intent = Intent(ctx, TerminalActivity::class.java).putExtra(TerminalActivity.EXTRA_MACHINE, Termux.ID).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        ActivityScenario.launch<TerminalActivity>(intent).use { sc ->
            waitFor(sc, "window.gecko&&gecko.conn.status==='online'&&gecko.firstSync===true", 60)
            js(sc, "gecko.newTab()")
            waitFor(sc, "gecko.activePane()&&gecko.activeSession()&&!gecko.activeSession().exited")
            val tools = args.getString("termuxTools") == "true"
            val cmd = "echo \$SHELL; echo shell=\$0; echo \$PREFIX; " + (if (tools) "git --version; tmux -V; node -v; tmux new -d -s gk 'sleep 600'; tmux ls; " else "") + "echo marker-\$((6*7))"
            js(sc, "gecko.sendInput(gecko.activeId(), ${org.json.JSONObject.quote("$cmd\r")})")
            waitFor(sc, "screen().includes('marker-42')", 30)
            val text = js(sc, "screen()")
            assertTrue(text, text.contains("/data/data/com.termux/files/usr"))
            assertTrue("not Termux's shell: $text", text.contains("/data/data/com.termux/files/usr/bin/bash"))
            assertTrue("not bash: $text", Regex("shell=\\S*bash").containsMatchIn(text))
            if (tools) {
                assertTrue(text, text.contains("git version"))
                assertTrue(text, Regex("tmux \\d").containsMatchIn(text))
                assertTrue(text, Regex("v\\d+\\.").containsMatchIn(text))
                assertTrue(text, text.contains("gk: 1 windows"))
                termux("tmux kill-session -t gk")
            }
            shot("tab")
        }
        // Stopping ends it and forgets the link.
        TermuxActivity.stop(ctx)
        waitUntil(10) { !Termux.portOpen("127.0.0.1", TermuxFlow.PORT) }
        assertEquals(null, Termux.machine(ctx))
    }

    /** Opens the setup screen and waits for a step's title. */
    private fun screen(start: Boolean, wait: Int, shot: String) {
        TermuxActivity.open(ctx, start)
        assertNotNull("no ${ctx.getString(wait)}", device.wait(Until.findObject(By.text(ctx.getString(wait))), 20_000))
        shot(shot)
        device.pressBack()
    }

    private fun shot(name: String) {
        if (args.getString("shots") != "true") return
        val dir = File(ctx.getExternalFilesDir(null), "termux").apply { mkdirs() }
        Thread.sleep(600)
        device.takeScreenshot(File(dir, "$name.png"))
    }

    private fun waitUntil(seconds: Int, cond: () -> Boolean) {
        val until = System.currentTimeMillis() + seconds * 1000L
        while (System.currentTimeMillis() < until) {
            if (cond()) return
            Thread.sleep(300)
        }
        throw AssertionError("timed out after $seconds s")
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
