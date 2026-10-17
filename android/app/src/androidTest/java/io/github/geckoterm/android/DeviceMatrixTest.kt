package io.github.geckoterm.android

import android.content.Intent
import android.graphics.Rect
import androidx.test.core.app.ActivityScenario
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
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
 * The layout on this device, in each of its shapes: portrait, landscape,
 * folded and unfolded, half open, a smaller window, large text, dark, RTL.
 * One session with a long scrollback (scrolled back to a marker) and two
 * agents throughout; after each change it checks the page fits, the tab bar,
 * keys and terminal are in view and clear of cutouts, the sidebar is a
 * drawer or persistent as the width calls for, and the session and scroll
 * position survived. Screenshots go to files/matrix/<device>-<step>.png,
 * results to files/matrix/<device>.json.
 *
 * -e matrix <device name> -e steps portrait,landscape,... -e geckoUrl
 * -e demoDir -e working -e asking (as CompactDemo).
 */
@RunWith(AndroidJUnit4::class)
class DeviceMatrixTest {
    private val ctx get() = ApplicationProvider.getApplicationContext<android.content.Context>()
    private val device get() = UiDevice.getInstance(InstrumentationRegistry.getInstrumentation())
    private var activity: TerminalActivity? = null
    private lateinit var sc: ActivityScenario<TerminalActivity>

