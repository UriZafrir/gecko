package io.github.geckoterm.android

import android.content.Intent
import android.os.Handler
import android.os.Looper
import android.view.KeyEvent
import android.view.inputmethod.InputConnection
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
 * Voice typing the way Gboard does it, with the debug build's ScriptIme as
 * the keyboard (growing composing text, then a
 * commit; going back to replace a word; auto-spaces; spoken punctuation;
 * corrections after the commit; emoji; Enter or an extra key mid-way),
 * through the web view's own InputConnection, into a terminal and into the
 * compose box. Checks the bytes that go to the tab: the line they make,
 * nothing doubled or lost, no stray control bytes, Enter only at the end.
 * Needs geckoUrl.
 */
@RunWith(AndroidJUnit4::class)
class DictationTest {
    private var activity: TerminalActivity? = null
    private lateinit var sc: ActivityScenario<TerminalActivity>

    /** One dictation and the line it should leave. */
    class Case(
        val name: String,
        val expect: String,
        val enterAtEnd: Boolean = false,
        /** Exactly these bytes, in a terminal only (an extra key goes to the terminal). */
        val raw: String? = null,
        val run: Ime.() -> Unit,
    )

    val cases = listOf(
        Case("partials then commit", "hello world this is a test") {
            for (p in listOf("hello", "hello world", "hello world this", "hello world this is", "hello world this is a test")) compose(p)
            commit("hello world this is a test")
        },
        Case("partials then finish", "fix the login bug") {
            for (p in listOf("fix", "fix the", "fix the log", "fix the login bug")) compose(p)
            finish()
        },
        Case("a word replaced while composing", "I want to buy milk") {
            compose("I want to by")
            compose("I want to buy")
            compose("I want to buy milk")
            commit("I want to buy milk")
        },
        Case("auto-space between phrases, auto-capital", "Hello there. How are you") {
            compose("hello there"); commit("Hello there")
            compose("."); commit(".")
            compose("how"); compose("how are you"); commit(" How are you")
        },
        Case("spoken comma replaces its space", "first, second") {
            compose("first"); commit("first")
            commit(" ")
            delete(1); commit(",")
            commit(" second")
        },
        Case("a word corrected after the commit", "meet me there") {
            compose("meet me their"); commit("meet me their")
            delete(5); commit("there")
        },
        Case("an earlier word recomposed", "send the report today") {
            commit("send the repot today")
            region(9, 14); compose("report"); finish()
        },
        Case("emoji", "looks good 👍") {
            compose("looks good"); commit("looks good")
            commit(" 👍")
        },
        Case("long dictation", LONG) {
            var done = ""
            for (sentence in LONG.split(". ")) {
                val s = if (sentence.endsWith(".")) sentence else "$sentence."
                val words = s.split(" ")
                for (i in 1..words.size) compose(words.take(i).joinToString(" "))
                commit(if (done.isEmpty()) s else " $s")
                done += s
            }
        },
        Case("Enter while composing", "run the tests", enterAtEnd = true) {
            compose("run the"); compose("run the tests")
            enter()
        },
        Case("an extra key while composing", "", raw = "abc def\u001b[Dx") {
            compose("abc"); compose("abc def")
            key("Left") // the native ← key: what was said is the shell's, then the arrow
            finish()
            compose("x"); commit("x")
        },
        // Samsung Keyboard: a word composes, a space commits it on its own,
        // backspace shrinks the composing word, a word left behind is taken
        // back into composing, and it autocorrects and adds ". " itself.
        Case("Samsung: word by word, space on its own", "hello world") {
            compose("hel"); compose("hello"); finish(); commit(" ")
            compose("wor"); compose("world"); finish()
        },
        Case("Samsung: backspace while composing", "hello") {
            compose("helo"); compose("hel"); compose("hell"); compose("hello"); finish()
        },
        Case("Samsung: back into a committed word", "help") {
            compose("hello"); finish(); commit(" ")
            delete(1); region(0, 5); compose("hell"); compose("help"); finish()
        },
        Case("Samsung: autocorrect on space", "the end") {
            compose("teh"); commit("the "); compose("end"); finish()
        },
        Case("Samsung: double space for a period", "done. Next") {
            compose("done"); finish(); commit(" ")
            delete(1); commit(". ")
            compose("Next"); finish()
        },
        Case("Samsung: backspace key on a committed line", "ab") {
            commit("abc"); finish()
            del()
        },
        Case("Samsung: voice input replaces its phrase", "Turn on the lights.") {
            compose("turn on the"); compose("turn on the lights"); commit("Turn on the lights.")
        },
    )

