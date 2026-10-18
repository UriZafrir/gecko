package io.github.geckoterm.android

import android.content.Intent
import android.os.SystemClock
import android.view.InputDevice
import android.view.KeyEvent
import androidx.test.core.app.ActivityScenario
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.uiautomator.By
import androidx.test.uiautomator.UiDevice
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assume.assumeTrue
import org.junit.Test
import org.junit.runner.RunWith
import java.io.File
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/**
 * The extra keys row and the symbols Claude Code draws, on a live Gecko:
 * - the symbols (-e glyphs: a script that prints them) shown, for a shot;
 * - Esc, Tab, Ctrl, Alt, the arrows and the menu wholly in view;
 * - Shift+Tab sends ESC [ Z from the menu, from holding Tab and from a
 *   hardware keyboard, while a tap on Tab still sends a tab;
 * - in a stand-in for Claude Code's prompt (-e mode: a script that cycles
 *   its mode line on ESC [ Z, as Claude Code does), each of those switches
 *   the mode.
 * Screenshots go to files/keybar/<name>-<step>.png, results to <name>.json.
 *
 * -e keybar <name> -e geckoUrl -e demoDir -e glyphs -e mode
 */
@RunWith(AndroidJUnit4::class)
class KeyBarTest {
    private val ctx get() = ApplicationProvider.getApplicationContext<android.content.Context>()
    private val device get() = UiDevice.getInstance(InstrumentationRegistry.getInstrumentation())
    private lateinit var sc: ActivityScenario<TerminalActivity>
    private var activity: TerminalActivity? = null