    @Test fun matrix() {
        val args = InstrumentationRegistry.getArguments()
        val name = args.getString("matrix")
        assumeTrue("not a matrix run", !name.isNullOrEmpty())
        val steps = (args.getString("steps") ?: "portrait,landscape").split(",").map { it.trim() }.filter { it.isNotEmpty() }
        val dir = args.getString("demoDir")!!
        val out = File(ctx.getExternalFilesDir(null), "matrix").apply { mkdirs() }
        val link = LinkParser.parse(args.getString("geckoUrl")!!)
        val m = Machine(name = "matrix", url = link.baseUrl, token = link.token!!)
        Gecko.machines(ctx).save(m)
        val results = JSONObject()
        val failures = ArrayList<String>()
        val states = deviceStates()
        sh("settings put system font_scale 1.0")
        val intent = Intent(ctx, TerminalActivity::class.java).putExtra(TerminalActivity.EXTRA_MACHINE, m.id)
        // A WebView older than the page needs (an emulator image that the
        // Play Store never updated): Gecko says so and offers the update.
        val webview = androidx.webkit.WebViewCompat.getCurrentWebViewPackage(ctx)?.versionName ?: ""
        if ((webview.substringBefore('.').toIntOrNull() ?: 999) < TerminalActivity.MIN_WEBVIEW) {
            ActivityScenario.launch<TerminalActivity>(intent).use {
                Thread.sleep(3000)
                device.takeScreenshot(File(out, "$name-01-old-webview.png"))
                val shown = device.hasObject(androidx.test.uiautomator.By.text(ctx.getString(R.string.webview_update)))
                val r = JSONObject().put("ok", shown).put("size", "WebView $webview")
                    .put("problems", if (shown) "" else "no update screen for WebView $webview")
                    .put("note", "WebView $webview is older than ${TerminalActivity.MIN_WEBVIEW}: Gecko shows how to update it")
                results.put("01-old-webview", r)
                android.util.Log.i(TAG, "$name / old-webview: ${if (shown) "ok" else "PROBLEMS"} (WebView $webview)")
                File(out, "$name.json").writeText(results.toString(1))
                assertEquals("update screen for WebView $webview", true, shown)
            }
            return
        }
        try {
            ActivityScenario.launch<TerminalActivity>(intent).use { s ->
                sc = s
                waitFor("window.gecko&&gecko.conn.status==='online'&&gecko.firstSync===true", 60)
                js("localStorage.removeItem('gecko.dockFolded')")
                val work = tab(dir)
                js("gecko.sendInput($work, '${args.getString("working")} 900 30\\r')")
                val ask = tab(dir)
                js("gecko.sendInput($ask, '${args.getString("asking")} 900\\r')")
                val shell = tab("$dir/src/payments")
                // A long scrollback with a marker in the middle, scrolled back to it.
                js("gecko.sendInput($shell, 'clear; for i in \$(seq 1 300); do echo \"line \$i of the scrollback\"; done; echo MARK-150 is line 150\\r')")
                waitFor("screen().includes('MARK-150')")
                waitFor("document.querySelectorAll('#dock .agent').length>=2", 40)
                js("document.activeElement.blur()")
                scrollTo("line 150 of the scrollback")
                Thread.sleep(800)
                for ((i, step) in steps.withIndex()) {
                    // Steps repeat (portrait, landscape, portrait): each its own shot.
                    val key = "%02d-%s".format(i + 1, step)
                    val r = JSONObject()
                    val applied = apply(step, states)
                    if (applied != null) {
                        r.put("skipped", applied)
                        results.put(key, r)
                        android.util.Log.i(TAG, "$name / $step: skipped ($applied)")
                        continue
                    }
                    Thread.sleep(2500)
                    val problems = check(shell, step).toMutableList()
                    device.takeScreenshot(File(out, "$name-$key.png"))
                    when (step) {
                        "keyboard" -> {
                            val seen = js("window.__seen||''").let { org.json.JSONTokener(it).nextValue() as String }
                            if (!seen.contains("echo") || !seen.contains("\u0001")) problems += "hardware keys didn't reach the shell (${seen.length} bytes)"
                            js("gecko.sendInput(gecko.activeId(), '\\u0005\\u0015')")
                        }
                        "drawer-swipe" -> {
                            if (js("document.body.classList.contains('drawer')") != "true") problems += "an edge swipe didn't open the drawer"
                            else device.pressBack()
                            Thread.sleep(800)
                            if (js("document.body.classList.contains('drawer')") == "true") problems += "Back didn't close the drawer"
                            if (act().isFinishing) problems += "Back left the app"
                        }
                        "mouse" -> {
                            if (mouseBefore == mouseAfter) problems += "the mouse wheel didn't scroll ($mouseBefore)"
                            if (!mouseTab) problems += "a mouse click didn't pick a tab"
                        }
                        "dark", "light" -> {
                            val dark = js("matchMedia('(prefers-color-scheme: dark)').matches") == "true"
                            if (dark != (step == "dark")) problems += "page still ${if (dark) "dark" else "light"}"
                        }
                    }
                    r.put("ok", problems.isEmpty())
                    r.put("problems", problems.joinToString("; "))
                    r.put("size", js("innerWidth+'x'+innerHeight+' css px, '+gecko.activePane().term.cols+'x'+gecko.activePane().term.rows").trim('"'))
                    r.put("recreated", recreated)
                    if (note.isNotEmpty()) { r.put("note", note); note = "" }
                    results.put(key, r)
                    android.util.Log.i(TAG, "$name / $step: ${if (problems.isEmpty()) "ok" else "PROBLEMS " + problems}  (${r.getString("size")})")
                    if (problems.isNotEmpty()) failures += "$step: ${problems.joinToString("; ")}"
                }
                for (id in listOf(work, ask)) js("gecko.sendInput($id, '\\u0003')")
            }
        } finally {
            restore(states)
            File(out, "$name.json").writeText(results.toString(1))
        }
        assertEquals("layout on $name", "", failures.joinToString("\n"))
    }

