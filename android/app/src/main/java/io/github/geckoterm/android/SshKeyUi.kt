package io.github.geckoterm.android

import android.app.Activity
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.util.TypedValue
import android.widget.LinearLayout
import android.widget.TextView
import android.widget.Toast
import androidx.appcompat.app.AlertDialog

/** "SSH key": this phone's public key, to put in ~/.ssh/authorized_keys on the machines it should reach. */
object SshKeyUi {
    fun show(a: Activity) {
        if (!LocalGecko.available(a)) return
        Thread {
            val result = runCatching { LocalGecko.sshPublicKey(a) }
            a.runOnUiThread {
                result.onFailure { e ->
                    AlertDialog.Builder(a).setMessage(a.getString(R.string.ssh_key_failed, e.message ?: "")).setPositiveButton(android.R.string.ok, null).show()
                }.onSuccess { key -> dialog(a, key) }
            }
        }.start()
    }

    private fun dialog(a: Activity, key: String) {
        val dp = { n: Int -> TypedValue.applyDimension(TypedValue.COMPLEX_UNIT_DIP, n.toFloat(), a.resources.displayMetrics).toInt() }
        val col = LinearLayout(a).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(24), dp(8), dp(24), 0)
        }
        col.addView(TextView(a).apply { text = a.getString(R.string.ssh_key_text) })
        col.addView(TextView(a).apply {
            text = key
            typeface = android.graphics.Typeface.MONOSPACE
            setTextSize(TypedValue.COMPLEX_UNIT_SP, 12f)
            setTextIsSelectable(true)
            setPadding(0, dp(12), 0, dp(4))
        })
        AlertDialog.Builder(a)
            .setTitle(R.string.ssh_key_title)
            .setView(col)
            .setPositiveButton(R.string.share_copy_key) { _, _ ->
                (a.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager).setPrimaryClip(ClipData.newPlainText("SSH key", key))
                Toast.makeText(a, R.string.copied, Toast.LENGTH_SHORT).show()
            }
            .setNeutralButton(R.string.ssh_key_share) { _, _ ->
                a.startActivity(Intent.createChooser(Intent(Intent.ACTION_SEND).setType("text/plain").putExtra(Intent.EXTRA_TEXT, key), null))
            }
            .setNegativeButton(R.string.done, null)
            .show()
    }
}