    @Test fun intoTheTerminal() = drive(compose = false)

    @Test fun intoTheComposeBox() = drive(compose = true)

    /**
     * For the video (-e demo dictation, -e geckoUrl, -e demoDir): voice
     * typing at a person's pace into a shell, then into the compose box.
     */
    @Test fun demo() {
        val args = InstrumentationRegistry.getArguments()
        assumeTrue(args.getString("demo") == "dictation")
        val link = LinkParser.parse(args.getString("geckoUrl")!!)
        val m = Machine(name = "demo", url = link.baseUrl, token = link.token!!)
        Gecko.machines(ctx).save(m)
        val intent = Intent(ctx, TerminalActivity::class.java).putExtra(TerminalActivity.EXTRA_MACHINE, m.id)
        ActivityScenario.launch<TerminalActivity>(intent).use { s ->
            sc = s
            waitFor("window.gecko&&gecko.conn.status==='online'&&gecko.firstSync===true", 60)
            val before = js("gecko.activeId()")
            js("gecko.newTab({cwd: ${org.json.JSONObject.quote(args.getString("demoDir"))}})")
            waitFor("gecko.activeId()!==$before&&gecko.activePane()&&gecko.activePane().term.buffer.active.getLine(0).translateToString(true).trim().length>0")
            js("gecko.sendInput(gecko.activeId(), 'clear\\r')")
            Thread.sleep(1500)
            tapTerminal()
            Thread.sleep(1000)
            // Said word by word, one word heard wrong and fixed, then Enter.
            Ime(connect(), 450).run {
                for (p in listOf("echo", "echo the", "echo the payments", "echo the payments test", "echo the payments test pases")) compose(p)
                compose("echo the payments test passes"); commit("echo the payments test passes")
                commit(" now"); delete(4); commit(", now")
                Thread.sleep(800)
                enter()
            }
            Thread.sleep(2500)
            // The compose box: Enter while it's still listening sends it all.
            js("gecko.toggleCompose(true)")
            waitFor("document.activeElement===document.querySelector('#compose textarea')")
            Thread.sleep(800)
            val geo = js("(()=>{const r=document.querySelector('#compose textarea').getBoundingClientRect(),d=devicePixelRatio;return [(r.left+r.width/2)*d,(r.top+r.height/2)*d].join(',')})()").trim('"').split(",").map { it.toDouble() }
            val dev = androidx.test.uiautomator.UiDevice.getInstance(InstrumentationRegistry.getInstrumentation())
            val web = dev.findObject(androidx.test.uiautomator.UiSelector().className("android.webkit.WebView")).bounds
            val starts = ScriptIme.started
            dev.click(web.left + geo[0].toInt(), web.top + geo[1].toInt())
            val until = System.currentTimeMillis() + 2000
            while (ScriptIme.started == starts && System.currentTimeMillis() < until) Thread.sleep(50)
            Thread.sleep(800)
            Ime(connect(), 450).run {
                for (p in listOf("echo", "echo sent", "echo sent from", "echo sent from the", "echo sent from the compose", "echo sent from the compose box")) compose(p)
                Thread.sleep(600)
                enter()
            }
            Thread.sleep(3000)
            js("gecko.toggleCompose(false)")
            Thread.sleep(1500)
        }
    }

