package io.github.geckoterm.android

import org.junit.Assert.assertEquals
import org.junit.Test

class StatusNoteTest {
    private fun t(phone: String?, names: List<String>?, trying: Int = names?.size ?: 0) =
        StatusNote.text(phone, names?.let { StatusNote.Watch(it, trying) })

    @Test fun watching() {
        assertEquals(StatusNote.Text("Watching agents on ubunty", "You'll be notified when an agent needs you."), t(null, listOf("ubunty")))
        assertEquals("Watching agents on 2 machines", t(null, listOf("ubunty", "this phone")).title)
        assertEquals("Watching agents on ubunty", t(null, listOf("ubunty", "ubunty"), 2).title) // the same one two ways
    }

    @Test fun notConnected() {
        assertEquals("Not connected, retrying", t(null, emptyList(), 1).title)
        assertEquals(StatusNote.Text("No machines to watch", "Add a machine, or start Gecko on this phone."), t(null, emptyList(), 0))
    }

    @Test fun withThisPhone() {
        assertEquals(StatusNote.Text("Watching agents on 2 machines", "Gecko running on this phone"), t("Gecko running on this phone", listOf("this phone", "ubunty")))
        assertEquals(StatusNote.Text("Gecko on this phone", "Gecko running on this phone"), t("Gecko running on this phone", null))
    }
}
