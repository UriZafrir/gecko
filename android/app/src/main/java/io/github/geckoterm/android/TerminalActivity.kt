package io.github.geckoterm.android

import android.Manifest
import android.annotation.SuppressLint
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Bitmap
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.util.Log
import android.view.View
import android.view.WindowManager
import android.webkit.ConsoleMessage
import android.webkit.WebChromeClient
import android.webkit.WebResourceError
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebSettings
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.Button
import android.widget.PopupMenu
import android.widget.TextView
import android.widget.Toast
import androidx.activity.OnBackPressedCallback
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AlertDialog
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.ContextCompat
import androidx.core.view.ViewCompat
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import org.json.JSONObject

/** One machine's Gecko web UI, fitted to the phone, with the extra keys row under it. */
class TerminalActivity : AppCompatActivity() {
    lateinit var web: WebView
        private set
    lateinit var keys: ExtraKeys
        private set
    private lateinit var errorView: View
    @Volatile private var machine: Machine? = null
    private var origin: String? = null
    private var dark = true

    /** The page in the web view is the machine's own (checked by the bridge). */
    @Volatile var pageTrusted = false
        private set

    private var startBackgroundAfterGrant = false

    private val askNotifications = registerForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
        if (granted) {
            // Turning on the background connection says so itself.
            if (!startBackgroundAfterGrant) toastInPage(getString(R.string.notify_on))
            offerBackground()
        } else {
            toastInPage(getString(R.string.notify_blocked))
        }
    }

    @SuppressLint("SetJavaScriptEnabled")
    override fun onCreate(savedInstanceState: Bundle?) {
        enableEdgeToEdge()
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_terminal)
        web = findViewById(R.id.web)
        keys = findViewById(R.id.keys)
        errorView = findViewById(R.id.error)
        Notifier.channels(this)

        // Bars and keyboard: the root's padding keeps the page clear of the
        // status bar, the navigation bar and the keyboard, and the extra keys
        // sit right on top of the keyboard. The web view shrinks, so the
        // terminal refits and the cursor line stays in view.
        val root = findViewById<View>(R.id.root)
        val topFill = findViewById<View>(R.id.top_fill)
        ViewCompat.setOnApplyWindowInsetsListener(root) { v, insets ->
            val bars = insets.getInsets(WindowInsetsCompat.Type.systemBars() or WindowInsetsCompat.Type.displayCutout())
            val ime = insets.getInsets(WindowInsetsCompat.Type.ime())
            v.setPadding(bars.left, 0, bars.right, maxOf(bars.bottom, ime.bottom))
            topFill.layoutParams = topFill.layoutParams.apply { height = bars.top }
            WindowInsetsCompat.CONSUMED
        }

        WebView.setWebContentsDebuggingEnabled(BuildConfig.DEBUG)
        web.settings.apply {
            javaScriptEnabled = true
            domStorageEnabled = true
            mediaPlaybackRequiresUserGesture = false
            textZoom = 100
            setSupportMultipleWindows(false)
            cacheMode = WebSettings.LOAD_DEFAULT
            allowFileAccess = false
            allowContentAccess = false
        }
        web.addJavascriptInterface(Bridge(this), "GeckoAndroid")
        web.webViewClient = Client()
        web.webChromeClient = object : WebChromeClient() {
            override fun onConsoleMessage(m: ConsoleMessage): Boolean {
                Log.d(TAG, "${m.messageLevel()}: ${m.message()} (${m.sourceId()}:${m.lineNumber()})")
                return true
            }
        }

        keys.onKey = { pressKey(it) }
        keys.onMenu = { showMenu(it) }
        findViewById<Button>(R.id.retry).setOnClickListener { load() }
        findViewById<Button>(R.id.machines).setOnClickListener { leave() }

        onBackPressedDispatcher.addCallback(this, object : OnBackPressedCallback(true) {
            override fun handleOnBackPressed() {
                if (errorView.visibility == View.VISIBLE || !pageTrusted) return leave()
                web.evaluateJavascript(BACK_JS) { r -> if (r != "true") leave() }
            }
        })

        applyKeepScreenOn()
        // Recreated (rotation, process restart): just the machine again; the
        // web UI restores its own state. A share or a tab was handled already.
        val again = savedInstanceState?.getString(EXTRA_MACHINE)
        openFrom(if (again != null) Intent().putExtra(EXTRA_MACHINE, again) else intent, true)
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        openFrom(intent, true)
    }

    override fun onSaveInstanceState(outState: Bundle) {
        super.onSaveInstanceState(outState)
        machine?.let { outState.putString(EXTRA_MACHINE, it.id) }
    }

    override fun onResume() {
        super.onResume()
        Gecko.foregroundMachine = machine?.id
        if (Gecko.prefs(this).getBoolean(Gecko.BACKGROUND, false)) WatchService.refresh(this)
    }

    override fun onPause() {
        Gecko.foregroundMachine = null
        super.onPause()
    }

    override fun onDestroy() {
        web.destroy()
        super.onDestroy()
    }

    /** Shows the machine an intent asks for; then a tab to focus or shared text to paste. */
    private fun openFrom(intent: Intent, fresh: Boolean): Boolean {
        val store = Gecko.machines(this)
        val shared = if (intent.action == Intent.ACTION_SEND) intent.getStringExtra(Intent.EXTRA_TEXT) else null
        val wantId = intent.getStringExtra(EXTRA_MACHINE)
        val target = when {
            wantId == LocalGecko.ID -> localPlaceholder()
            wantId != null -> store.get(wantId)
            shared != null -> machine ?: pickForShare(store, shared).let { return true }
            else -> machine ?: store.recent() ?: store.list().firstOrNull()
        }
        if (target == null) {
            Toast.makeText(this, R.string.no_machine, Toast.LENGTH_LONG).show()
            startActivity(Intent(this, ConnectActivity::class.java))
            finish()
            return false
        }
        val switching = machine?.id != target.id
        // This phone, already loaded: keep its address and token.
        if (switching || target.id != LocalGecko.ID) machine = target
        if (target.id != LocalGecko.ID) store.touch(target.id)
        Gecko.foregroundMachine = target.id
        title = target.name
        if (switching || (fresh && web.url == null)) load()
        intent.getStringExtra(EXTRA_SESSION)?.let { id ->
            Notifier.cancel(this, target.id, id)
            whenReady("g.sessions.has(${js(id)})", "g.focusSession(${js(id)})")
        }
        shared?.let { pasteShared(it) }
        return true
    }

    /** Text shared from another app, with no machine open yet: the last one used, or ask. */
    private fun pickForShare(store: MachineStore, text: String) {
        val all = store.list()
        val pick = { m: Machine ->
            setIntent(Intent().putExtra(EXTRA_MACHINE, m.id))
            openFrom(intent, true)
            pasteShared(text)
        }
        when {
            all.isEmpty() -> {
                Toast.makeText(this, R.string.no_machine, Toast.LENGTH_LONG).show()
                startActivity(Intent(this, ConnectActivity::class.java))
                finish()
            }
            all.size == 1 -> pick(all[0])
            else -> AlertDialog.Builder(this)
                .setTitle(R.string.share_to)
                .setItems(all.map { it.name }.toTypedArray()) { _, i -> pick(all[i]) }
                .setOnCancelListener { finish() }
                .show()
        }
    }

    private fun pasteShared(text: String) {
        // As a paste: the shell or agent gets it as typed text, not run.
        whenReady("g.activePane()", "g.activePane().term.paste(${js(text)}); g.activePane().focus()")
    }

    /** This phone before its daemon has told us its address and token. */
    private fun localPlaceholder() = Machine(id = LocalGecko.ID, name = getString(R.string.this_phone), url = "http://127.0.0.1/", token = "")

    val onThisPhone: Boolean get() = machine?.id == LocalGecko.ID

    /** Starts Gecko on this phone (in its service, so it keeps running), then shows it. */
    private fun loadLocal() {
        errorView.visibility = View.GONE
        PhoneService.start(this)
        Thread {
            val ok = LocalGecko.start(this)
            val m = if (ok) LocalGecko.machine(this) else null
            runOnUiThread {
                if (isDestroyed || !onThisPhone) return@runOnUiThread
                if (m == null) return@runOnUiThread showError(getString(R.string.phone_failed))
                machine = m
                origin = LinkParser.origin(m.url)
                web.loadUrl(LinkParser.withToken(m.url, m.token))
            }
        }.start()
    }

    private fun load() {
        val m = machine ?: return
        if (m.id == LocalGecko.ID) return loadLocal()
        errorView.visibility = View.GONE
        if (m.token.isEmpty()) return showError(getString(R.string.err_no_token))
        val link = try { LinkParser.parse(m.url) } catch (e: LinkException) { return showError(e.message ?: "") }
        origin = LinkParser.origin(m.url)
        Thread {
            // The token must not travel in cleartext outside a private network.
            val ok = link.https || NetPolicy.cleartextAllowedResolving(link.host)
            runOnUiThread {
                if (isDestroyed) return@runOnUiThread
                if (!ok) showError(getString(R.string.err_cleartext, link.host))
                else web.loadUrl(LinkParser.withToken(m.url, m.token))
            }
        }.start()
    }

    private fun showError(text: String) {
        pageTrusted = false
        findViewById<TextView>(R.id.error_text).text = text
        errorView.visibility = View.VISIBLE
    }

    private fun leave() {
        startActivity(Intent(this, ConnectActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP))
        finish()
    }

    /** Runs action in the page once cond holds (the UI has loaded and synced), for up to 20 seconds. */
    fun whenReady(cond: String, action: String) {
        web.evaluateJavascript(
            """(function(){var n=0;var t=setInterval(function(){var g=window.gecko;n++;
               if(g&&g.firstSync&&($cond)){clearInterval(t);$action}else if(n>200)clearInterval(t);},100);})()""",
            null,
        )
    }

    private fun pressKey(name: String) {
        if (!pageTrusted) return
        val fallback = ExtraKeys.FALLBACK[name]?.let { js(it) } ?: "null"
        web.evaluateJavascript(
            """(function(){var g=window.gecko;if(!g)return;
               if(g.pressKey)g.pressKey(${js(name)});
               else if(${js(name)}==='ctrl'||${js(name)}==='alt'){g.mods[${js(name)}]=!g.mods[${js(name)}];g.renderKeybar();}
               else if(${js(name)}==='paste')g.paste();
               else if(${js(name)}==='select')g.openTextView();
               else if(${js(name)}==='compose')g.toggleCompose();
               else if($fallback&&g.activeId())g.sendInput(g.activeId(),$fallback);})()""",
            null,
        )
    }

    private fun showMenu(anchor: View) {
        val p = Gecko.prefs(this)
        PopupMenu(this, anchor).apply {
            menuInflater.inflate(R.menu.terminal, menu)
            menu.findItem(R.id.keep_screen_on).isChecked = p.getBoolean(Gecko.KEEP_SCREEN_ON, false)
            menu.findItem(R.id.background).isChecked = p.getBoolean(Gecko.BACKGROUND, false)
            if (onThisPhone) {
                menu.add(0, MENU_ALLOW, 10, R.string.share_toggle).isCheckable = true
                menu.findItem(MENU_ALLOW).isChecked = ShareUi.allowed(this@TerminalActivity)
                if (ShareUi.allowed(this@TerminalActivity)) menu.add(0, MENU_LINKS, 11, R.string.share_links_title)
                menu.add(0, MENU_STOP, 12, R.string.phone_stop_long)
            }
            setOnMenuItemClickListener { item ->
                when (item.itemId) {
                    MENU_ALLOW -> if (item.isChecked) ShareUi.turnOff(this@TerminalActivity) else ShareUi.turnOn(this@TerminalActivity)
                    MENU_LINKS -> ShareUi.showLinks(this@TerminalActivity)
                    MENU_STOP -> {
                        PhoneService.stop(this@TerminalActivity)
                        leave()
                    }
                    R.id.keep_screen_on -> {
                        p.edit().putBoolean(Gecko.KEEP_SCREEN_ON, !item.isChecked).apply()
                        applyKeepScreenOn()
                    }
                    R.id.background -> if (item.isChecked) WatchService.stop(this@TerminalActivity) else startBackground()
                    R.id.palette -> web.evaluateJavascript("window.gecko&&gecko.openPalette()", null)
                    R.id.reload -> load()
                    R.id.machines -> leave()
                }
                true
            }
            show()
        }
    }

    private fun applyKeepScreenOn() {
        if (Gecko.prefs(this).getBoolean(Gecko.KEEP_SCREEN_ON, false)) window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        else window.clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
    }

    private fun needsPermission() = Build.VERSION.SDK_INT >= 33 &&
        ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED

    private fun startBackground() {
        if (needsPermission()) {
            startBackgroundAfterGrant = true
            askNotifications.launch(Manifest.permission.POST_NOTIFICATIONS)
            return
        }
        WatchService.start(this)
        toastInPage(getString(R.string.background_on))
    }

    /** "Notify me when agents need input" in the web UI's palette. */
    fun enableNotifications() {
        if (needsPermission()) askNotifications.launch(Manifest.permission.POST_NOTIFICATIONS)
        else {
            toastInPage(getString(R.string.notify_on))
            offerBackground()
        }
    }

    private fun offerBackground() {
        if (startBackgroundAfterGrant) {
            startBackgroundAfterGrant = false
            WatchService.start(this)
            toastInPage(getString(R.string.background_on))
            return
        }
        if (Gecko.prefs(this).getBoolean(Gecko.BACKGROUND, false)) return
        AlertDialog.Builder(this)
            .setTitle(R.string.background_title)
            .setMessage(R.string.background_text)
            .setPositiveButton(R.string.background_yes) { _, _ -> startBackground() }
            .setNegativeButton(R.string.background_no, null)
            .show()
    }

    /** From the web UI (it only asks while the page is hidden). */
    fun notifyAgent(title: String, body: String, sessionId: String) {
        val m = machine ?: return
        if (sessionId.isEmpty()) return
        Notifier.agent(this, m, sessionId, title, body)
    }

    private fun toastInPage(text: String) {
        if (pageTrusted) web.evaluateJavascript("window.gecko&&gecko.toast(${js(text)})", null)
        else Toast.makeText(this, text, Toast.LENGTH_SHORT).show()
    }

    /** The web UI's theme, for the system bars and the extra keys. */
    fun applyTheme(json: String) {
        val o = try { JSONObject(json) } catch (e: Exception) { return }
        dark = o.optBoolean("dark", true)
        val bg = ExtraKeys.parseColor(o.optString("bg"), 0xFF19211A.toInt())
        val panel = ExtraKeys.parseColor(o.optString("panel"), bg)
        window.decorView.setBackgroundColor(bg)
        keys.setColors(
            ExtraKeys.Colors(
                bar = panel,
                key = ExtraKeys.parseColor(o.optString("raise"), panel),
                text = ExtraKeys.parseColor(o.optString("text"), 0xFFD8E0D2.toInt()),
                accent = ExtraKeys.parseColor(o.optString("accent"), 0xFF7CCB3A.toInt()),
                onAccent = ExtraKeys.parseColor(o.optString("on-accent"), bg),
            ),
        )
        // The bottom inset (navigation bar) shows the keys row's color.
        findViewById<View>(R.id.root).setBackgroundColor(panel)
        findViewById<View>(R.id.top_fill).setBackgroundColor(bg)
        WindowCompat.getInsetsController(window, window.decorView).apply {
            isAppearanceLightStatusBars = !dark
            isAppearanceLightNavigationBars = !dark
        }
    }

    private inner class Client : WebViewClient() {
        override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
            val u = request.url
            if (LinkParser.origin(u.toString()) == origin) return false
            // Links out of Gecko (in terminal output, docs) open in the browser.
            try {
                startActivity(Intent(Intent.ACTION_VIEW, u).addCategory(Intent.CATEGORY_BROWSABLE))
            } catch (e: Exception) {
                Toast.makeText(this@TerminalActivity, u.toString(), Toast.LENGTH_SHORT).show()
            }
            return true
        }

        override fun onPageStarted(view: WebView, url: String, favicon: Bitmap?) {
            pageTrusted = LinkParser.origin(url) == origin
        }

        override fun doUpdateVisitedHistory(view: WebView, url: String, isReload: Boolean) {
            pageTrusted = LinkParser.origin(url) == origin
        }

        override fun onReceivedError(view: WebView, request: WebResourceRequest, error: WebResourceError) {
            if (request.isForMainFrame) showError(getString(R.string.err_unreachable, machine?.name ?: "", error.description))
        }

        override fun onReceivedHttpError(view: WebView, request: WebResourceRequest, response: WebResourceResponse) {
            if (!request.isForMainFrame) return
            showError(
                if (response.statusCode == 403 || response.statusCode == 401) getString(R.string.err_token)
                else getString(R.string.err_http, response.statusCode, response.reasonPhrase ?: ""),
            )
        }
    }

    companion object {
        private const val TAG = "Gecko"
        const val EXTRA_MACHINE = "machine"
        const val EXTRA_SESSION = "session"
        private const val MENU_ALLOW = 101
        private const val MENU_LINKS = 102
        private const val MENU_STOP = 103

        /** Closes what's open in the web UI (gecko.back in main.js), or says there was nothing. */
        private const val BACK_JS = """(function(){var g=window.gecko;if(!g)return false;
            if(g.back)return g.back();
            if(document.body.classList.contains('drawer')){g.closeDrawer();return true}
            if(g.paletteOpen){g.closePalette();return true}
            return false})()"""

        /** A JavaScript string literal. */
        fun js(s: String): String = JSONObject.quote(s)

        fun open(ctx: android.content.Context, m: Machine) =
            ctx.startActivity(Intent(ctx, TerminalActivity::class.java).putExtra(EXTRA_MACHINE, m.id))
    }
}