    /**
     * With the phone's own keyboard (Gboard on the emulator) up: what
     * `input text` types reaches the terminal once, in order.
     */
    @Test fun inputTextWithGboard() {
        val url = InstrumentationRegistry.getArguments().getString("geckoUrl")
        assumeTrue("no geckoUrl argument", !url.isNullOrEmpty())
        assumeTrue("no keyboard before the test", was.isNotEmpty() && was != "null")
        val link = LinkParser.parse(url!!)
        val m = Machine(name = "dictation", url = link.baseUrl, token = link.token!!)
        Gecko.machines(ctx).save(m)
        sh("ime set $was")
        try {
            val intent = Intent(ctx, TerminalActivity::class.java).putExtra(TerminalActivity.EXTRA_MACHINE, m.id)
            ActivityScenario.launch<TerminalActivity>(intent).use { s ->
                sc = s
                waitFor("window.gecko&&gecko.conn.status==='online'&&gecko.firstSync===true", 60)
                waitFor("gecko.activePane()&&gecko.activePane().term.textarea")
                js("(()=>{window.__sent=[];const c=gecko.conn;if(!c.__o){c.__o=c.input.bind(c);c.input=(id,d)=>{__sent.push(typeof d==='string'?d:new TextDecoder().decode(d));return c.__o(id,d)}}})()")
                val dev = androidx.test.uiautomator.UiDevice.getInstance(InstrumentationRegistry.getInstrumentation())
                val web = dev.findObject(androidx.test.uiautomator.UiSelector().className("android.webkit.WebView")).bounds
                dev.click(web.centerX(), web.centerY())
                Thread.sleep(1500)
                // executeShellCommand doesn't strip quotes: %s is a space.
                js("__sent.length=0")
                sh("input text echo%sone%stwo")
                Thread.sleep(1500)
                val sent = js("JSON.stringify(__sent.join(''))").let { org.json.JSONTokener(it).nextValue() as String }.let { org.json.JSONTokener(it).nextValue() as String }
                android.util.Log.i(TAG, "input text with $was: sent ${show(sent)}")
                js("gecko.sendInput(gecko.activeId(), '\\u0015')")
                assertEquals("echo one two", sent)
            }
        } finally {
            sh("ime set ${component.flattenToShortString()}")
        }
    }

