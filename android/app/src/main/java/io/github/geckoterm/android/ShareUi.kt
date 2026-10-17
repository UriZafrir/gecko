package io.github.geckoterm.android

import android.app.Activity
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.graphics.Bitmap
import android.graphics.Color
import android.util.TypedValue
import android.view.Gravity
import android.widget.ImageView
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView
import android.widget.Toast
import androidx.appcompat.app.AlertDialog
import com.google.zxing.BarcodeFormat
import com.google.zxing.EncodeHintType
import com.google.zxing.qrcode.QRCodeWriter

/** "Allow other devices" for the Gecko running on this phone: turning it on and off, and its link and QR code. */
object ShareUi {
    fun allowed(ctx: Context) = Gecko.prefs(ctx).getBoolean(PhoneService.ALLOW_OTHERS, false)

    /** Asks first (it opens this phone's terminal to the network), then shows the link. */
    fun turnOn(a: Activity, done: () -> Unit = {}) {
        AlertDialog.Builder(a)
            .setTitle(R.string.share_title)
            .setMessage(R.string.share_warning)
            .setPositiveButton(R.string.share_allow) { _, _ ->
                Thread {
                    val result = runCatching {
                        if (!LocalGecko.start(a)) throw IllegalStateException(a.getString(R.string.phone_failed))
                        LocalGecko.share(a, true)
                    }
                    a.runOnUiThread {
                        result.onSuccess {
                            Gecko.prefs(a).edit().putBoolean(PhoneService.ALLOW_OTHERS, true).apply()
                            PhoneService.refresh(a)
                            showLinks(a, it)
                        }.onFailure { e ->
                            AlertDialog.Builder(a).setMessage(a.getString(R.string.share_failed, e.message ?: "")).setPositiveButton(android.R.string.ok, null).show()
                        }
                        done()
                    }
                }.start()
            }
            .setNegativeButton(android.R.string.cancel, null)
            .show()
    }

    fun turnOff(a: Activity, done: () -> Unit = {}) {
        Thread {
            runCatching { LocalGecko.share(a, false) }
            Gecko.prefs(a).edit().putBoolean(PhoneService.ALLOW_OTHERS, false).apply()
            a.runOnUiThread {
                if (LocalGecko.running(a)) PhoneService.refresh(a)
                Toast.makeText(a, R.string.share_off, Toast.LENGTH_LONG).show()
                done()
            }
        }.start()
    }

    /** The current links, as in Gecko's own "Open on another device". */
    fun showLinks(a: Activity, known: LocalGecko.Share? = null) {
        Thread {
            val st = known ?: LocalGecko.shareState(a)
            val links = st.links(LocalGecko.lanAddresses())
            a.runOnUiThread { linksDialog(a, st, links) }
        }.start()
    }

    private fun linksDialog(a: Activity, st: LocalGecko.Share, links: List<String>) {
        val dp = { n: Int -> TypedValue.applyDimension(TypedValue.COMPLEX_UNIT_DIP, n.toFloat(), a.resources.displayMetrics).toInt() }
        val col = LinearLayout(a).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(24), dp(8), dp(24), dp(8))
        }
        fun text(s: String, size: Float = 14f, mono: Boolean = false) = TextView(a).apply {
            text = s
            setTextSize(TypedValue.COMPLEX_UNIT_SP, size)
            if (mono) typeface = android.graphics.Typeface.MONOSPACE
            setTextIsSelectable(true)
        }
        when {
            !st.on -> col.addView(text(a.getString(R.string.share_not_on)))
            links.isEmpty() -> col.addView(text(a.getString(R.string.share_no_network)))
            else -> {
                col.addView(text(a.getString(R.string.share_scan)))
                for (link in links) {
                    col.addView(ImageView(a).apply {
                        setImageBitmap(qr(link, dp(220)))
                        contentDescription = link
                        setPadding(0, dp(16), 0, dp(4))
                    }, LinearLayout.LayoutParams(LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT).apply { gravity = Gravity.CENTER_HORIZONTAL })
                    col.addView(text(link, 12f, mono = true))
                    col.addView(TextView(a).apply {
                        text = a.getString(R.string.share_copy)
                        setTextColor(0xFF3D8C1C.toInt())
                        setTextSize(TypedValue.COMPLEX_UNIT_SP, 15f)
                        setPadding(0, dp(10), 0, dp(10))
                        gravity = Gravity.CENTER
                        setOnClickListener {
                            (a.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager).setPrimaryClip(ClipData.newPlainText("Gecko", link))
                            Toast.makeText(a, R.string.copied, Toast.LENGTH_SHORT).show()
                        }
                    })
                }
                col.addView(text(a.getString(R.string.share_footer), 13f))
            }
        }
        AlertDialog.Builder(a)
            .setTitle(R.string.share_links_title)
            .setView(ScrollView(a).apply { addView(col) })
            .setPositiveButton(R.string.done, null)
            .apply { if (st.on) setNegativeButton(R.string.share_turn_off) { _, _ -> turnOff(a) } }
            .show()
    }

    fun qr(text: String, size: Int): Bitmap {
        val m = QRCodeWriter().encode(text, BarcodeFormat.QR_CODE, size, size, mapOf(EncodeHintType.MARGIN to 2))
        val px = IntArray(size * size) { i -> if (m.get(i % size, i / size)) Color.BLACK else Color.WHITE }
        return Bitmap.createBitmap(px, size, size, Bitmap.Config.ARGB_8888)
    }
}
