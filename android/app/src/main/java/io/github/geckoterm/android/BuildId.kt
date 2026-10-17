package io.github.geckoterm.android

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.text.SpannableString
import android.text.Spanned
import android.text.style.ForegroundColorSpan
import android.text.style.RelativeSizeSpan
import android.widget.Toast
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale

/**
 * Which build this is: the commit (with -dirty when the tree had changes)
 * and when it was built, e.g. "a1b2c3d · Oct 17 02:15" in this phone's time
 * zone. Set at build time (BuildConfig, from `make android-gecko`); the web
 * UI and gecko inside carry the same one.
 */
object BuildId {
    private val TIME = DateTimeFormatter.ofPattern("MMM d HH:mm", Locale.US)

    fun format(commit: String, time: String, zone: ZoneId = ZoneId.systemDefault()): String {
        val c = commit.ifEmpty { "unknown" }
        val t = try { Instant.parse(time) } catch (e: Exception) { return c }
        return "$c · " + TIME.withZone(zone).format(t)
    }

    val current: String get() = format(BuildConfig.BUILD_COMMIT, BuildConfig.BUILD_TIME)

    /** The id as small, muted text (the ⋮ menu's last line). */
    fun small(ctx: Context, id: String): CharSequence = SpannableString(id).apply {
        val muted = ctx.obtainStyledAttributes(intArrayOf(android.R.attr.textColorSecondary)).run {
            try { getColor(0, 0xFF888888.toInt()) } finally { recycle() }
        }
        setSpan(RelativeSizeSpan(0.8f), 0, length, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
        setSpan(ForegroundColorSpan(muted), 0, length, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
    }

    /** Tapped: copies "Gecko for Android <id>". */
    fun copy(ctx: Context) {
        val cb = ctx.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
        cb.setPrimaryClip(ClipData.newPlainText("Gecko build", "Gecko for Android " + current))
        // Android 13 and later say so themselves.
        if (android.os.Build.VERSION.SDK_INT < 33) Toast.makeText(ctx, R.string.build_copied, Toast.LENGTH_SHORT).show()
    }
}
