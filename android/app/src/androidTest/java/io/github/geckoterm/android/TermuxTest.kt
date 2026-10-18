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

    /**
     * Gecko in Termux is one of This phone's machines: This phone runs with
     * a tab of its own, Termux is set up, and then it's in This phone's
     * sidebar; a tab opened on it from This phone's UI runs Termux's shell
     * and packages, its agent notifies through This phone, and This phone's
     * own tab is still there. Then: an older app's Termux machine moves over
     * without setup, This phone starts Gecko in Termux again when it ends,
     * and leaves it stopped when stopped from the menu.
     */
    @Test fun e_termuxIsAMachineOfThisPhone() {
        // From scratch: not set up, not running.
        TermuxActivity.stop(ctx)
        waitUntil(10) { !Termux.portOpen("127.0.0.1", TermuxFlow.PORT) }
        PhoneService.start(ctx)
        waitUntil(30) { LocalGecko.running(ctx) }
        PhoneTermux.forget(ctx, fromPhone = true)
        val me = phoneName()

        // This phone, with a tab of its own from before.
        val phone = Intent(ctx, TerminalActivity::class.java).putExtra(TerminalActivity.EXTRA_MACHINE, LocalGecko.ID).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        ActivityScenario.launch<TerminalActivity>(phone).use { sc ->
            waitFor(sc, "window.gecko&&gecko.conn.status==='online'&&gecko.firstSync===true", 60)
            if (js(sc, "[...gecko.sessions.values()].some(s=>s.name==='phone-old')") != "true") {
                js(sc, "gecko.conn.request({t:'create',create:{name:'phone-old',cols:80,rows:24}})")
            }
            waitFor(sc, "[...gecko.sessions.values()].some(s=>s.name==='phone-old')")
        }

        // The connect screen: Termux is a line under This phone, not a Gecko of its own.
        ActivityScenario.launch<ConnectActivity>(Intent(ctx, ConnectActivity::class.java)).use {
            assertNotNull("no Termux line", device.wait(Until.findObject(By.text(ctx.getString(R.string.termux_sub_offer))), 10_000))
            assertEquals(null, device.findObject(By.text(ctx.getString(R.string.termux))))
            shot("list-offer")
        }

        // Set up: the setup screen starts Gecko in Termux and adds it to This phone.
        TermuxActivity.open(ctx, start = true)
        waitUntil(90) { LocalGecko.hosts(ctx)?.contains(TermuxMachine.HOST) == true }
        val m = Termux.machine(ctx)!!
        assertEquals("http://127.0.0.1:${TermuxFlow.PORT}/", m.url)
        assertEquals("not in the machine list", null, Gecko.machines(ctx).get(Termux.ID))
        device.wait(Until.findObject(By.pkg(ctx.packageName)), 5_000)

        // An agent stand-in for Claude Code in Termux: it works, then asks.
        termux("echo ${android.util.Base64.encodeToString(FAKE_CLAUDE.toByteArray(), android.util.Base64.NO_WRAP)} | base64 -d > \$PREFIX/bin/claude-standin && chmod 755 \$PREFIX/bin/claude-standin")
        Gecko.prefs(ctx).edit().putBoolean(Gecko.BACKGROUND, true).apply()
        WatchService.start(ctx)

        ActivityScenario.launch<TerminalActivity>(phone).use { sc ->
            waitFor(sc, "window.gecko&&gecko.conn.status==='online'&&gecko.firstSync===true", 60)
            waitFor(sc, "gecko.hosts.some(h=>h.name==='termux'&&h.kind==='link'&&h.status==='connected')", 60)
            shot("sidebar")
            // A tab on Termux from This phone's UI, next to This phone's own.
            js(sc, "gecko.newTab({host:'termux',cwd:''})")
            waitFor(sc, "gecko.activeSession()&&gecko.activeSession().host==='termux'&&!gecko.activeSession().exited")
            val tools = args.getString("termuxTools") == "true"
            val cmd = "echo \$SHELL; echo shell=\$0; echo \$PREFIX; " + (if (tools) "git --version; node -v; " else "") + "echo marker-\$((6*7))"
            js(sc, "gecko.sendInput(gecko.activeId(), ${org.json.JSONObject.quote("$cmd\r")})")
            waitFor(sc, "screen().includes('marker-42')", 30)
            val text = js(sc, "screen()")
            assertTrue("not Termux's shell: $text", text.contains("/data/data/com.termux/files/usr/bin/bash"))
            if (tools) {
                assertTrue(text, text.contains("git version"))
                assertTrue(text, Regex("v\\d+\\.").containsMatchIn(text))
            }
            // This phone's own tab is still there, on This phone.
            assertEquals("true", js(sc, "[...gecko.sessions.values()].some(s=>s.name==='phone-old'&&s.host===${org.json.JSONObject.quote(me)})"))
            shot("termux-tab")
            js(sc, "gecko.sendInput(gecko.activeId(), 'clear; claude-standin\\r')")
            waitFor(sc, "screen().includes('stand-in')", 15)
            try {
                waitFor(sc, "gecko.activeSession().agent&&gecko.activeSession().agent.name==='claude'", 30)
            } catch (e: AssertionError) {
                throw AssertionError("no agent: ${js(sc, "JSON.stringify(gecko.activeSession())")}\n${js(sc, "screen()")}")
            }
        }
        // In the background, the agent's question arrives as a notification of This phone's.
        device.pressHome()
        val nm = ctx.getSystemService(android.app.NotificationManager::class.java)
        val asked = { nm.activeNotifications.any { (it.notification.extras.getCharSequence("android.title") ?: "").contains("needs you") } }
        waitUntil(60) { asked() }
        device.openNotification()
        device.wait(Until.findObject(By.textContains("needs you")), 5_000)
        shot("notification")
        device.pressBack()
        termux("pkill -f claude-standin; rm -f \$PREFIX/bin/claude-standin")

        // An older app saved Termux as a machine of its own: it moves over, no setup.
        LocalGecko.removeHost(ctx, TermuxMachine.HOST)
        PhoneTermux.forget(ctx, fromPhone = false)
        Gecko.machines(ctx).save(m.copy(lastUsed = 5))
        assertTrue(TermuxMachine.migrate(Gecko.machines(ctx), Gecko.termux(ctx)))
        assertEquals(null, PhoneTermux.sync(ctx))
        assertTrue(LocalGecko.hosts(ctx)!!.contains(TermuxMachine.HOST))
        assertEquals(null, Gecko.machines(ctx).get(Termux.ID))

        // Termux ended Gecko (or Termux was closed): This phone starts it again.
        assertEquals(0, termux("PATH=\$PATH:\$HOME/.local/bin; gecko stop").exitCode)
        waitUntil(15) { !Termux.portOpen("127.0.0.1", TermuxFlow.PORT) }
        waitUntil(60) { Termux.portOpen("127.0.0.1", TermuxFlow.PORT) }

        // Stopped from the menu: it stays stopped, and says so.
        TermuxActivity.stop(ctx)
        waitUntil(15) { !Termux.portOpen("127.0.0.1", TermuxFlow.PORT) }
        Thread.sleep(20_000)
        assertFalse(Termux.portOpen("127.0.0.1", TermuxFlow.PORT))
        assertEquals(TermuxMachine.Status.Paused, PhoneTermux.status(ctx))
        // Started again from the connect screen's line.
        TermuxActivity.open(ctx, start = true)
        waitUntil(60) { Termux.portOpen("127.0.0.1", TermuxFlow.PORT) && !PhoneTermux.paused(ctx) }
        ActivityScenario.launch<ConnectActivity>(Intent(ctx, ConnectActivity::class.java)).use {
            assertNotNull(device.wait(Until.findObject(By.text(ctx.getString(R.string.termux_sub_running))), 15_000))
            shot("list-running")
        }
    }

    /** This phone's machine name, as its Gecko says. */
    private fun phoneName(): String = LocalGecko.hosts(ctx)!!.first()

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

    companion object {
        /** Works for a few seconds (Claude Code's title), then asks a question with choices. */
        private val FAKE_CLAUDE = """#!/data/data/com.termux/files/usr/bin/sh
printf '\033]0;\342\234\263 Claude Code\007'
echo 'Claude Code (a stand-in)'
for i in 1 2 3 4 5 6 7 8; do printf '\033]0;\342\227\220 Fixing the flaky test\007'; printf '\r* Fixing the flaky test... (%ss)' "${'$'}i"; sleep 1; done
printf '\033]0;\342\234\263 Fix the flaky test\007'
printf '\r\033[K\nEdit file test/flaky.test.js\nDo you want to make this edit to flaky.test.js?\n\342\235\257 1. Yes\n  2. No\n'
read answer
echo "answered ${'$'}answer"
sleep 60
"""
    }
}
