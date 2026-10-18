package io.github.geckoterm.android

import android.annotation.SuppressLint
import android.content.Context
import android.graphics.Color
import android.graphics.Typeface
import android.graphics.drawable.Drawable
import android.graphics.drawable.GradientDrawable
import android.graphics.drawable.InsetDrawable
import android.graphics.drawable.LayerDrawable
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
 * Arrows and the menu are drawn icons, centred in their keys, not text: a
 * font's arrow characters sit wherever its baseline puts them and differ
 * from phone to phone. Holding Tab sends Shift+Tab (the menu has it too).
 *
 * Esc, Tab, Ctrl, Alt, the four arrows and the menu are always in view:
 * where they don't fit at their natural size (a 360 dp phone) they share
 * the width equally (39.5 dp each there, gaps included, as a thumb sees
 * them), and the rest of the row scrolls on after them. Narrower windows
 * (split screen) scroll rather than go under 39 dp.
 */
class ExtraKeys @JvmOverloads constructor(ctx: Context, attrs: AttributeSet? = null) : LinearLayout(ctx, attrs) {
    data class Key(val name: String, val label: String, val repeat: Boolean = false, val wide: Boolean = false, val description: String? = null, val icon: Int = 0)

    /** Pressed: the web UI's key name (main.js pressKey). */
    var onKey: (String) -> Unit = {}
    var onMenu: (View) -> Unit = {}

    private val keyViews = HashMap<String, TextView>()
    private val handler = Handler(Looper.getMainLooper())
    private var ctrl = false
    private var alt = false
    private var palette = Colors(0xFF1E2620.toInt(), 0xFF2B352D.toInt(), 0xFFD8E0D2.toInt(), 0xFF7CCB3A.toInt(), 0xFF19211A.toInt())

    data class Colors(val bar: Int, val key: Int, val text: Int, val accent: Int, val onAccent: Int)

    // Not baseline aligned: the icon keys have no text, and lining their
    // (empty) baseline up with the text keys' pushed the text keys 3 px down.
    private val row = LinearLayout(ctx).apply { orientation = HORIZONTAL; isBaselineAligned = false }
    private val scroll = HorizontalScrollView(ctx).apply {
        isHorizontalScrollBarEnabled = false
        isFocusable = false
        addView(row)
    }
    private val menu: TextView