    @Test fun keyBar() {
        val args = InstrumentationRegistry.getArguments()
        val name = args.getString("keybar")
        assumeTrue("not a key bar run", !name.isNullOrEmpty())
        val out = File(ctx.getExternalFilesDir(null), "keybar").apply { mkdirs() }
        val link = LinkParser.parse(args.getString("geckoUrl")!!)
        val m = Machine(name = "keybar", url = link.baseUrl, token = link.token!!)
        Gecko.machines(ctx).save(m)
        val results = JSONObject()
        val failures = ArrayList<String>()
        fun check(step: String, ok: Boolean, what: String) {
            results.put(step, JSONObject().put("ok", ok).put("what", what))
            android.util.Log.i(TAG, "$name / $step: ${if (ok) "ok" else "FAILED"} $what")
            if (!ok) failures += "$step: $what"
        }
        fun shot(step: String) {
            device.takeScreenshot(File(out, "$name-$step.png"))
            val f = CountDownLatch(1); var cur = ""
            act().runOnUiThread { cur = "${act().currentFocus?.javaClass?.simpleName} web=${act().web.hasFocus()} windowFocus=${act().hasWindowFocus()}"; f.countDown() }
            f.await(5, TimeUnit.SECONDS)
            android.util.Log.i(TAG, "$name / focus at $step: $cur")
            Thread.sleep(200)
        }
        val intent = Intent(ctx, TerminalActivity::class.java).putExtra(TerminalActivity.EXTRA_MACHINE, m.id)
        try {
            ActivityScenario.launch<TerminalActivity>(intent).use { s ->
                sc = s
                waitFor("window.gecko&&gecko.conn.status==='online'&&gecko.firstSync===true", 60)
                val dir = args.getString("demoDir")!!

                // The symbols.
                val glyphTab = tab(dir)
                js("gecko.sendInput($glyphTab, '${args.getString("glyphs")}\\r')")
                waitFor("screen().includes('logo ')")
                js("document.activeElement.blur()")
                Thread.sleep(1500) // fonts fetched and drawn
                shot("01-glyphs")

                // What the page sends, as it sends it.
                js("window.__seen='';const c=gecko.conn;if(!c.__mo){c.__mo=c.input.bind(c);c.input=(id,d)=>{window.__seen+=typeof d==='string'?d:new TextDecoder().decode(d);return c.__mo(id,d)}}")
                val mode = tab(dir)
                js("gecko.sendInput($mode, '${args.getString("mode")}\\r')")
                waitFor("screen().includes('accept edits on')")
                js("document.activeElement.blur()")
                Thread.sleep(800)
                shot("02-mode-start")

                fun sent(): String = (org.json.JSONTokener(js("window.__seen||''")).nextValue() as String).also { js("window.__seen=''") }
                fun shown(text: String) = try { waitFor("screen().includes(${JSONObject.quote(text)})", 5); true } catch (e: AssertionError) { false }
                fun hex(s: String) = s.map { "%02x".format(it.code) }.joinToString(" ")

                // Every key that must be in view is, wholly, at a tappable size.
                val inView = keysInView()
                check("02-keys-in-view", inView.isEmpty(), inView.ifEmpty { "esc, tab, ctrl, alt, the arrows and the menu all in view" })

                // The menu's Shift+Tab.
                js("window.__seen=''")
                device.findObject(By.desc(ctx.getString(R.string.key_menu))).click()
                Thread.sleep(800)
                val item = device.findObject(By.text(java.util.regex.Pattern.compile("(?i).*shift\\+tab.*")))
                if (item == null) { check("03-shift-tab-menu", false, "no Shift+Tab in the menu"); device.pressBack() }
                else {
                    item.click()
                    Thread.sleep(800)
                    val got = sent()
                    check("03-shift-tab-menu", got == "\u001b[Z" && shown("plan mode on"), "sent [${hex(got)}], mode line shows plan mode: ${shown("plan mode on")}")
                }
                shot("03-shift-tab-menu")

                // Holding Tab.
                val tabKey = device.findObject(By.desc("Tab (hold for Shift+Tab)")) ?: device.findObject(By.text("tab"))
                tabKey.longClick()
                Thread.sleep(800)
                var got = sent()
                check("04-hold-tab", got == "\u001b[Z" && shown("auto mode on"), "sent [${hex(got)}], mode line shows auto mode: ${shown("auto mode on")}")
                shot("04-hold-tab")

                // A tap on Tab is still a tab (the mode stays).
                tabKey.click()
                Thread.sleep(800)
                got = sent()
                check("05-tap-tab", got == "\t" && shown("auto mode on"), "sent [${hex(got)}]")

                // A hardware keyboard's Shift+Tab, into the focused terminal.
                js("gecko.activePane().focus()")
                val focused = try { waitFor("document.activeElement&&document.activeElement.classList.contains('xterm-helper-textarea')", 10); true } catch (e: AssertionError) { false }
                Thread.sleep(800)
                val webFocus = act().web.hasFocus()
                js("window.__seen=''")
                hardKey(KeyEvent.KEYCODE_TAB, KeyEvent.META_SHIFT_ON or KeyEvent.META_SHIFT_LEFT_ON)
                Thread.sleep(800)
                got = sent()
                check("06-hardware-shift-tab", got == "\u001b[Z" && shown("manual mode on"), "sent [${hex(got)}], mode line shows manual mode: ${shown("manual mode on")} (terminal focused: $focused, web view focused: $webFocus)")
                hardKey(KeyEvent.KEYCODE_TAB, 0)
                Thread.sleep(600)
                got = sent()
                check("07-hardware-tab", got == "\t", "sent [${hex(got)}]")
                js("document.activeElement.blur()")
                Thread.sleep(800)
                shot("06-hardware-shift-tab")
                js("gecko.sendInput($mode, '\\u0003')")
            }
        } finally {
            File(out, "$name.json").writeText(results.toString(1))
        }
        assertEquals("key bar: ${failures.joinToString("; ")}", 0, failures.size)
    }