    /** Puts the device in this shape; null when done, else why it can't. */
    private fun apply(step: String, states: Map<String, Int>): String? {
        when (step) {
            "portrait" -> device.setOrientationPortrait()
            "landscape" -> device.setOrientationLandscape()
            "folded", "unfolded", "half" -> {
                val id = states.entries.firstOrNull { (n, _) ->
                    !n.contains("REAR") && !n.contains("CONCURRENT") && when (step) {
                        "folded" -> n.contains("CLOSE") || n == "FOLDED"
                        "half" -> n.contains("HALF")
                        else -> (n.contains("OPEN") || n == "UNFOLDED") && !n.contains("HALF")
                    }
                }?.value ?: return "no $step device state"
                sh("cmd device_state state $id")
                if (step == "folded") sh("input keyevent WAKEUP")
            }
            "font-1.3" -> sh("settings put system font_scale 1.3")
            "font-2.0" -> sh("settings put system font_scale 2.0")
            "font-1.0" -> sh("settings put system font_scale 1.0")
            "dark" -> sh("cmd uimode night yes")
            "rtl" -> sh("settings put global debug.force_rtl 1")
            "ltr" -> sh("settings put global debug.force_rtl 0")
            "nav-3", "nav-gesture" -> {
                // A system overlay: Android restarts the activity for it (no
                // configChanges covers that), so the page loads again, at the
                // bottom of the terminal. Back to the marker for the checks.
                val was = recreated
                sh("cmd overlay enable-exclusive --category com.android.internal.systemui.navbar." + if (step == "nav-3") "threebutton" else "gestural")
                Thread.sleep(4000)
                act()
                waitFor("window.gecko&&gecko.conn.status==='online'&&gecko.firstSync===true&&gecko.activePane()&&screen().length>0", 60)
                waitFor("screen().includes('MARK-150')||true")
                Thread.sleep(1500)
                if (recreated != was) { note = "the page loaded again (Android restarts apps for a navigation mode change)"; scrollTo("line 150 of the scrollback") }
            }
            "drawer-swipe" -> {
                // In from the left edge, mid-height: the drawer, not Back.
                if (js("innerWidth>760&&innerHeight>500") == "true") return "a persistent sidebar here"
                js("document.activeElement.blur()")
                val y = device.displayHeight / 2
                device.swipe(1, y, device.displayWidth / 2, y, 20)
                Thread.sleep(1200)
            }
            "light" -> sh("cmd uimode night no")
            "split-side", "split-top", "window" -> {
                // A smaller window, as in split screen or a desktop window.
                val tid = taskId() ?: return "no task id"
                val (w, h) = device.displayWidth to device.displayHeight
                val bounds = when (step) {
                    "split-side" -> Rect(0, 0, w / 2, h)
                    "split-top" -> Rect(0, 0, w, h / 2)
                    else -> Rect(w / 8, h / 8, w * 7 / 8, h * 6 / 8)
                }
                // 6: multi-window (split screen), 5: freeform (a desktop window).
                val before = js("innerWidth+'x'+innerHeight")
                sh("am start --windowingMode ${if (step == "window") 5 else 6} -n $PKG/.TerminalActivity")
                Thread.sleep(1200)
                val res = sh("cmd activity task resize $tid ${bounds.left} ${bounds.top} ${bounds.right} ${bounds.bottom}")
                if (res.contains("rror") || res.contains("nknown")) return "resize: $res"
                Thread.sleep(1500)
                if (js("innerWidth+'x'+innerHeight") == before) return "the window didn't change size ($before)"
            }
            "fullscreen" -> {
                // The whole screen first, while still resizable, then full screen.
                taskId()?.let { sh("cmd activity task resize $it 0 0 ${device.displayWidth} ${device.displayHeight}") }
                Thread.sleep(800)
                sh("am start --windowingMode 1 -n $PKG/.TerminalActivity")
                Thread.sleep(1500)
            }
            "mouse" -> {
                // A mouse or trackpad: the wheel scrolls the scrollback, a click picks a tab.
                val web = device.findObject(androidx.test.uiautomator.UiSelector().className("android.webkit.WebView")).bounds
                val d = ctx.resources.displayMetrics.density
                mouseBefore = js("gecko.activePane().term.buffer.active.viewportY")
                sh("input mouse scroll ${web.centerX()} ${web.centerY()} --axis VSCROLL,4")
                Thread.sleep(800)
                mouseAfter = js("gecko.activePane().term.buffer.active.viewportY")
                val tab = js("(()=>{const r=document.querySelector('#tabs .tab').getBoundingClientRect();return Math.round(r.left+r.width/2)+','+Math.round(r.top+r.height/2)})()").trim('"').split(",").map { it.toInt() }
                val shellId = js("gecko.activeId()")
                sh("input mouse tap ${web.left + (tab[0] * d).toInt()} ${web.top + (tab[1] * d).toInt()}")
                Thread.sleep(800)
                mouseTab = js("gecko.activeId()") != shellId
                js("gecko.focusSession($shellId)")
                // The wheel's scrolling coasts a little: let it end, then back
                // to the marker for the layout checks.
                Thread.sleep(1500)
                scrollTo("line 150 of the scrollback")
                Thread.sleep(1000)
            }
            "keyboard" -> {
                // A hardware keyboard's typing, with the terminal focused.
                val web = device.findObject(androidx.test.uiautomator.UiSelector().className("android.webkit.WebView")).bounds
                device.click(web.centerX(), web.centerY())
                Thread.sleep(800)
                js("gecko.activePane().focus()")
                js("window.__seen='';const c=gecko.conn;if(!c.__mo){c.__mo=c.input.bind(c);c.input=(id,d)=>{window.__seen+=typeof d==='string'?d:new TextDecoder().decode(d);return c.__mo(id,d)}}")
                sh("input keyboard text echo")
                sh("input keycombination KEYCODE_CTRL_LEFT KEYCODE_A")
                Thread.sleep(500)
            }
            else -> return "unknown step"
        }
        return null
    }

