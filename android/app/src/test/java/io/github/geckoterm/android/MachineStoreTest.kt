package io.github.geckoterm.android

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.Base64

class MachineStoreTest {
    private class MapKV : KeyValue {
        val map = HashMap<String, String>()
        override fun get(key: String) = map[key]
        override fun put(key: String, value: String?) { if (value == null) map.remove(key) else map[key] = value }
    }

    /** Stands in for the Keystore: reversible, and visibly not the plain token. */
    private class FakeBox : SecretBox {
        var broken = false
        override fun seal(plain: String) = "sealed:" + Base64.getEncoder().encodeToString(plain.reversed().toByteArray())
        override fun open(sealed: String) = if (broken || !sealed.startsWith("sealed:")) null
            else String(Base64.getDecoder().decode(sealed.removePrefix("sealed:"))).reversed()
    }

    private val kv = MapKV()
    private val box = FakeBox()
    private val store = MachineStore(kv, box)

    @Test fun saveListRemove() {
        assertTrue(store.list().isEmpty())
        val a = Machine(name = "devbox", url = "http://192.168.1.20:7681/", token = "secret-token-1")
        val b = Machine(name = "laptop", url = "http://100.64.0.2:7681/", token = "secret-token-2")
        store.save(a)
        store.save(b)
        assertEquals(listOf(a, b), store.list())
        assertEquals(b, store.get(b.id))
        assertEquals(a, store.findByUrl("http://192.168.1.20:7681/"))
        store.save(a.copy(name = "desk"))
        assertEquals("desk", store.get(a.id)!!.name)
        assertEquals(2, store.list().size)
        store.remove(a.id)
        assertEquals(listOf(b), store.list())
    }

    @Test fun tokensAreSealed() {
        store.save(Machine(name = "devbox", url = "http://h:1/", token = "secret-token-1"))
        val raw = kv.map["machines"]!!
        assertFalse(raw.contains("secret-token-1"))
        assertTrue(raw.contains("sealed:"))
        assertEquals("secret-token-1", store.list()[0].token)
    }

    @Test fun unreadableTokenBecomesEmpty() {
        store.save(Machine(name = "devbox", url = "http://h:1/", token = "secret-token-1"))
        box.broken = true
        val m = store.list()[0]
        assertEquals("", m.token)
        assertEquals("devbox", m.name)
    }

    @Test fun recent() {
        val a = Machine(name = "a", url = "http://a:1/", token = "tttttttt1")
        val b = Machine(name = "b", url = "http://b:1/", token = "tttttttt2")
        store.save(a)
        store.save(b)
        assertNull(store.recent())
        store.touch(b.id, 100)
        store.touch(a.id, 200)
        assertEquals(a.id, store.recent()!!.id)
    }

    @Test fun damagedStorage() {
        kv.map["machines"] = "not json"
        assertTrue(store.list().isEmpty())
        kv.map["machines"] = """[{"id":"x","name":"n","url":"http://h:1/","token":""},{"name":"no id"}]"""
        assertEquals(1, store.list().size)
    }
}