    /** The keys that should be wholly in view and aren't (or are too small), as text. */
    private fun keysInView(): String {
        val problems = ArrayList<String>()
        val done = CountDownLatch(1)
        act().runOnUiThread {
            try {
                val d = act().resources.displayMetrics.density
                for (name in DeviceMatrixTest.KEYS_IN_VIEW) {
                    val k = act().keys.findViewWithTag<android.view.View>(name)
                    val r = android.graphics.Rect()
                    // A hardware keyboard: only the menu shows (ExtraKeys.setHardwareKeyboard).
                    if (name != "menu" && (k?.parent?.parent as? android.view.View)?.visibility == android.view.View.INVISIBLE) continue
                    if (k == null || !k.getGlobalVisibleRect(r) || r.width() < k.width || r.height() < k.height) problems += "$name not wholly in view (${k?.let { "${it.width}x${it.height}, visible $r" }})"
                    else if (k.width < 39 * d - 1) problems += "$name only ${(k.width / d).toInt()}dp wide"
                }
                android.util.Log.i(TAG, "key widths: " + DeviceMatrixTest.KEYS_IN_VIEW.joinToString { n -> act().keys.findViewWithTag<android.view.View>(n)?.let { "$n ${"%.1f".format(it.width / d)}dp" } ?: "$n none" })
            } finally { done.countDown() }
        }
        done.await(5, TimeUnit.SECONDS)
        return problems.joinToString("; ")
    }

    /** A key from a hardware keyboard, down and up, with these modifiers. */
    private fun hardKey(code: Int, meta: Int) {
        val ui = InstrumentationRegistry.getInstrumentation().uiAutomation
        val t = SystemClock.uptimeMillis()
        val shift = meta and KeyEvent.META_SHIFT_ON != 0
        fun ev(action: Int, c: Int, m: Int) = KeyEvent(t, SystemClock.uptimeMillis(), action, c, 0, m, KeyEvent.KEYCODE_UNKNOWN, 0, 0, InputDevice.SOURCE_KEYBOARD)
        if (shift) ui.injectInputEvent(ev(KeyEvent.ACTION_DOWN, KeyEvent.KEYCODE_SHIFT_LEFT, meta), true)
        ui.injectInputEvent(ev(KeyEvent.ACTION_DOWN, code, meta), true)
        ui.injectInputEvent(ev(KeyEvent.ACTION_UP, code, meta), true)
        if (shift) ui.injectInputEvent(ev(KeyEvent.ACTION_UP, KeyEvent.KEYCODE_SHIFT_LEFT, 0), true)
    }

    private fun tab(cwd: String): String {
        val before = js("gecko.activeId()")
        js("gecko.newTab({cwd: '$cwd'})")
        waitFor("gecko.activeId()!==$before&&gecko.activePane()&&gecko.activePane().term.buffer.active.getLine(0).translateToString(true).trim().length>0")
        Thread.sleep(500)
        return js("gecko.activeId()")
    }

    private fun act(): TerminalActivity {
        activity?.let { if (!it.isDestroyed) return it }
        var x: TerminalActivity? = null
        sc.onActivity { x = it }
        activity = x
        return x!!
    }

    private val screenJs = "(function(){var t=gecko.activePane().term,b=t.buffer.active,o=[];for(var i=b.viewportY;i<b.viewportY+t.rows;i++){var l=b.getLine(i);if(l)o.push(l.translateToString(true))}return o.join('\\n')})()"

    // Not through sc.onActivity, which waits for an idle main thread.
    private fun js(script: String): String {
        val a = act()
        val latch = CountDownLatch(1)
        var out = ""
        a.runOnUiThread { a.web.evaluateJavascript(script.replace("screen()", screenJs)) { out = it ?: ""; latch.countDown() } }
        latch.await(10, TimeUnit.SECONDS)
        return out
    }

    private fun waitFor(cond: String, seconds: Int = 20) {
        val until = System.currentTimeMillis() + seconds * 1000L
        while (System.currentTimeMillis() < until) {
            if (js("!!($cond)") == "true") return
            Thread.sleep(250)
        }
        throw AssertionError("timed out waiting for $cond")
    }

    companion object {
        const val TAG = "KeyBar"
    }
}
