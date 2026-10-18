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

    /** Gecko's web UI from this APK, served for every machine (see [BundledUi]). */
    val bundled by lazy { BundledUi(this) }

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
        // The page on this phone's own Gecko registers a service worker
        // (a secure context); requests it passes on are served the same way.
        try {
            androidx.webkit.ServiceWorkerControllerCompat.getInstance().setServiceWorkerClient(object : androidx.webkit.ServiceWorkerClientCompat() {
                override fun shouldInterceptRequest(request: WebResourceRequest): WebResourceResponse? = intercept(request)
            })
        } catch (e: Exception) {
            Log.w(TAG, "service worker client: ${e.message}")
        }
        web.webChromeClient = object : WebChromeClient() {
            override fun onConsoleMessage(m: ConsoleMessage): Boolean {
                Log.d(TAG, "${m.messageLevel()}: ${m.message()} (${m.sourceId()}:${m.lineNumber()})")
                return true
            }
        }

        // The web UI opens its drawer with a swipe in from the left edge,
        // where Android's back gesture lives: keep the back gesture off a
        // strip there (Android grants at most 200dp of height per edge), in
        // the middle of the terminal. Back still works from the rest of the
        // edge and with the button.
        // The strip is the root's, from the screen edge: in landscape the page
        // starts after a camera cutout, and a swipe from the edge begins
        // there, beside the page (passed on to it below).
        if (Build.VERSION.SDK_INT >= 29) {
            web.addOnLayoutChangeListener { _, left, top, right, bottom, _, _, _, _ ->
                val d = resources.displayMetrics.density
                // A persistent sidebar (wide, not short): no drawer, so no swipe.
                if ((right - left) / d > 760 && (bottom - top) / d > 500) {
                    root.systemGestureExclusionRects = emptyList()
                    return@addOnLayoutChangeListener
                }
                val at = IntArray(2).also { web.getLocationInWindow(it) }
                val rootAt = IntArray(2).also { root.getLocationInWindow(it) }
                val webTop = at[1] - rootAt[1]
                val h = (200 * d).toInt().coerceAtMost(bottom - top)
                val y = webTop + ((bottom - top) - h) / 2
                root.systemGestureExclusionRects = listOf(android.graphics.Rect(0, y, at[0] - rootAt[0] + (32 * d).toInt(), y + h))
            }
        }
        // Touches beside the page (over a cutout's inset at the left or
        // right edge) go to the page's edge, so the drawer swipe works from
        // the screen edge.
        root.setOnTouchListener { v, ev ->
            val at = IntArray(2).also { web.getLocationInWindow(it) }
            val rootAt = IntArray(2).also { v.getLocationInWindow(it) }
            val dx = (at[0] - rootAt[0]).toFloat()
            val dy = (at[1] - rootAt[1]).toFloat()
            if (ev.actionMasked == android.view.MotionEvent.ACTION_DOWN && (ev.y < dy || ev.y > dy + web.height)) return@setOnTouchListener false
            val e = android.view.MotionEvent.obtain(ev)
            e.offsetLocation(-dx, -dy)
            if (e.x < 0) e.setLocation(0f, e.y)
            if (e.x > web.width - 1) e.setLocation((web.width - 1).toFloat(), e.y)
            val handled = web.dispatchTouchEvent(e)
            e.recycle()
            handled || ev.actionMasked != android.view.MotionEvent.ACTION_DOWN
        }

        keys.setHardwareKeyboard(hasHardwareKeyboard())
        getSystemService(android.hardware.input.InputManager::class.java)?.registerInputDeviceListener(keyboards, null)
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
        fitBars()
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

    // Rotation, folding, a window resize, dark mode, text size: the page
    // stays (and with it the session and the scroll position) and refits.
    override fun onConfigurationChanged(newConfig: android.content.res.Configuration) {
        super.onConfigurationChanged(newConfig)
        fitBars()
        keys.resize()
        keys.setHardwareKeyboard(hasHardwareKeyboard())
    }

    override fun onMultiWindowModeChanged(inMultiWindow: Boolean, newConfig: android.content.res.Configuration) {
        super.onMultiWindowModeChanged(inMultiWindow, newConfig)
        fitBars()
        keys.resize()
    }

    /**
     * A phone on its side has little height: the status bar's row goes to
     * the terminal (a swipe down from the top shows it for a moment) and the
     * keys row is slimmer. Tablets, unfolded foldables and split windows
     * keep both.
     */
    private fun fitBars() {
        val c = resources.configuration
        val short = shortLandscape(c.orientation == android.content.res.Configuration.ORIENTATION_LANDSCAPE, c.screenHeightDp, isInMultiWindowMode)
        keys.compact = short
        WindowCompat.getInsetsController(window, window.decorView).apply {
            systemBarsBehavior = androidx.core.view.WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
            if (short) hide(WindowInsetsCompat.Type.statusBars()) else show(WindowInsetsCompat.Type.statusBars())
        }
    }

    /**
     * A keyboard plugged in or paired (not the phone's own buttons, nor
     * a built-in one that only some devices and emulators report).
     */
    private fun hasHardwareKeyboard(): Boolean {
        val im = getSystemService(android.hardware.input.InputManager::class.java) ?: return false
        return im.inputDeviceIds.any { id ->
            val d = im.getInputDevice(id) ?: return@any false
            !d.isVirtual && d.keyboardType == android.view.InputDevice.KEYBOARD_TYPE_ALPHABETIC &&
                (Build.VERSION.SDK_INT < 29 || d.isExternal)
        }
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

    private val keyboards = object : android.hardware.input.InputManager.InputDeviceListener {
        override fun onInputDeviceAdded(id: Int) = keys.setHardwareKeyboard(hasHardwareKeyboard())
        override fun onInputDeviceRemoved(id: Int) = keys.setHardwareKeyboard(hasHardwareKeyboard())
        override fun onInputDeviceChanged(id: Int) = keys.setHardwareKeyboard(hasHardwareKeyboard())
    }

    override fun onDestroy() {
        getSystemService(android.hardware.input.InputManager::class.java)?.unregisterInputDeviceListener(keyboards)
        web.destroy()
        super.onDestroy()
    }

    /** Shows the machine an intent asks for; then a tab to focus or shared text to paste. */
    private fun openFrom(intent: Intent, fresh: Boolean): Boolean {
        val store = Gecko.machines(this)
        val shared = if (intent.action == Intent.ACTION_SEND) intent.getStringExtra(Intent.EXTRA_TEXT) else null
        val wantId = intent.getStringExtra(EXTRA_MACHINE)
        val target = when {
            // Termux was a machine of its own before it became This phone's (old notifications, shortcuts).
            wantId == LocalGecko.ID || wantId == Termux.ID -> localPlaceholder()
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
        errorView.visibility = View.GONE; keys.visibility = View.VISIBLE
        PhoneService.start(this)
        Thread {
            val ok = LocalGecko.start(this)
            val m = if (ok) LocalGecko.machine(this) else null
            runOnUiThread {
                if (isDestroyed || !onThisPhone) return@runOnUiThread
                if (m == null) return@runOnUiThread showError(getString(R.string.phone_failed))
                machine = m
                origin = LinkParser.origin(m.url)
                currentOrigin = origin
                openPage(m)
            }
        }.start()
    }

    /** The system WebView's major version, when it's older than the page needs. */
    private fun oldWebView(): String? {
        val v = androidx.webkit.WebViewCompat.getCurrentWebViewPackage(this)?.versionName ?: return null
        val major = v.substringBefore('.').toIntOrNull() ?: return null
        return if (major < MIN_WEBVIEW) v else null
    }

    private fun load() {
        oldWebView()?.let { v ->
            showError(getString(R.string.webview_old, v))
            findViewById<Button>(R.id.retry).apply {
                setText(R.string.webview_update)
                setOnClickListener {
                    val pkg = androidx.webkit.WebViewCompat.getCurrentWebViewPackage(this@TerminalActivity)?.packageName ?: "com.google.android.webview"
                    try { startActivity(Intent(Intent.ACTION_VIEW, android.net.Uri.parse("market://details?id=$pkg"))) }
                    catch (e: Exception) { startActivity(Intent(Intent.ACTION_VIEW, android.net.Uri.parse("https://play.google.com/store/apps/details?id=$pkg"))) }
                }
            }
            return
        }
        val m = machine ?: return
        if (m.id == LocalGecko.ID) return loadLocal()
        errorView.visibility = View.GONE; keys.visibility = View.VISIBLE
        if (m.token.isEmpty()) return showError(getString(R.string.err_no_token))
        val link = try { LinkParser.parse(m.url) } catch (e: LinkException) { return showError(e.message ?: "") }
        origin = LinkParser.origin(m.url)
        currentOrigin = origin
        Thread {
            // The token must not travel in cleartext outside a private network.
            val ok = link.https || NetPolicy.cleartextAllowedResolving(link.host)
            // With the bundled UI the page itself never asks the machine
            // with the token in the link, so check the token here: a wrong
            // one gets the same screen as before.
            val refused: Boolean? = if (ok && bundled.available) tokenRefused(m) else false
            runOnUiThread {
                if (isDestroyed) return@runOnUiThread
                when {
                    !ok -> showError(getString(R.string.err_cleartext, link.host))
                    refused == null -> showError(getString(R.string.err_unreachable, m.name, ""))
                    refused -> showError(getString(R.string.err_token))
                    else -> openPage(m)
                }
            }
        }.start()
    }

    /** Whether the machine refuses m's token: null when it can't be reached. Blocking. */
    private fun tokenRefused(m: Machine): Boolean? = try {
        val client = okhttp3.OkHttpClient.Builder().callTimeout(10, java.util.concurrent.TimeUnit.SECONDS).build()
        val req = okhttp3.Request.Builder().url(m.url + "api/info").header("Authorization", "Bearer " + m.token).build()
        client.newCall(req).execute().use {
            if (it.isSuccessful) daemonBuild = try { daemonBuildOf(org.json.JSONObject(it.body?.string() ?: "")) } catch (e: Exception) { null }
            it.code == 401 || it.code == 403
        }
    } catch (e: Exception) {
        Log.w(TAG, "check token: ${e.message}")
        null
    }

    /**
     * Shows the machine's Gecko. With the bundled UI the token goes in as
     * the daemon's own cookie (what its ?token= link would set) and the page
     * loads without it: that link is answered by a redirect, which a web
     * view doesn't let the app serve, so the machine's own UI would show.
     */
    private fun openPage(m: Machine) {
        if (!bundled.available) return web.loadUrl(LinkParser.withToken(m.url, m.token))
        val cm = android.webkit.CookieManager.getInstance()
        cm.setCookie(m.url, "gecko_token=${m.token}; Path=/; HttpOnly; SameSite=Strict; Max-Age=34560000") {
            cm.flush()
            if (!isDestroyed) web.loadUrl(m.url)
        }
    }

    /** A request from the page: the machine's own UI files come from the APK instead. */
    private fun intercept(request: WebResourceRequest): WebResourceResponse? {
        val o = currentOrigin ?: return null
        if (LinkParser.origin(request.url.toString()) != o) return null
        return bundled.respond(request.method, request.url)
    }

    private fun showError(text: String) {
        pageTrusted = false
        findViewById<TextView>(R.id.error_text).text = text
        errorView.visibility = View.VISIBLE
        keys.visibility = View.GONE // nothing to type into
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
                menu.add(0, MENU_SSH_KEY, 12, R.string.ssh_key_title)
                menu.add(0, MENU_STOP, 13, R.string.phone_stop_long)
            }
            if (Gecko.prefs(this@TerminalActivity).getBoolean(Gecko.BACKGROUND, false) && KeepAlive.needsHelp(this@TerminalActivity)) {
                menu.add(0, MENU_KEEPALIVE, 80, R.string.keepalive_menu)
            }
            menu.add(0, MENU_TOUCH, 90, R.string.touch_diagnostics)
            menu.add(0, MENU_BUILD, 100, BuildId.small(this@TerminalActivity, BuildId.current))
            setOnMenuItemClickListener { item ->
                when (item.itemId) {
                    MENU_ALLOW -> if (item.isChecked) ShareUi.turnOff(this@TerminalActivity) else ShareUi.turnOn(this@TerminalActivity)
                    MENU_LINKS -> ShareUi.showLinks(this@TerminalActivity)
                    MENU_SSH_KEY -> SshKeyUi.show(this@TerminalActivity)
                    MENU_TOUCH -> showTouchDiagnostics()
                    MENU_KEEPALIVE -> KeepAlive.show(this@TerminalActivity)
                    MENU_BUILD -> BuildId.copy(this@TerminalActivity)
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

    /** The connected Gecko's build, from its /api/info ("" when it's too old to say). */
    @Volatile private var daemonBuild: String? = null

    private fun daemonBuildOf(info: org.json.JSONObject): String {
        val commit = info.optString("commit")
        if (commit.isEmpty()) return ""
        val id = BuildId.format(commit, info.optString("built"))
        val v = info.optString("version")
        return if (v.isEmpty() || v == "dev") id else "$id ($v)"
    }

    /** This app's build id, and what the last gesture on a terminal looked like (gecko.touchReport), and this device's screen and web view. */
    private fun showTouchDiagnostics() {
        web.evaluateJavascript("window.gecko&&gecko.touchReport?gecko.touchReport():'The page has no touch report.'") { v ->
            val page = try { org.json.JSONTokener(v).nextValue() as? String } catch (e: Exception) { null } ?: "The page isn't loaded."
            @Suppress("DEPRECATION")
            val hz = windowManager.defaultDisplay.refreshRate
            val wv = androidx.webkit.WebViewCompat.getCurrentWebViewPackage(this)?.versionName ?: "?"
            val there = when (val b = daemonBuild) {
                null -> ""
                "" -> "\nGecko there: too old to report its build"
                else -> if (b.startsWith(BuildConfig.BUILD_COMMIT + " ")) "" else "\nGecko there: build $b"
            }
            val text = "App build: ${BuildId.current}\n" + page + there + "\nDisplay: %.0f Hz; WebView %s; Android %s (%s)".format(hz, wv, Build.VERSION.RELEASE, Build.MODEL)
            AlertDialog.Builder(this)
                .setTitle(R.string.touch_diagnostics)
                .setMessage(text)
                .setPositiveButton(android.R.string.ok, null)
                .setNeutralButton(R.string.copy) { _, _ ->
                    (getSystemService(CLIPBOARD_SERVICE) as android.content.ClipboardManager).setPrimaryClip(android.content.ClipData.newPlainText("Gecko", text))
                }
                .show()
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
        KeepAlive.offerOnce(this)
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
            KeepAlive.offerOnce(this)
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
        override fun shouldInterceptRequest(view: WebView, request: WebResourceRequest): WebResourceResponse? = intercept(request)

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
        private const val MENU_SSH_KEY = 104
        private const val MENU_TOUCH = 105
        private const val MENU_BUILD = 106
        private const val MENU_KEEPALIVE = 107

        /** The page needs color-mix() (111), dvh units (108) and ES2022. */
        const val MIN_WEBVIEW = 111

        /** A phone held sideways (not a tablet, not a split window): see fitBars. */
        fun shortLandscape(landscape: Boolean, heightDp: Int, multiWindow: Boolean) =
            !multiWindow && landscape && heightDp in 1 until 480

        /** The origin of the machine on show, for requests on web view threads. */
        @Volatile var currentOrigin: String? = null

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