    init {
        orientation = HORIZONTAL
        isBaselineAligned = false
        isFocusable = false
        descendantFocusability = FOCUS_BLOCK_DESCENDANTS
        pad(4)
        addView(scroll, LayoutParams(0, LayoutParams.WRAP_CONTENT, 1f))
        for (k in KEYS) row.addView(button(k))
        menu = button(MENU.copy(description = ctx.getString(R.string.key_menu)))
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

    /**
     * The system's text size, up to 1.5x: past that a row of keys would be
     * all scrolling. The keys grow with it.
     */
    private fun textScale() = resources.configuration.fontScale.coerceIn(1f, 1.5f)

    /**
     * A key at its natural size. Each key's view takes the gap after it
     * (its fill is inset by half the gap on both sides), so the space
     * between keys is part of what a thumb can hit.
     */
    private fun size(v: TextView, k: Key) {
        val s = textScale()
        v.setAutoSizeTextTypeWithDefaults(TextView.AUTO_SIZE_TEXT_TYPE_NONE)
        v.setTextSize(TypedValue.COMPLEX_UNIT_DIP, textSize(k))
        v.minWidth = naturalWidth(k)
        v.minimumWidth = v.minWidth
        v.setPadding(dp(6) + GAP_DP.dpPx() / 2, 0, dp(6) + GAP_DP.dpPx() / 2, 0)
        v.layoutParams = LayoutParams(LayoutParams.WRAP_CONTENT, dp(((if (compact) 32 else 38) * s).toInt()))
    }

    private fun textSize(k: Key) = (if (k.label.length > 2) 13f else 16f) * textScale()
    private fun naturalWidth(k: Key) = dp(((if (k.wide) 46 else 38) * textScale()).toInt()) + GAP_DP.dpPx()
    private fun Int.dpPx() = dp(this)

    /** The bar's padding, less the half gap the first and last keys bring. */
    private fun pad(n: Int) {
        val h = maxOf(0, dp(n) - GAP_DP.dpPx() / 2)
        setPadding(h, dp(n), h, dp(n))
    }

    private var fittedFor = -1

    /**
     * Esc to the arrows and the menu in view: at their natural widths when
     * they fit, otherwise all of them the same width, the bar's width shared
     * out, with their text shrunk to fit if it must.
     */
    private fun fit(width: Int) {
        if (width <= 0 || width == fittedFor) return
        fittedFor = width
        val keys = KEYS.take(ALWAYS) + MENU
        val views = (0 until ALWAYS).map { row.getChildAt(it) as TextView } + menu
        val avail = width - paddingLeft - paddingRight
        val natural = keys.sumOf { naturalWidth(it) }
        for ((i, v) in views.withIndex()) {
            val k = keys[i]
            size(v, k)
            if (natural > avail) {
                // Never under a comfortable tap: in a window narrower than a
                // phone (split screen) the row scrolls instead.
                val each = maxOf(avail / views.size, dp(MIN_TAP_DP))
                v.minWidth = 0
                v.minimumWidth = 0
                v.setPadding(GAP_DP.dpPx() / 2 + dp(2), 0, GAP_DP.dpPx() / 2 + dp(2), 0)
                v.layoutParams = (v.layoutParams as LayoutParams).apply { this.width = each }
                if (k.icon == 0) v.setAutoSizeTextTypeUniformWithConfiguration(8, maxOf(9, textSize(k).toInt()), 1, TypedValue.COMPLEX_UNIT_DIP)
            }
        }
    }

    override fun onMeasure(widthMeasureSpec: Int, heightMeasureSpec: Int) {
        fit(MeasureSpec.getSize(widthMeasureSpec))
        super.onMeasure(widthMeasureSpec, heightMeasureSpec)
    }

    /** A phone on its side: slimmer keys, so the terminal keeps its rows. */
    var compact = false
        set(v) {
            if (field == v) return
            field = v
            pad(if (v) 2 else 4)
            resize()
        }

    /** After a change of text size (the activity keeps running through it). */
    fun resize() {
        for (i in 0 until row.childCount) {
            val v = row.getChildAt(i) as TextView
            size(v, KEYS.first { it.name == v.tag })
        }
        size(menu, MENU)
        fittedFor = -1
        requestLayout()
    }

    /**
     * With a hardware keyboard, its own Esc, Tab, Ctrl and arrows: only the
     * menu stays, at the end of the row.
     */
    fun setHardwareKeyboard(on: Boolean) {
        scroll.visibility = if (on) INVISIBLE else VISIBLE
    }

    private fun dp(n: Int) = TypedValue.applyDimension(TypedValue.COMPLEX_UNIT_DIP, n.toFloat(), resources.displayMetrics).toInt()

    @SuppressLint("ClickableViewAccessibility")
    private fun button(k: Key): TextView {
        val v = TextView(context).apply {
            text = k.label
            contentDescription = k.description ?: k.label
            if (k.icon != 0) setTag(R.id.key_icon, context.getDrawable(k.icon)!!.mutate())
            gravity = Gravity.CENTER
            isFocusable = false
            isFocusableInTouchMode = false
            typeface = if (k.label.length > 1 && k.label[0].isLetter()) Typeface.DEFAULT else Typeface.MONOSPACE
            tag = k.name
        }
        size(v, k)
        var repeating: Runnable? = null
        var held: Runnable? = null
        var heldFired = false
        v.setOnTouchListener { view, ev ->
            when (ev.actionMasked) {
                MotionEvent.ACTION_DOWN -> {
                    view.isPressed = true
                    view.performHapticFeedback(HapticFeedbackConstants.KEYBOARD_TAP)
                    if (k.name == "tab") {
                        // Tab on release; held, it's Shift+Tab (Claude Code's mode switch).
                        heldFired = false
                        val r = Runnable {
                            heldFired = true
                            view.performHapticFeedback(HapticFeedbackConstants.LONG_PRESS)
                            press(SHIFT_TAB)
                        }
                        held = r
                        handler.postDelayed(r, HOLD_MS)
                        return@setOnTouchListener true
                    }
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
                    held?.let {
                        handler.removeCallbacks(it)
                        if (!heldFired && ev.actionMasked == MotionEvent.ACTION_UP) press(k)
                    }
                    held = null
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
            val half = GAP_DP.dpPx() / 2
            val fill = InsetDrawable(GradientDrawable().apply {
                cornerRadius = dp(6).toFloat()
                setColor(if (on) palette.accent else palette.key)
            }, half, 0, half, 0)
            val fg = if (on) palette.onAccent else palette.text
            v.background = (v.getTag(R.id.key_icon) as Drawable?)?.let { icon -> iconOn(fill, icon, fg) } ?: fill
            v.setTextColor(fg)
        }
    }

    /** The key's fill with its icon on top, at the exact centre, in the text colour. */
    private fun iconOn(fill: Drawable, icon: Drawable, color: Int): Drawable {
        icon.setTint(color)
        val side = dp((16 * textScale()).toInt())
        return LayerDrawable(arrayOf(fill, icon)).apply {
            setLayerGravity(1, Gravity.CENTER)
            setLayerSize(1, side, side)
        }
    }

    fun isOn(name: String) = (name == "ctrl" && ctrl) || (name == "alt" && alt)

    companion object {
        val KEYS = listOf(
            Key("esc", "esc", wide = true),
            Key("tab", "tab", wide = true, description = "Tab (hold for Shift+Tab)"),
            Key("ctrl", "ctrl", wide = true),
            Key("alt", "alt", wide = true),
            Key("left", "", repeat = true, description = "Left", icon = R.drawable.key_left),
            Key("up", "", repeat = true, description = "Up", icon = R.drawable.key_up),
            Key("down", "", repeat = true, description = "Down", icon = R.drawable.key_down),
            Key("right", "", repeat = true, description = "Right", icon = R.drawable.key_right),
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

        val MENU = Key("menu", "", icon = R.drawable.key_menu)

        /** Sent by holding Tab (and from the menu); not a key of its own. */
        val SHIFT_TAB = Key("shift-tab", "", description = "Shift+Tab")

        /** The keys always in view, from the start of KEYS: Esc to the right arrow. */
        const val ALWAYS = 8

        /** The narrowest a key gets (its share of the gaps included). */
        const val MIN_TAP_DP = 39

        /** Between keys. */
        const val GAP_DP = 4

        /** How long Tab is held before it's Shift+Tab. */
        const val HOLD_MS = 450L

        /** What each key sends when the web UI is too old to have pressKey. */
        val FALLBACK = mapOf(
            "esc" to "\u001b", "tab" to "\t", "shift-tab" to "\u001b[Z", "up" to "\u001b[A", "down" to "\u001b[B", "left" to "\u001b[D", "right" to "\u001b[C",
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
