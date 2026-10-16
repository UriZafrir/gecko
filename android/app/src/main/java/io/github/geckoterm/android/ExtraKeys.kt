package io.github.geckoterm.android

import android.annotation.SuppressLint
import android.content.Context
import android.graphics.Color
import android.graphics.Typeface
import android.graphics.drawable.GradientDrawable
import android.os.Handler
import android.os.Looper
import android.util.AttributeSet
import android.util.TypedValue
import android.view.Gravity
import android.view.HapticFeedbackConstants
import android.view.MotionEvent
import android.view.View
import android.widget.HorizontalScrollView
import android.widget.LinearLayout
import android.widget.TextView

/**
 * The row of keys a phone keyboard lacks, above the keyboard: Esc, Tab,
 * sticky Ctrl and Alt, arrows (they repeat while held) and the symbols that
 * are awkward to reach. Keys never take focus, so the keyboard stays up.
 */
class ExtraKeys @JvmOverloads constructor(ctx: Context, attrs: AttributeSet? = null) : LinearLayout(ctx, attrs) {
    data class Key(val name: String, val label: String, val repeat: Boolean = false, val wide: Boolean = false, val description: String? = null)

    /** Pressed: the web UI's key name (main.js pressKey). */
    var onKey: (String) -> Unit = {}
    var onMenu: (View) -> Unit = {}

    private val keyViews = HashMap<String, TextView>()
    private val handler = Handler(Looper.getMainLooper())
    private var ctrl = false
    private var alt = false
    private var palette = Colors(0xFF1E2620.toInt(), 0xFF2B352D.toInt(), 0xFFD8E0D2.toInt(), 0xFF7CCB3A.toInt(), 0xFF19211A.toInt())

    data class Colors(val bar: Int, val key: Int, val text: Int, val accent: Int, val onAccent: Int)

    private val row = LinearLayout(ctx).apply { orientation = HORIZONTAL }
    private val scroll = HorizontalScrollView(ctx).apply {
        isHorizontalScrollBarEnabled = false
        isFocusable = false
        addView(row)
    }
    private val menu: TextView

    init {
        orientation = HORIZONTAL
        isFocusable = false
        descendantFocusability = FOCUS_BLOCK_DESCENDANTS
        val pad = dp(4)
        setPadding(pad, pad, pad, pad)
        addView(scroll, LayoutParams(0, LayoutParams.WRAP_CONTENT, 1f))
        for (k in KEYS) row.addView(button(k))
        menu = button(Key("menu", "⋮", description = ctx.getString(R.string.key_menu)))
        menu.setOnTouchListener(null)
        menu.setOnClickListener { onMenu(it) }
        addView(menu)
        paint()
    }

    fun setMods(ctrl: Boolean, alt: Boolean) {
        this.ctrl = ctrl
        this.alt = alt
        paint()
    }

    fun setColors(c: Colors) {
        palette = c
        paint()
    }

    private fun dp(n: Int) = TypedValue.applyDimension(TypedValue.COMPLEX_UNIT_DIP, n.toFloat(), resources.displayMetrics).toInt()

    @SuppressLint("ClickableViewAccessibility")
    private fun button(k: Key): TextView {
        val v = TextView(context).apply {
            text = k.label
            contentDescription = k.description ?: k.label
            gravity = Gravity.CENTER
            isFocusable = false
            isFocusableInTouchMode = false
            typeface = if (k.label.length > 1 && k.label[0].isLetter()) Typeface.DEFAULT else Typeface.MONOSPACE
            setTextSize(TypedValue.COMPLEX_UNIT_SP, if (k.label.length > 2) 13f else 16f)
            minWidth = dp(if (k.wide) 52 else 40)
            setPadding(dp(8), 0, dp(8), 0)
            tag = k.name
        }
        v.layoutParams = LayoutParams(LayoutParams.WRAP_CONTENT, dp(38)).apply { marginEnd = dp(4) }
        var repeating: Runnable? = null
        v.setOnTouchListener { view, ev ->
            when (ev.actionMasked) {
                MotionEvent.ACTION_DOWN -> {
                    view.isPressed = true
                    view.performHapticFeedback(HapticFeedbackConstants.KEYBOARD_TAP)
                    press(k)
                    if (k.repeat) {
                        val r = object : Runnable {
                            override fun run() {
                                press(k)
                                handler.postDelayed(this, 60)
                            }
                        }
                        repeating = r
                        handler.postDelayed(r, 420)
                    }
                }
                MotionEvent.ACTION_UP, MotionEvent.ACTION_CANCEL -> {
                    view.isPressed = false
                    repeating?.let { handler.removeCallbacks(it) }
                    repeating = null
                    if (ev.actionMasked == MotionEvent.ACTION_UP) view.performClick()
                }
            }
            true
        }
        keyViews[k.name] = v
        return v
    }

    private fun press(k: Key) {
        // Sticky keys light up at once; the web UI confirms (GeckoAndroid.mods).
        if (k.name == "ctrl") ctrl = !ctrl
        if (k.name == "alt") alt = !alt
        paint()
        onKey(k.name)
    }

    private fun paint() {
        setBackgroundColor(palette.bar)
        for ((name, v) in keyViews) {
            val on = (name == "ctrl" && ctrl) || (name == "alt" && alt)
            v.background = GradientDrawable().apply {
                cornerRadius = dp(6).toFloat()
                setColor(if (on) palette.accent else palette.key)
            }
            v.setTextColor(if (on) palette.onAccent else palette.text)
        }
    }

    fun isOn(name: String) = (name == "ctrl" && ctrl) || (name == "alt" && alt)

    companion object {
        val KEYS = listOf(
            Key("esc", "esc", wide = true),
            Key("tab", "tab", wide = true),
            Key("ctrl", "ctrl", wide = true),
            Key("alt", "alt", wide = true),
            Key("left", "←", repeat = true, description = "Left"),
            Key("up", "↑", repeat = true, description = "Up"),
            Key("down", "↓", repeat = true, description = "Down"),
            Key("right", "→", repeat = true, description = "Right"),
            Key("pipe", "|"),
            Key("tilde", "~"),
            Key("slash", "/"),
            Key("dash", "-"),
            Key("ctrl-c", "^C", description = "Ctrl+C"),
            Key("home", "home"),
            Key("end", "end"),
            Key("pgup", "pgup"),
            Key("pgdn", "pgdn"),
            Key("paste", "paste"),
            Key("select", "select"),
            Key("compose", "compose"),
        )

        /** What each key sends when the web UI is too old to have pressKey. */
        val FALLBACK = mapOf(
            "esc" to "\u001b", "tab" to "\t", "up" to "\u001b[A", "down" to "\u001b[B", "left" to "\u001b[D", "right" to "\u001b[C",
            "pipe" to "|", "tilde" to "~", "slash" to "/", "dash" to "-", "ctrl-c" to "\u0003",
            "home" to "\u001b[H", "end" to "\u001b[F", "pgup" to "\u001b[5~", "pgdn" to "\u001b[6~",
        )

        fun parseColor(s: String?, fallback: Int): Int = try {
            if (s.isNullOrEmpty()) fallback else Color.parseColor(if (s.length == 9 && s.startsWith("#")) "#" + s.substring(7) + s.substring(1, 7) else s)
        } catch (e: Exception) {
            fallback
        }
    }
}
