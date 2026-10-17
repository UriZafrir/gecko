package io.github.geckoterm.android

import android.inputmethodservice.InputMethodService
import android.view.inputmethod.EditorInfo

/**
 * Debug builds only, off unless a device test enables it: a keyboard with
 * no keys that the test drives through its input connection, as Gboard's
 * voice typing does (setComposingText, commitText, deleteSurroundingText...).
 */
class ScriptIme : InputMethodService() {
    override fun onCreate() {
        super.onCreate()
        current = this
    }

    override fun onDestroy() {
        if (current === this) current = null
        super.onDestroy()
    }

    override fun onStartInput(attribute: EditorInfo?, restarting: Boolean) {
        super.onStartInput(attribute, restarting)
        started++
    }

    companion object {
        @Volatile var current: ScriptIme? = null
        @Volatile var started = 0
    }
}
