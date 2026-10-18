package io.github.geckoterm.android

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class TermuxMachineTest {
    private class MapKV : KeyValue {
        val map = HashMap<String, String>()
        override fun get(key: String) = map[key]
        override fun put(key: String, value: String?) { if (value == null) map.remove(key) else map[key] = value }
    }

    private class FakeBox : SecretBox {
        override fun seal(plain: String) = "sealed:" + plain.reversed()
        override fun open(sealed: String) = if (sealed.startsWith("sealed:")) sealed.removePrefix("sealed:").reversed() else null
    }

    private val tok = "0123456789abcdef0123456789abcdef"
    private val termuxLink = Machine(id = Termux.ID, name = "Termux", url = "http://127.0.0.1:7683/", token = tok, lastUsed = 99)

    // An app from before: Termux saved as a machine of its own, next to a computer.
    @Test fun migratesAnOldTermuxMachine() {
        val mainKv = MapKV()
        val main = MachineStore(mainKv, FakeBox())
        val termux = MachineStore(MapKV(), FakeBox())
        val desk = Machine(name = "desk", url = "http://192.168.1.20:7681/", token = "desk-token-123")
        main.save(desk)
        main.save(termuxLink)
        assertTrue(TermuxMachine.migrate(main, termux))
        // Out of the machine list, the computer untouched, the link kept (token sealed).
        assertEquals(listOf(desk), main.list())
        assertEquals(termuxLink.copy(lastUsed = 0), termux.get(Termux.ID))
        assertFalse(mainKv.map.values.any { it.contains(tok) })
        // Once only; and nothing to do on a fresh install.
        assertFalse(TermuxMachine.migrate(main, termux))
        assertFalse(TermuxMachine.migrate(MachineStore(MapKV(), FakeBox()), MachineStore(MapKV(), FakeBox())))
        // Then it's handed to This phone's Gecko without setting it up again.
        assertEquals(TermuxMachine.Step.Add("http://127.0.0.1:7683/?token=$tok"), TermuxMachine.next(termux.get(Termux.ID), null, null))
    }

    @Test fun migrationKeepsANewerLink() {
        val main = MachineStore(MapKV(), FakeBox())
        val termux = MachineStore(MapKV(), FakeBox())
        val newer = termuxLink.copy(token = "newer-token-0123456789", lastUsed = 0)
        termux.save(newer)
        main.save(termuxLink)
        assertTrue(TermuxMachine.migrate(main, termux))
        assertEquals(newer, termux.get(Termux.ID))
        assertTrue(main.list().isEmpty())
    }

    @Test fun migrationDropsATermuxMachineWithoutToken() {
        val main = MachineStore(MapKV(), FakeBox())
        val termux = MachineStore(MapKV(), FakeBox())
        main.save(termuxLink.copy(token = ""))
        assertTrue(TermuxMachine.migrate(main, termux))
        assertNull(termux.get(Termux.ID))
        assertEquals(TermuxMachine.Step.None, TermuxMachine.next(termux.get(Termux.ID), null, null))
    }

    @Test fun handingTheLinkToThisPhone() {
        val fp = TermuxMachine.fingerprint(termuxLink)
        assertFalse("the fingerprint shows the token", fp.contains(tok))
        // Never set up: nothing to do.
        assertEquals(TermuxMachine.Step.None, TermuxMachine.next(null, null, null))
        // Set up, not handed yet (or This phone's list unknown): add it.
        val add = TermuxMachine.Step.Add("http://127.0.0.1:7683/?token=$tok")
        assertEquals(add, TermuxMachine.next(termuxLink, null, null))
        assertEquals(add, TermuxMachine.next(termuxLink, null, false))
        // Handed and listed (or unknown): nothing.
        assertEquals(TermuxMachine.Step.None, TermuxMachine.next(termuxLink, fp, true))
        assertEquals(TermuxMachine.Step.None, TermuxMachine.next(termuxLink, fp, null))
        // A new token (Termux's Gecko got a fresh home): add it again.
        assertEquals(TermuxMachine.Step.Add("http://127.0.0.1:7683/?token=other-token-123456"),
            TermuxMachine.next(termuxLink.copy(token = "other-token-123456"), fp, true))
        // Handed, then removed in Gecko's sidebar: forget it here too.
        assertEquals(TermuxMachine.Step.Forget, TermuxMachine.next(termuxLink, fp, false))
    }

    @Test fun whatTheConnectScreenSays() {
        assertEquals(TermuxMachine.Status.Hidden, TermuxMachine.status(installed = false, setUp = false, paused = false, listening = false))
        assertEquals(TermuxMachine.Status.Offer, TermuxMachine.status(installed = true, setUp = false, paused = false, listening = false))
        assertEquals(TermuxMachine.Status.Running, TermuxMachine.status(installed = true, setUp = true, paused = false, listening = true))
        assertEquals(TermuxMachine.Status.Stopped, TermuxMachine.status(installed = true, setUp = true, paused = false, listening = false))
        assertEquals(TermuxMachine.Status.Paused, TermuxMachine.status(installed = true, setUp = true, paused = true, listening = false))
        // Termux uninstalled after setup: still shown, as not running.
        assertEquals(TermuxMachine.Status.Stopped, TermuxMachine.status(installed = false, setUp = true, paused = false, listening = false))
    }

    @Test fun startingItAgain() {
        val now = 1_000_000L
        assertTrue(TermuxMachine.shouldStart(setUp = true, paused = false, permitted = true, listening = false, now = now, lastTry = 0, failures = 0))
        assertFalse(TermuxMachine.shouldStart(setUp = false, paused = false, permitted = true, listening = false, now = now, lastTry = 0, failures = 0))
        assertFalse("stopped from the menu", TermuxMachine.shouldStart(setUp = true, paused = true, permitted = true, listening = false, now = now, lastTry = 0, failures = 0))
        assertFalse(TermuxMachine.shouldStart(setUp = true, paused = false, permitted = false, listening = false, now = now, lastTry = 0, failures = 0))
        assertFalse(TermuxMachine.shouldStart(setUp = true, paused = false, permitted = true, listening = true, now = now, lastTry = 0, failures = 0))
        assertFalse("too soon", TermuxMachine.shouldStart(setUp = true, paused = false, permitted = true, listening = false, now = now, lastTry = now - 5_000, failures = 0))
        assertTrue(TermuxMachine.shouldStart(setUp = true, paused = false, permitted = true, listening = false, now = now, lastTry = now - 10_000, failures = 0))
        assertFalse("backs off", TermuxMachine.shouldStart(setUp = true, paused = false, permitted = true, listening = false, now = now, lastTry = now - 10_000, failures = 2))
        assertEquals(10_000L, TermuxMachine.retryDelay(0))
        assertEquals(40_000L, TermuxMachine.retryDelay(2))
        assertEquals(300_000L, TermuxMachine.retryDelay(9))
    }
}

class TokensOutOfLogsTest {
    @Test fun toStringHidesTokens() {
        val tok = "eaa941e80cc184589f01cc53723c08f8"
        val r = TermuxResult(stdout = "http://127.0.0.1:7683/?token=$tok\n", exitCode = 0)
        val l = GeckoLink("http://127.0.0.1:7683/", "127.0.0.1", tok)
        val m = Machine(name = "Termux", url = "http://127.0.0.1:7683/", token = tok)
        for (s in listOf(r.toString(), l.toString(), m.toString(), TermuxEvent.Url(r).toString())) {
            assertFalse(s, s.contains(tok))
        }
        assertTrue(r.toString().contains("127.0.0.1:7683"))
    }
}
