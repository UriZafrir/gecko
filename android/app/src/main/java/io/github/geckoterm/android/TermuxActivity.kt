package io.github.geckoterm.android

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.provider.Settings
import android.util.Log
import android.view.View
import android.widget.Button
import android.widget.ProgressBar
import android.widget.TextView
import android.widget.Toast
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat
import com.google.android.material.appbar.MaterialToolbar

/**
 * Gecko in Termux: checks each thing it needs (Termux, the permission,
 * allow-external-apps, gecko installed there), shows how to fix the first
 * one missing, then starts the daemon there and adds it to This phone's
 * machines, and opens This phone. The steps themselves are [TermuxFlow];
 * this runs the commands.
 */
class TermuxActivity : AppCompatActivity() {
    private var state: TermuxState = TermuxState.Checking
    /** Start and open Gecko as soon as it is ready (tapped in the machine list). */
    private var wantStart = true
    @Volatile private var busy = false

    private val askPermission = registerForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
        val again = granted || shouldShowRequestPermissionRationale(Termux.PERMISSION)
        move(TermuxEvent.Permission(granted, again))
        if (state == TermuxState.Checking) check()
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        enableEdgeToEdge()
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_termux)
        ViewCompat.setOnApplyWindowInsetsListener(findViewById(R.id.root)) { v, insets ->
            val b = insets.getInsets(WindowInsetsCompat.Type.systemBars() or WindowInsetsCompat.Type.displayCutout())
            v.setPadding(b.left, b.top, b.right, b.bottom)
            WindowInsetsCompat.CONSUMED
        }
        findViewById<MaterialToolbar>(R.id.toolbar).setNavigationOnClickListener { finish() }
        wantStart = intent.getBooleanExtra(EXTRA_START, true)
        render()
    }

    override fun onResume() {
        super.onResume()
        // Back from Termux, the settings or an install: look again.
        if (state !is TermuxState.Starting && state !is TermuxState.Running) check()
    }

    private fun move(e: TermuxEvent) {
        val next = TermuxFlow.reduce(state, e)
        Log.i(TAG, "$e: $state -> $next")
        state = next
        runOnUiThread { render() }
    }

    /** From the top: is Termux there, may we use it, does it answer, is gecko in it. */
    private fun check() {
        if (busy) return
        busy = true
        state = TermuxState.Checking
        render()
        Thread {
            try {
                move(TermuxEvent.Checked(Termux.installed(this), Termux.permitted(this)))
                if (state == TermuxState.Checking) {
                    val r = try { Termux.run(this, TermuxFlow.PROBE, "gecko version", 15_000) } catch (e: SecurityException) { null.also { move(TermuxEvent.Refused) } }
                    if (state == TermuxState.Checking) move(TermuxEvent.Probed(r))
                }
                if (state is TermuxState.Ready && wantStart) startGecko()
            } catch (e: Exception) {
                Log.w(TAG, "check", e)
                state = TermuxState.Failed(e.message ?: e.javaClass.simpleName)
                runOnUiThread { render() }
            } finally {
                busy = false
            }
        }.start()
    }

    /** Starts `gecko daemon` in Termux (unless it answers already), gets its link, opens it. Blocking. */
    private fun startGecko() {
        move(TermuxEvent.Start)
        if (state != TermuxState.Starting) return
        val exited = java.util.concurrent.atomic.AtomicReference<TermuxResult?>()
        val up = Termux.portOpen("127.0.0.1", TermuxFlow.PORT) || run {
            Termux.spawn(this, TermuxFlow.START, "gecko daemon") { exited.set(it) }
            val until = System.currentTimeMillis() + 15_000
            while (System.currentTimeMillis() < until && exited.get() == null) {
                if (Termux.portOpen("127.0.0.1", TermuxFlow.PORT)) return@run true
                Thread.sleep(250)
            }
            // A daemon that ends this quickly has said why by now.
            if (exited.get() == null) Thread.sleep(500)
            Termux.portOpen("127.0.0.1", TermuxFlow.PORT)
        }
        move(TermuxEvent.Started(up, if (up) null else exited.get()))
        if (state != TermuxState.Starting) return
        move(TermuxEvent.Url(Termux.run(this, TermuxFlow.URL, "gecko url", 15_000)))
        val s = state
        if (s is TermuxState.Running) {
            // A machine of This phone: start This phone (its service keeps
            // Gecko in Termux running from now on), hand it the link, open it.
            Termux.save(this, s.link)
            PhoneTermux.setPaused(this, false)
            PhoneService.start(this)
            val until = System.currentTimeMillis() + 20_000
            while (!LocalGecko.running(this) && System.currentTimeMillis() < until) Thread.sleep(200)
            val err = if (LocalGecko.running(this)) PhoneTermux.sync(this) else getString(R.string.phone_failed)
            if (err != null) {
                state = TermuxState.Failed(getString(R.string.termux_handoff_failed, err))
                runOnUiThread { render() }
                return
            }
            runOnUiThread {
                Toast.makeText(this, R.string.termux_added, Toast.LENGTH_LONG).show()
                startActivity(Intent(this, TerminalActivity::class.java).putExtra(TerminalActivity.EXTRA_MACHINE, LocalGecko.ID))
                finish()
            }
        }
    }

    private fun copy(text: String) {
        (getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager).setPrimaryClip(ClipData.newPlainText("Termux", text))
    }

    private fun openUrl(url: String) = try {
        startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)))
    } catch (e: Exception) {
        Toast.makeText(this, url, Toast.LENGTH_LONG).show()
    }

    private fun openTermux() {
        if (!Termux.open(this)) Toast.makeText(this, R.string.termux_missing_title, Toast.LENGTH_LONG).show()
    }

    /** The command to install Gecko in Termux; debug builds can point it elsewhere (tests, demos). */
    private fun installCommand(): String =
        (if (BuildConfig.DEBUG) Gecko.prefs(this).getString(INSTALL_OVERRIDE, null) else null) ?: TermuxFlow.INSTALL

    private fun render() {
        val step = findViewById<TextView>(R.id.step)
        val title = findViewById<TextView>(R.id.title)
        val text = findViewById<TextView>(R.id.text)
        val code = findViewById<TextView>(R.id.code)
        val text2 = findViewById<TextView>(R.id.text2)
        val code2 = findViewById<TextView>(R.id.code2)
        val note = findViewById<TextView>(R.id.note)
        val progress = findViewById<ProgressBar>(R.id.progress)
        val primary = findViewById<Button>(R.id.primary)
        val secondary = findViewById<Button>(R.id.secondary)
        fun show(v: TextView, s: CharSequence?) { v.text = s ?: ""; v.visibility = if (s == null) View.GONE else View.VISIBLE }
        fun button(b: Button, label: Int?, action: (() -> Unit)?) {
            b.visibility = if (label == null) View.INVISIBLE else View.VISIBLE
            if (label != null) b.setText(label)
            b.setOnClickListener { action?.invoke() }
        }
        show(step, null); show(code, null); show(text2, null); show(code2, null); show(note, null)
        progress.visibility = View.GONE
        button(secondary, null, null)
        val playNote = if (Termux.installed(this) && Termux.fromPlay(this)) getString(R.string.termux_play_warning) else null
        val install = installCommand()
        when (val s = state) {
            TermuxState.Checking, TermuxState.Starting, is TermuxState.Running -> {
                title.setText(when (s) { TermuxState.Checking -> R.string.termux_checking; TermuxState.Starting -> R.string.termux_starting; else -> R.string.termux_opening })
                show(text, if (s == TermuxState.Checking) null else getString(R.string.termux_starting_text))
                progress.visibility = View.VISIBLE
                button(primary, null, null)
            }
            TermuxState.NotInstalled -> {
                show(step, getString(R.string.termux_step, 1))
                title.setText(R.string.termux_missing_title)
                show(text, getString(R.string.termux_missing_text))
                show(text2, getString(R.string.termux_play_warning))
                button(primary, R.string.termux_fdroid) { openUrl(Termux.FDROID) }
                button(secondary, R.string.termux_github) { openUrl(Termux.GITHUB) }
            }
            TermuxState.NoPermission -> {
                show(step, getString(R.string.termux_step, 1))
                title.setText(R.string.termux_permission_title)
                show(text, getString(R.string.termux_permission_text))
                button(primary, R.string.termux_allow) { askPermission.launch(Termux.PERMISSION) }
            }
            TermuxState.PermissionBlocked -> {
                show(step, getString(R.string.termux_step, 1))
                title.setText(R.string.termux_blocked_title)
                show(text, getString(R.string.termux_blocked_text))
                button(primary, R.string.termux_app_settings) {
                    startActivity(Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.fromParts("package", packageName, null)))
                }
                button(secondary, R.string.termux_check_again) { check() }
            }
            TermuxState.ExternalAppsOff -> {
                show(step, getString(R.string.termux_step, 2))
                title.setText(R.string.termux_external_title)
                show(text, getString(R.string.termux_external_text))
                show(code, install)
                show(text2, getString(R.string.termux_external_manual))
                show(code2, TermuxFlow.PROPERTY)
                show(note, playNote)
                button(primary, R.string.termux_copy_open) {
                    copy(install)
                    Toast.makeText(this, R.string.termux_copied, Toast.LENGTH_LONG).show()
                    openTermux()
                }
                button(secondary, R.string.termux_check_again) { check() }
            }
            TermuxState.NeedsFirstRun, TermuxState.NotAnswering -> {
                val first = s == TermuxState.NeedsFirstRun
                show(step, getString(R.string.termux_step, 2))
                title.setText(if (first) R.string.termux_firstrun_title else R.string.termux_noanswer_title)
                show(text, getString(if (first) R.string.termux_firstrun_text else R.string.termux_noanswer_text))
                show(note, playNote)
                button(primary, R.string.termux_open) { openTermux() }
                button(secondary, R.string.retry) { check() }
            }
            TermuxState.GeckoMissing -> {
                show(step, getString(R.string.termux_step, 3))
                title.setText(R.string.termux_gecko_missing_title)
                show(text, getString(R.string.termux_gecko_missing_text))
                show(code, install)
                button(primary, R.string.termux_install) {
                    try {
                        Termux.runInSession(this, install, "Install Gecko")
                        move(TermuxEvent.InstallSent)
                    } catch (e: SecurityException) {
                        move(TermuxEvent.Refused)
                    }
                }
                button(secondary, R.string.termux_copy) { copy(install); Toast.makeText(this, R.string.termux_copied, Toast.LENGTH_SHORT).show() }
            }
            TermuxState.Installing -> {
                show(step, getString(R.string.termux_step, 3))
                title.setText(R.string.termux_installing_title)
                show(text, getString(R.string.termux_installing_text))
                show(code, install)
                button(primary, R.string.termux_check_again) { check() }
                button(secondary, R.string.termux_open) { openTermux() }
            }
            is TermuxState.Ready -> {
                title.setText(R.string.termux_ready_title)
                show(text, s.version)
                button(primary, R.string.termux_start) { wantStart = true; check() }
            }
            is TermuxState.Failed -> {
                title.setText(R.string.termux_failed_title)
                show(text, s.why)
                show(text2, getString(R.string.termux_failed_log))
                show(note, playNote)
                button(primary, R.string.retry) { wantStart = true; check() }
                button(secondary, R.string.termux_open) { openTermux() }
            }
        }
    }

    companion object {
        private const val TAG = "GeckoTermux"
        const val EXTRA_START = "start"
        /** Debug builds: the install line to show and send (a local server in tests). */
        const val INSTALL_OVERRIDE = "termuxInstallCommand"

        fun open(ctx: Context, start: Boolean = true) =
            ctx.startActivity(
                Intent(ctx, TermuxActivity::class.java).putExtra(EXTRA_START, start)
                    .apply { if (ctx !is android.app.Activity) addFlags(Intent.FLAG_ACTIVITY_NEW_TASK) },
            )

        /** Stops Gecko in Termux; it stays stopped (This phone doesn't start it again) until started. Blocking. */
        fun stop(ctx: Context) {
            PhoneTermux.setPaused(ctx, true)
            try { Termux.run(ctx, TermuxFlow.STOP, "gecko stop", 10_000) } catch (e: Exception) { Log.w(TAG, "stop: ${e.message}") }
        }

        /** Stops Gecko in Termux and removes it from This phone's machines. Blocking. */
        fun remove(ctx: Context) {
            stop(ctx)
            PhoneTermux.forget(ctx, fromPhone = true)
        }
    }
}