    private fun restore(states: Map<String, Int>) {
        try { device.unfreezeRotation() } catch (_: Exception) {}
        sh("settings put system font_scale 1.0")
        sh("cmd uimode night no")
        sh("settings put global debug.force_rtl 0")
        if (states.isNotEmpty()) sh("cmd device_state state reset")
    }

    /** What's wrong with the layout now. */
    private fun check(shell: String, step: String): List<String> {
        val p = ArrayList<String>()
        val raw = js("""(()=>{
            const v={w:innerWidth,h:innerHeight};
            const r=(s)=>{const e=document.querySelector(s);if(!e)return null;const b=e.getBoundingClientRect();const cs=getComputedStyle(e);return {l:b.left,t:b.top,r:b.right,b:b.bottom,w:b.width,h:b.height,shown:cs.display!=='none'&&cs.visibility!=='hidden'&&b.width>0&&b.height>0}};
            const pane=gecko.activePane();const t=pane.term;const pr=pane.el.getBoundingClientRect();
            const screen=t.element.querySelector('.xterm-screen').getBoundingClientRect();
            const b=t.buffer.active;const rows=[];for(let i=b.viewportY;i<b.viewportY+t.rows;i++){const l=b.getLine(i);rows.push(l?l.translateToString(true):'')}
            return JSON.stringify({v,sw:document.documentElement.scrollWidth,bw:document.body.scrollWidth,
              tabbar:r('#tabbar'),side:r('#side'),dock:r('#dock'),main:r('#main'),
              pane:{l:pr.left,r:pr.right,t:pr.top,b:pr.bottom},screen:{l:screen.left,r:screen.right,t:screen.top,b:screen.bottom},
              cols:t.cols,rows:t.rows,active:gecko.activeId(),scrolled:b.viewportY<b.baseY,
              marker:rows.some(x=>x.includes('line 150 of the')),
              drawer:document.body.classList.contains('drawer'),touch:gecko.touch,
              rtl:getComputedStyle(document.documentElement).direction})
        })()""")
        val o = JSONObject(org.json.JSONTokener(raw).nextValue() as String)
        val v = o.getJSONObject("v")
        val w = v.getDouble("w"); val h = v.getDouble("h")
        if (o.getDouble("sw") > w + 1) p += "page scrolls sideways (${o.getDouble("sw")} > $w)"
        val tabbar = o.optJSONObject("tabbar")
        if (tabbar == null || !tabbar.getBoolean("shown") || tabbar.getDouble("t") < -1 || tabbar.getDouble("r") > w + 1) p += "tab bar not in view"
        val pane = o.getJSONObject("pane"); val scr = o.getJSONObject("screen")
        if (scr.getDouble("r") > pane.getDouble("r") + 2 || scr.getDouble("r") > w + 1) p += "terminal wider than its pane (${scr.getDouble("r")} > ${pane.getDouble("r")})"
        if (scr.getDouble("b") > h + 1) p += "terminal below the window"
        if (!o.getBoolean("touch")) p += "touch handling off (the WebView reports no coarse pointer)"
        if (o.getInt("rows") < 5) p += "only ${o.getInt("rows")} rows"
        if (o.getInt("cols") < 20) p += "only ${o.getInt("cols")} columns"
        if (o.getString("active").trim('"') != shell.trim('"')) p += "a different tab is active"
        if (step != "keyboard" && (!o.getBoolean("scrolled") || !o.getBoolean("marker"))) p += "scroll position lost (scrolled ${o.getBoolean("scrolled")}, marker in view ${o.getBoolean("marker")})"
        val side = o.optJSONObject("side")
        val wide = w >= 761 && h >= 501
        if (wide && (side == null || !side.getBoolean("shown") || side.getDouble("r") <= 1)) p += "no sidebar at $w px"
        if (!wide && side != null && side.getDouble("r") > 1 && !o.getBoolean("drawer")) p += "drawer open at $w px"
        val dock = o.optJSONObject("dock")
        if (dock != null && dock.getBoolean("shown") && dock.getDouble("b") > h + 1) p += "agent dock below the window"
        // Native: keys and web view clear of cutouts, inside the window.
        val a = act()
        val native = CountDownLatch(1)
        a.runOnUiThread {
            try {
                val webR = Rect(); a.web.getGlobalVisibleRect(webR)
                val keys = a.findViewById<android.view.View>(R.id.keys)
                val keysR = Rect(); keys.getGlobalVisibleRect(keysR)
                val cut = a.window.decorView.rootWindowInsets?.displayCutout?.boundingRects ?: emptyList()
                // Cutouts in screen coordinates vs the views' (window) ones.
                val loc = IntArray(2); a.window.decorView.getLocationOnScreen(loc)
                for (c in cut) {
                    val cw = Rect(c); cw.offset(-loc[0], -loc[1])
                    if (Rect.intersects(cw, webR)) p += "page under a cutout $c"
                    if (keys.isShown && Rect.intersects(cw, keysR)) p += "keys under a cutout $c"
                }
                val d = a.resources.displayMetrics.density
                if (keys.isShown && keysR.height() > 90 * d) p += "key row ${(keysR.height() / d).toInt()}dp tall"
                if (keys.isShown && keysR.width() < a.window.decorView.width / 2) p += "key row ${(keysR.width() / d).toInt()}dp wide"
            } finally { native.countDown() }
        }
        native.await(5, TimeUnit.SECONDS)
        return p
    }