    /**
     * The same into real Claude Code (-e claudeUrl <link to a Gecko>,
     * -e claudeDir <a folder there>): what its input box shows after each
     * dictation, and one prompt sent with Enter mid-phrase.
     */
    @Test fun intoClaudeCode() {
        val args = InstrumentationRegistry.getArguments()
        val url = args.getString("claudeUrl")
        assumeTrue("no claudeUrl argument", !url.isNullOrEmpty())
        val link = LinkParser.parse(url!!)
        val m = Machine(name = "claude", url = link.baseUrl, token = link.token!!)
        Gecko.machines(ctx).save(m)
        val intent = Intent(ctx, TerminalActivity::class.java).putExtra(TerminalActivity.EXTRA_MACHINE, m.id)
        ActivityScenario.launch<TerminalActivity>(intent).use { s ->
            sc = s
            waitFor("window.gecko&&gecko.conn.status==='online'&&gecko.firstSync===true", 60)
            val before = js("gecko.activeId()")
            js("gecko.newTab({cwd: ${org.json.JSONObject.quote(args.getString("claudeDir"))}})")
            waitFor("gecko.activeId()!==$before&&gecko.activePane()&&gecko.activePane().term.buffer.active.getLine(0).translateToString(true).trim().length>0")
            Thread.sleep(800)
            // Plan mode: Claude only reads and answers, whatever gets sent.
            js("gecko.sendInput(gecko.activeId(), 'claude --permission-mode plan\\r')")
            waitFor("gecko.activeSession().agent&&gecko.activeSession().agent.status==='idle'", 60)
            Thread.sleep(2500)
            js("(()=>{window.__sent=[];const c=gecko.conn;if(!c.__o){c.__o=c.input.bind(c);c.input=(id,d)=>{__sent.push(typeof d==='string'?d:new TextDecoder().decode(d));return c.__o(id,d)}}})()")
            // Ready for typing: a letter shows in its box.
            val ready = System.currentTimeMillis() + 60_000
            while (System.currentTimeMillis() < ready) {
                js("gecko.sendInput(gecko.activeId(), 'q')"); Thread.sleep(700)
                val got = Regex("[>❯] q").containsMatchIn(js("screen()"))
                js("gecko.sendInput(gecko.activeId(), '\\u007f')"); Thread.sleep(300)
                if (got) break
            }
            val failures = ArrayList<String>()
            val claudeCases = cases.filter { !it.enterAtEnd }.map { c ->
                if (c.raw != null) Case(c.name, "abc dexf", run = c.run) else c
            } + Case("Enter while composing", "reply with just the word ok", enterAtEnd = true) {
                compose("reply with"); compose("reply with just the word ok")
                enter()
            }
            tapTerminal()
            for (c in claudeCases) {
                val starts = ScriptIme.started
                js("gecko.activePane().focus()")
                val a = act()
                a.runOnUiThread {
                    val imm = a.getSystemService(android.view.inputmethod.InputMethodManager::class.java)
                    imm.restartInput(a.web)
                    imm.showSoftInput(a.web, 0)
                }
                val until = System.currentTimeMillis() + 2000
                while (ScriptIme.started == starts && System.currentTimeMillis() < until) Thread.sleep(50)
                Thread.sleep(300)
                js("__sent.length=0")
                Ime(connect()).run(c.run)
                // The test keyboard's first connection after Claude starts can
                // be one the page no longer listens to: once more, freshly.
                for (again in 1..3) {
                    if (js("__sent.length") != "0") break
                    android.util.Log.i(TAG, "claude / ${c.name}: nothing reached the page (focus " + js("document.activeElement.tagName+'.'+document.activeElement.className+' ta='+(document.activeElement===gecko.activePane().term.textarea)+' val='+JSON.stringify(gecko.activePane().term.textarea.value)") + ", starts ${ScriptIme.started}), again")
                    a.runOnUiThread { a.getSystemService(android.view.inputmethod.InputMethodManager::class.java).restartInput(a.web) }
                    Thread.sleep(800)
                    Ime(connect()).run(c.run)
                }
                Thread.sleep(1500)
                val shown = js("screen()").let { org.json.JSONTokener(it).nextValue() as String }
                // Claude's box wraps long input: compare without line breaks and the box's edges.
                val flat = shown.replace(Regex("[│╭╮╰╯─]"), " ").replace(Regex("\\s+"), " ")
                val typed = Regex("[>❯] " + Regex.escape(c.expect)).containsMatchIn(flat)
                val ok = typed && !flat.contains("${c.expect}${c.expect.take(3)}")
                val sent = js("JSON.stringify(__sent.join(''))").let { org.json.JSONTokener(it).nextValue() as String }.let { org.json.JSONTokener(it).nextValue() as String }
                android.util.Log.i(TAG, "claude / ${c.name}: ${if (ok) "ok" else "WRONG"} sent ${show(sent)}: ${flat.takeLast(200)}")
                if (!ok) failures += "${c.name}: screen ends ${flat.takeLast(300)}"
                if (c.enterAtEnd) {
                    // Its answer, then on.
                    val done = System.currentTimeMillis() + 120_000
                    Thread.sleep(3000)
                    while (System.currentTimeMillis() < done && js("gecko.activeSession().agent.status") != "\"idle\"") Thread.sleep(500)
                    android.util.Log.i(TAG, "claude answered: " + js("screen()").split("reply with just the word ok").last().replace("\\n", " | ").take(200))
                } else {
                    // Empty the box: a backspace for each character, and a few more.
                    js("gecko.sendInput(gecko.activeId(), '\\u007f'.repeat(${c.expect.length + 8}))")
                    Thread.sleep(600)
                }
            }
            // Leave Claude.
            js("gecko.sendInput(gecko.activeId(), '\\u0003')"); Thread.sleep(500); js("gecko.sendInput(gecko.activeId(), '\\u0003')")
            Thread.sleep(1000)
            js("gecko.sendInput(gecko.activeId(), 'exit\\r')")
            assertEquals("dictation into Claude Code", "", failures.joinToString("\n"))
        }
    }

