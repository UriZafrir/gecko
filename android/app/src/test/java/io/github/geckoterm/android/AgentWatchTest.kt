package io.github.geckoterm.android

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class AgentWatchTest {
    private fun session(id: String, status: String?, detail: String = "", name: String = "", choices: String = "") =
        """{"id":"$id","host":"devbox","workspace":"default","name":"$name","kind":"agent","cwd":"/home/me/app"${
            if (status == null) "" else ""","agent":{"name":"claude","status":"$status","detail":"$detail"${if (choices.isEmpty()) "" else ""","choices":$choices"""}}"""
        }}"""

    @Test fun needsInputAfterWorking() {
        val w = AgentWatch()
        assertTrue(w.onMessage("""{"t":"sessions","sessions":[${session("a", "working")}]}""").isEmpty())
        val ev = w.onMessage("""{"t":"session","session":${session("a", "needs-input", "Do you want to run this command?", choices = """[{"key":"1","label":"Yes"},{"key":"2","label":"No"}]""")}}""")
        assertEquals(1, ev.size)
        assertEquals("claude needs you · claude", ev[0].title)
        assertEquals("Do you want to run this command?", ev[0].body)
        assertEquals(listOf("1" to "Yes", "2" to "No"), ev[0].choices)
        assertTrue(ev[0].needsInput)
        // The same state again is not news.
        assertTrue(w.onMessage("""{"t":"session","session":${session("a", "needs-input")}}""").isEmpty())
    }

    @Test fun finished() {
        val w = AgentWatch()
        w.onMessage("""{"t":"session","session":${session("a", "working", name = "api")}}""")
        val ev = w.onMessage("""{"t":"session","session":${session("a", "idle", "Done.", name = "api")}}""")
        assertEquals("claude finished · api", ev.single().title)
        assertTrue(ev.single().choices.isEmpty())
    }

    @Test fun firstSightIsQuiet() {
        // Already waiting when we connect: the web UI doesn't notify either.
        val w = AgentWatch()
        assertTrue(w.onMessage("""{"t":"sessions","sessions":[${session("a", "needs-input")}]}""").isEmpty())
        assertTrue(w.onMessage("""{"t":"session","session":${session("b", "needs-input")}}""").isEmpty())
    }

    @Test fun reconnectCatchesUp() {
        val w = AgentWatch()
        w.onMessage("""{"t":"sessions","sessions":[${session("a", "working")}]}""")
        val ev = w.onMessage("""{"t":"sessions","sessions":[${session("a", "needs-input")}]}""")
        assertEquals(1, ev.size)
    }

    @Test fun resolvedWhenAnsweredOrClosed() {
        val w = AgentWatch()
        w.onMessage("""{"t":"session","session":${session("a", "working")}}""")
        w.onMessage("""{"t":"session","session":${session("a", "needs-input")}}""")
        w.onMessage("""{"t":"session","session":${session("a", "working")}}""")
        assertEquals(listOf("a"), w.resolved)
        w.onMessage("""{"t":"session","session":${session("a", "needs-input")}}""")
        w.onMessage("""{"t":"closed","id":"a"}""")
        assertEquals(listOf("a"), w.resolved)
    }

    @Test fun junkIgnored() {
        val w = AgentWatch()
        assertTrue(w.onMessage("nope").isEmpty())
        assertTrue(w.onMessage("""{"t":"hello","host":"x"}""").isEmpty())
    }

    @Test fun wire() {
        val f = Wire.inputFrame("devbox/abc", "1")
        assertEquals(2, f[0].toInt())
        assertEquals(10, f[1].toInt())
        assertEquals("devbox/abc", String(f, 2, 10))
        assertEquals(2 + 10 + 8 + 1, f.size)
        assertEquals('1'.code, f.last().toInt())
        assertEquals("\r", Wire.keyText("enter"))
        assertEquals("y", Wire.keyText("y"))
    }
}