    /** Scrolls the terminal back so this line is in view. */
    private fun scrollTo(text: String) {
        js("""(()=>{const t=gecko.activePane().term,b=t.buffer.active;for(let i=0;i<b.length;i++){const l=b.getLine(i);if(l&&l.translateToString(true).includes(${JSONObject.quote(text)})){t.scrollToLine(Math.max(0,i-3));return i}}return -1})()""")
    }

    private fun deviceStates(): Map<String, Int> {
        val out = sh("cmd device_state print-states")
        return Regex("identifier=(\\d+), name='([^']+)'").findAll(out).associate { it.groupValues[2] to it.groupValues[1].toInt() }
    }

    private fun taskId(): Int? = act().taskId.takeIf { it > 0 }

    private fun sh(cmd: String): String {
        val pfd = InstrumentationRegistry.getInstrumentation().uiAutomation.executeShellCommand(cmd)
        return android.os.ParcelFileDescriptor.AutoCloseInputStream(pfd).use { it.readBytes().decodeToString().trim() }
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
        // A new one (the system recreated it): note it, the session should still be there.
        if (activity != null) recreated++
        var x: TerminalActivity? = null
        sc.onActivity { x = it }
        activity = x
        return x!!
    }
    private var recreated = 0
    private var note = ""
    private var mouseBefore = ""
    private var mouseAfter = ""
    private var mouseTab = false

    private val screenJs = "(function(){var t=gecko.activePane().term,b=t.buffer.active,o=[];for(var i=b.viewportY;i<b.viewportY+t.rows;i++){var l=b.getLine(i);if(l)o.push(l.translateToString(true))}return o.join('\\n')})()"

    // Not through sc.onActivity, which waits for an idle main thread (never, while a logo fades).
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
        const val TAG = "DeviceMatrix"
        const val PKG = "io.github.geckoterm.android"
    }
}