    private fun drive(compose: Boolean) {
        val url = InstrumentationRegistry.getArguments().getString("geckoUrl")
        assumeTrue("no geckoUrl argument", !url.isNullOrEmpty())
        val link = LinkParser.parse(url!!)
        val m = Machine(name = "dictation", url = link.baseUrl, token = link.token!!)
        Gecko.machines(ctx).save(m)
        val intent = Intent(ctx, TerminalActivity::class.java).putExtra(TerminalActivity.EXTRA_MACHINE, m.id)
        ActivityScenario.launch<TerminalActivity>(intent).use { s ->
            sc = s
            waitFor("window.gecko&&gecko.conn.status==='online'&&gecko.firstSync===true", 60)
            val before = js("gecko.activeId()")
            js("gecko.newTab()")
            waitFor("gecko.activeId()!==$before&&gecko.activePane()&&gecko.activePane().term.buffer.active.getLine(0).translateToString(true).trim().length>0")
            Thread.sleep(800)
            // What goes to the tab, byte for byte.
            js("(()=>{window.__sent=[];const c=gecko.conn;if(!c.__o){c.__o=c.input.bind(c);c.input=(id,d)=>{__sent.push(typeof d==='string'?d:new TextDecoder().decode(d));return c.__o(id,d)}}})()")
            if (!compose) tapTerminal()
            val failures = ArrayList<String>()
            for (c in cases) {
                if (compose && c.raw != null) continue
                val starts = ScriptIme.started
                if (compose) {
                    js("gecko.toggleCompose(true)")
                    waitFor("document.activeElement===document.querySelector('#compose textarea')")
                    // A tap on it, as a person would.
                    val geo = js("(()=>{const r=document.querySelector('#compose textarea').getBoundingClientRect(),d=devicePixelRatio;return [(r.left+r.width/2)*d,(r.top+r.height/2)*d].join(',')})()").trim('"').split(",").map { it.toDouble() }
                    val web = androidx.test.uiautomator.UiDevice.getInstance(InstrumentationRegistry.getInstrumentation())
                        .findObject(androidx.test.uiautomator.UiSelector().className("android.webkit.WebView")).bounds
                    androidx.test.uiautomator.UiDevice.getInstance(InstrumentationRegistry.getInstrumentation()).click(web.left + geo[0].toInt(), web.top + geo[1].toInt())
                } else {
                    js("gecko.toggleCompose(false);gecko.activePane().focus()")
                    waitFor("document.activeElement&&document.activeElement.classList.contains('xterm-helper-textarea')")
                }
                // The keyboard comes up for it, as after a tap.
                val a = act()
                a.runOnUiThread {
                    val imm = a.getSystemService(android.view.inputmethod.InputMethodManager::class.java)
                    imm.restartInput(a.web)
                    imm.showSoftInput(a.web, 0)
                }
                // The keyboard connects to what has focus now (if it changed).
                val until = System.currentTimeMillis() + 2000
                while (ScriptIme.started == starts && System.currentTimeMillis() < until) Thread.sleep(50)
                Thread.sleep(300)
                js("__sent.length=0")
                val ime = Ime(connect())
                ime.run(c.run)
                Thread.sleep(400)
                if (compose) android.util.Log.i(TAG, "compose holds " + js("document.querySelector('#compose textarea').value") + ", focus " + js("document.activeElement.tagName+'.'+document.activeElement.className") + ", starts ${ScriptIme.started}, ic ${System.identityHashCode(ime)}")
                // The compose box sends with its button (or Enter, already pressed).
                if (compose && !c.enterAtEnd) { js("document.querySelector('#compose [data-send]').click()"); Thread.sleep(200) }
                Thread.sleep(300)
                val sent = js("JSON.stringify(__sent.join(''))").let { org.json.JSONTokener(it).nextValue() as String }.let { org.json.JSONTokener(it).nextValue() as String }
                val r = replay(sent)
                val want = c.expect
                val enter = compose || c.enterAtEnd
                val ok = if (c.raw != null) sent == c.raw || sent == c.raw.replace("\u001b[", "\u001bO")
                    else r.line == want && r.stray.isEmpty() && (if (enter) r.enters == 1 && r.endsWithEnter else r.enters == 0)
                android.util.Log.i(TAG, "${if (compose) "compose" else "terminal"} / ${c.name}: ${if (ok) "ok" else "WRONG"} sent ${show(sent)} -> '${r.line}'")
                if (!ok) failures += "${c.name}: sent ${show(sent)}, line '${r.line}' (want '$want'), enters ${r.enters}, stray ${r.stray}"
                // Clear what's left on the shell's line.
                js("gecko.sendInput(gecko.activeId(), '\\u0015')")
                if (compose) js("document.querySelector('#compose textarea').value=''")
                Thread.sleep(200)
            }
            js("gecko.toggleCompose(false)")
            assertEquals("dictation into the ${if (compose) "compose box" else "terminal"}", "", failures.joinToString("\n"))
        }
    }

