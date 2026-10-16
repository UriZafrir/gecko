package io.github.geckoterm.android

import android.Manifest
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import android.view.LayoutInflater
import android.view.Menu
import android.view.MenuItem
import android.view.View
import android.view.ViewGroup
import android.widget.EditText
import android.widget.ImageButton
import android.widget.PopupMenu
import android.widget.TextView
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AlertDialog
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.ContextCompat
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat
import androidx.recyclerview.widget.LinearLayoutManager
import androidx.recyclerview.widget.RecyclerView
import com.google.android.material.appbar.MaterialToolbar
import com.google.android.material.snackbar.Snackbar
import com.journeyapps.barcodescanner.ScanContract
import com.journeyapps.barcodescanner.ScanOptions
import okhttp3.OkHttpClient
import okhttp3.Request
import org.json.JSONObject
import java.util.concurrent.TimeUnit

/** The machines this phone knows; add one by scanning Gecko's QR code or pasting its link. */
class ConnectActivity : AppCompatActivity() {
    private lateinit var list: RecyclerView
    private lateinit var empty: View
    private val store by lazy { Gecko.machines(this) }
    private val http by lazy { OkHttpClient.Builder().callTimeout(6, TimeUnit.SECONDS).build() }

    private val scan = registerForActivityResult(ScanContract()) { r -> r.contents?.let { addFromText(it) } }
    private val pickImage = registerForActivityResult(ActivityResultContracts.GetContent()) { uri ->
        if (uri == null) return@registerForActivityResult
        Thread {
            val text = try { QrImage.decode(this, uri) } catch (e: Exception) { null }
            runOnUiThread { if (text == null) message(getString(R.string.no_qr)) else addFromText(text) }
        }.start()
    }
    private val askNotifications = registerForActivityResult(ActivityResultContracts.RequestPermission()) { ok ->
        if (ok) WatchService.start(this) else message(getString(R.string.notify_blocked))
        invalidateOptionsMenu()
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        enableEdgeToEdge()
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_connect)
        val bar = findViewById<MaterialToolbar>(R.id.toolbar)
        setSupportActionBar(bar)
        ViewCompat.setOnApplyWindowInsetsListener(findViewById(R.id.root)) { v, insets ->
            val b = insets.getInsets(WindowInsetsCompat.Type.systemBars() or WindowInsetsCompat.Type.displayCutout() or WindowInsetsCompat.Type.ime())
            v.setPadding(b.left, b.top, b.right, b.bottom)
            WindowInsetsCompat.CONSUMED
        }
        list = findViewById(R.id.list)
        empty = findViewById(R.id.empty)
        list.layoutManager = LinearLayoutManager(this)
        findViewById<View>(R.id.scan).setOnClickListener { startScan() }
        findViewById<View>(R.id.paste).setOnClickListener { pasteDialog() }
        Notifier.channels(this)
        if (Gecko.prefs(this).getBoolean(Gecko.BACKGROUND, false)) WatchService.refresh(this)
    }

    override fun onResume() {
        super.onResume()
        render()
    }

    override fun onCreateOptionsMenu(menu: Menu): Boolean {
        menuInflater.inflate(R.menu.connect, menu)
        menu.findItem(R.id.background).isChecked = Gecko.prefs(this).getBoolean(Gecko.BACKGROUND, false)
        return true
    }

    override fun onOptionsItemSelected(item: MenuItem): Boolean {
        when (item.itemId) {
            R.id.scan_image -> pickImage.launch("image/*")
            R.id.background -> {
                if (item.isChecked) WatchService.stop(this)
                else if (Build.VERSION.SDK_INT >= 33 && ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
                    askNotifications.launch(Manifest.permission.POST_NOTIFICATIONS)
                } else WatchService.start(this)
                invalidateOptionsMenu()
            }
            else -> return super.onOptionsItemSelected(item)
        }
        return true
    }

    /** Whether Gecko runs on this phone right now (checked off the main thread). */
    private var localRunning = false

    private fun render() {
        val saved = store.list().sortedByDescending { it.lastUsed }
        val local = LocalGecko.available(this)
        // This phone comes first; with no computers yet, how to add one follows.
        val machines = (if (local) listOf(Machine(id = LocalGecko.ID, name = getString(R.string.this_phone), url = "http://127.0.0.1/", token = "")) else emptyList()) + saved
        empty.visibility = if (saved.isEmpty()) View.VISIBLE else View.GONE
        list.visibility = if (machines.isEmpty()) View.GONE else View.VISIBLE
        list.layoutParams = (list.layoutParams as android.widget.LinearLayout.LayoutParams).apply {
            if (saved.isEmpty()) { height = ViewGroup.LayoutParams.WRAP_CONTENT; weight = 0f } else { height = 0; weight = 1f }
        }
        list.adapter = Adapter(machines)
        if (local) Thread {
            val running = LocalGecko.running(this)
            runOnUiThread { if (running != localRunning) { localRunning = running; list.adapter?.notifyItemChanged(0) } }
        }.start()
    }

    private fun startScan() {
        scan.launch(
            ScanOptions()
                .setDesiredBarcodeFormats(ScanOptions.QR_CODE)
                .setPrompt(getString(R.string.scan_prompt))
                .setBeepEnabled(false)
                .setOrientationLocked(false),
        )
    }

    private fun pasteDialog(initial: String = "") {
        val v = LayoutInflater.from(this).inflate(R.layout.dialog_link, null)
        val field = v.findViewById<EditText>(R.id.link)
        val clip = (getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager).primaryClip
        val clipText = clip?.takeIf { it.itemCount > 0 }?.getItemAt(0)?.coerceToText(this)?.toString().orEmpty()
        field.setText(initial.ifEmpty { if (clipText.contains("token=")) clipText.trim() else "" })
        AlertDialog.Builder(this)
            .setTitle(R.string.paste_title)
            .setView(v)
            .setPositiveButton(R.string.add) { _, _ -> addFromText(field.text.toString()) }
            .setNegativeButton(android.R.string.cancel, null)
            .show()
    }

    /** A scanned or pasted link: check it, ask the machine who it is, then save it. */
    fun addFromText(text: String) {
        val link = try { LinkParser.parse(text) } catch (e: LinkException) { return message(e.message ?: "") }
        if (link.token == null) return message(getString(R.string.err_link_no_token))
        val wait = Snackbar.make(list, getString(R.string.checking, link.host), Snackbar.LENGTH_INDEFINITE).also { it.show() }
        Thread {
            val result = probe(link)
            runOnUiThread {
                wait.dismiss()
                if (isFinishing) return@runOnUiThread
                when (result) {
                    is Probe.Refused -> message(result.why)
                    is Probe.Ok -> nameDialog(link, result.name, null)
                    is Probe.Unreachable -> AlertDialog.Builder(this)
                        .setTitle(getString(R.string.unreachable_title, link.host))
                        .setMessage(getString(R.string.unreachable_text, result.why))
                        .setPositiveButton(R.string.save_anyway) { _, _ -> nameDialog(link, null, null) }
                        .setNegativeButton(android.R.string.cancel, null)
                        .show()
                }
            }
        }.start()
    }

    private sealed class Probe {
        class Ok(val name: String?) : Probe()
        class Refused(val why: String) : Probe()
        class Unreachable(val why: String) : Probe()
    }

    private fun probe(link: GeckoLink): Probe {
        if (!link.https && !NetPolicy.cleartextAllowedResolving(link.host)) return Probe.Refused(getString(R.string.err_cleartext, link.host))
        return try {
            val req = Request.Builder().url(link.baseUrl + "api/info").header("Authorization", "Bearer " + link.token).build()
            http.newCall(req).execute().use { r ->
                when {
                    r.code == 401 || r.code == 403 -> Probe.Refused(getString(R.string.err_token))
                    !r.isSuccessful -> Probe.Unreachable("HTTP ${r.code}")
                    else -> Probe.Ok(try { JSONObject(r.body?.string().orEmpty()).optString("host").ifEmpty { null } } catch (e: Exception) { null })
                }
            }
        } catch (e: Exception) {
            Probe.Unreachable(e.message ?: e.javaClass.simpleName)
        }
    }

    /** Names a new machine (or edits one). Scanning a known machine again just updates its token. */
    private fun nameDialog(link: GeckoLink, suggested: String?, editing: Machine?) {
        val known = editing ?: store.findByUrl(link.baseUrl)
        if (known != null && editing == null) {
            store.save(known.copy(token = link.token ?: known.token))
            WatchService.refresh(this)
            render()
            message(getString(R.string.updated, known.name))
            return
        }
        val v = LayoutInflater.from(this).inflate(R.layout.dialog_name, null)
        val field = v.findViewById<EditText>(R.id.name)
        field.setText(known?.name ?: suggested ?: link.host)
        field.setSelection(field.text.length)
        AlertDialog.Builder(this)
            .setTitle(if (editing != null) R.string.edit_title else R.string.name_title)
            .setMessage(link.baseUrl)
            .setView(v)
            .setPositiveButton(if (editing != null) R.string.save else R.string.add_open) { _, _ ->
                val name = field.text.toString().trim().ifEmpty { suggested ?: link.host }
                val m = (editing ?: Machine(name = name, url = link.baseUrl, token = link.token ?: ""))
                    .copy(name = name, url = link.baseUrl, token = link.token ?: editing?.token ?: "")
                store.save(m)
                WatchService.refresh(this)
                render()
                if (editing == null) TerminalActivity.open(this, m)
            }
            .setNegativeButton(android.R.string.cancel, null)
            .show()
    }

    private fun edit(m: Machine) {
        val v = LayoutInflater.from(this).inflate(R.layout.dialog_edit, null)
        val name = v.findViewById<EditText>(R.id.name)
        val link = v.findViewById<EditText>(R.id.link)
        name.setText(m.name)
        link.setText(m.url)
        AlertDialog.Builder(this)
            .setTitle(R.string.edit_title)
            .setView(v)
            .setPositiveButton(R.string.save) { _, _ ->
                val parsed = try { LinkParser.parse(link.text.toString()) } catch (e: LinkException) { return@setPositiveButton message(e.message ?: "") }
                store.save(m.copy(name = name.text.toString().trim().ifEmpty { m.name }, url = parsed.baseUrl, token = parsed.token ?: m.token))
                WatchService.refresh(this)
                render()
            }
            .setNegativeButton(android.R.string.cancel, null)
            .show()
    }

    private fun remove(m: Machine) {
        AlertDialog.Builder(this)
            .setTitle(getString(R.string.remove_title, m.name))
            .setMessage(R.string.remove_text)
            .setPositiveButton(R.string.remove) { _, _ ->
                store.remove(m.id)
                WatchService.refresh(this)
                render()
            }
            .setNegativeButton(android.R.string.cancel, null)
            .show()
    }

    private fun message(text: String) {
        AlertDialog.Builder(this).setMessage(text).setPositiveButton(android.R.string.ok, null).show()
    }

    private inner class Adapter(val items: List<Machine>) : RecyclerView.Adapter<Adapter.Row>() {
        inner class Row(v: View) : RecyclerView.ViewHolder(v) {
            val name: TextView = v.findViewById(R.id.name)
            val addr: TextView = v.findViewById(R.id.addr)
            val more: ImageButton = v.findViewById(R.id.more)
            val icon: android.widget.ImageView = v.findViewById(R.id.icon)
        }

        private fun bindLocal(h: Row) {
            val a = this@ConnectActivity
            h.icon.setImageResource(R.drawable.ic_phone)
            h.name.text = getString(R.string.this_phone)
            h.addr.text = getString(
                when {
                    !localRunning -> R.string.phone_tap_to_start
                    ShareUi.allowed(a) -> R.string.phone_running_shared
                    else -> R.string.phone_running
                },
            )
            h.itemView.setOnClickListener {
                startActivity(Intent(a, TerminalActivity::class.java).putExtra(TerminalActivity.EXTRA_MACHINE, LocalGecko.ID))
            }
            h.more.setOnClickListener { anchor ->
                PopupMenu(a, anchor).apply {
                    val allowed = ShareUi.allowed(a)
                    menu.add(0, 1, 0, R.string.share_toggle).apply { isCheckable = true; isChecked = allowed }
                    if (allowed) menu.add(0, 2, 1, R.string.share_links_title)
                    if (localRunning) menu.add(0, 3, 2, R.string.phone_stop_long)
                    setOnMenuItemClickListener {
                        when (it.itemId) {
                            1 -> if (allowed) ShareUi.turnOff(a) { render() } else ShareUi.turnOn(a) { render() }
                            2 -> ShareUi.showLinks(a)
                            3 -> {
                                PhoneService.stop(a)
                                localRunning = false
                                list.postDelayed({ render() }, 1500)
                            }
                        }
                        true
                    }
                    show()
                }
            }
        }

        override fun onCreateViewHolder(parent: ViewGroup, viewType: Int) =
            Row(LayoutInflater.from(parent.context).inflate(R.layout.item_machine, parent, false))

        override fun getItemCount() = items.size

        override fun onBindViewHolder(h: Row, i: Int) {
            val m = items[i]
            if (m.id == LocalGecko.ID) return bindLocal(h)
            h.icon.setImageResource(R.drawable.ic_machine)
            h.name.text = m.name
            h.addr.text = if (m.token.isEmpty()) getString(R.string.needs_link, m.url) else m.url.removeSuffix("/")
            h.itemView.setOnClickListener {
                if (m.token.isEmpty()) pasteDialog() else TerminalActivity.open(this@ConnectActivity, m)
            }
            h.more.setOnClickListener { anchor ->
                PopupMenu(this@ConnectActivity, anchor).apply {
                    menu.add(0, 1, 0, R.string.edit)
                    menu.add(0, 2, 1, R.string.remove)
                    setOnMenuItemClickListener {
                        if (it.itemId == 1) edit(m) else remove(m)
                        true
                    }
                    show()
                }
            }
        }
    }
}