    /** The line the shell would hold after these bytes (DEL deletes a character). */
    class Replayed(val line: String, val enters: Int, val endsWithEnter: Boolean, val stray: List<String>)

    private fun replay(s: String): Replayed {
        val line = StringBuilder()
        var enters = 0
        val stray = ArrayList<String>()
        var i = 0
        val body = s.replace("\u001b[200~", "").replace("\u001b[201~", "")
        while (i < body.length) {
            val cp = body.codePointAt(i)
            val n = Character.charCount(cp)
            when {
                cp == 0x7f || cp == 8 -> if (line.isNotEmpty()) line.setLength(line.length - Character.charCount(line.codePointBefore(line.length)))
                cp == 13 -> enters++
                cp < 32 -> stray += "\\x%02x".format(cp)
                else -> line.appendCodePoint(cp)
            }
            i += n
        }
        return Replayed(line.toString(), enters, body.endsWith("\r"), stray)
    }

    private fun show(s: String) = s.map { if (it.code < 32 || it.code == 127) "\\x%02x".format(it.code) else it.toString() }.joinToString("")

    /** The keyboard's input connection to what has focus now. */
    private fun connect(): InputConnection {
        val until = System.currentTimeMillis() + 10_000
        while (System.currentTimeMillis() < until) {
            ScriptIme.current?.currentInputConnection?.let { return it }
            if (System.currentTimeMillis() + 9_800 < until) android.util.Log.i(TAG, "keyboard: service ${ScriptIme.current}, inputs started ${ScriptIme.started}")
            Thread.sleep(100)
        }
        throw AssertionError("the test keyboard has no input connection")
    }

    /** A keyboard's calls, each on the connection's thread, a few ms apart like Gboard's. */
    inner class Ime(private val ic: InputConnection, private val pace: Long = 60) {
        private val handler = ic.handler ?: Handler(Looper.getMainLooper())

        private fun call(f: InputConnection.() -> Unit) {
            val latch = CountDownLatch(1)
            handler.post { ic.f(); latch.countDown() }
            latch.await(5, TimeUnit.SECONDS)
            Thread.sleep(pace)
        }

        fun compose(text: String) = call { setComposingText(text, 1) }
        fun commit(text: String) = call { commitText(text, 1) }
        fun finish() = call { finishComposingText() }
        fun delete(before: Int) = call { deleteSurroundingText(before, 0) }
        fun region(start: Int, end: Int) = call { setComposingRegion(start, end) }
        fun key(desc: String) {
            androidx.test.uiautomator.UiDevice.getInstance(InstrumentationRegistry.getInstrumentation())
                .findObject(androidx.test.uiautomator.By.desc(desc)).click()
            Thread.sleep(300)
        }
        /** The keyboard's Backspace as a key (Samsung's, outside composing). */
        fun del() {
            call { sendKeyEvent(KeyEvent(KeyEvent.ACTION_DOWN, KeyEvent.KEYCODE_DEL)) }
            call { sendKeyEvent(KeyEvent(KeyEvent.ACTION_UP, KeyEvent.KEYCODE_DEL)) }
        }
        fun enter() {
            call { sendKeyEvent(KeyEvent(KeyEvent.ACTION_DOWN, KeyEvent.KEYCODE_ENTER)) }
            call { sendKeyEvent(KeyEvent(KeyEvent.ACTION_UP, KeyEvent.KEYCODE_ENTER)) }
        }
    }

    private fun act(): TerminalActivity = activity ?: run { var x: TerminalActivity? = null; sc.onActivity { x = it }; activity = x; x!! }

    /** A tap on the terminal, as a person would: the keyboard comes up for it. */
    private fun tapTerminal() {
        val dev = androidx.test.uiautomator.UiDevice.getInstance(InstrumentationRegistry.getInstrumentation())
        val web = dev.findObject(androidx.test.uiautomator.UiSelector().className("android.webkit.WebView")).bounds
        val starts = ScriptIme.started
        dev.click(web.centerX(), web.centerY())
        val until = System.currentTimeMillis() + 3000
        while (ScriptIme.started == starts && System.currentTimeMillis() < until) Thread.sleep(50)
        Thread.sleep(500)
    }

    // What the tab shows now (its visible rows).
    private val screenJs = "(function(){var t=gecko.activePane().term,b=t.buffer.active,o=[];for(var i=b.viewportY;i<b.viewportY+t.rows;i++){var l=b.getLine(i);if(l)o.push(l.translateToString(true))}return o.join('\\n')})()"

    private fun js(script: String): String {
        val expr = script.replace("screen()", screenJs)
        val a = act()
        val latch = CountDownLatch(1)
        var out = ""
        a.runOnUiThread { a.web.evaluateJavascript(expr) { out = it ?: ""; latch.countDown() } }
        latch.await(10, TimeUnit.SECONDS)
        return out
    }

    private fun waitFor(cond: String, seconds: Int = 20) {
        val until = System.currentTimeMillis() + seconds * 1000L
        while (System.currentTimeMillis() < until) {
            if (js("!!($cond)") == "true") return
            Thread.sleep(200)
        }
        throw AssertionError("timed out waiting for $cond")
    }

    companion object {
        const val TAG = "DictationTest"
        private val ctx get() = ApplicationProvider.getApplicationContext<android.content.Context>()
        private val component get() = android.content.ComponentName(ctx, ScriptIme::class.java)
        private var was = ""

        private fun sh(cmd: String): String {
            val pfd = InstrumentationRegistry.getInstrumentation().uiAutomation.executeShellCommand(cmd)
            return android.os.ParcelFileDescriptor.AutoCloseInputStream(pfd).use { it.readBytes().decodeToString().trim() }
        }

        /** Our keyboard for these tests (the debug build's ScriptIme). */
        @org.junit.BeforeClass @JvmStatic fun useScriptIme() {
            val ime = component.flattenToShortString()
            was = sh("settings get secure default_input_method")
            ctx.packageManager.setComponentEnabledSetting(component, android.content.pm.PackageManager.COMPONENT_ENABLED_STATE_ENABLED, android.content.pm.PackageManager.DONT_KILL_APP)
            // The system picks up the new keyboard.
            val until = System.currentTimeMillis() + 10_000
            while (!sh("ime list -a -s").contains(ime) && System.currentTimeMillis() < until) Thread.sleep(300)
            android.util.Log.i(TAG, "keyboard: " + sh("ime enable $ime 2>&1") + " / " + sh("ime set $ime 2>&1"))
        }

        /** The keyboard there was before. */
        @org.junit.AfterClass @JvmStatic fun restoreKeyboard() {
            val ime = component.flattenToShortString()
            if (was.isNotEmpty() && was != "null") sh("ime set $was")
            sh("ime disable $ime")
            ctx.packageManager.setComponentEnabledSetting(component, android.content.pm.PackageManager.COMPONENT_ENABLED_STATE_DEFAULT, android.content.pm.PackageManager.DONT_KILL_APP)
        }
        const val LONG = "The build failed on the payments test. It looks like the card number check is wrong. Please fix it and run the tests again."
    }
}
