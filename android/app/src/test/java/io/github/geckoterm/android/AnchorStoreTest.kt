package io.github.geckoterm.android

import org.junit.Assert.assertEquals
import org.junit.Test

class AnchorStoreTest {
    private class MapKV : KeyValue {
        val m = HashMap<String, String>()
        override fun get(key: String) = m[key]
        override fun put(key: String, value: String?) { if (value == null) m.remove(key) else m[key] = value }
    }

    @Test fun keepsThePathPerMachine() {
        val s = AnchorStore(MapKV())
        assertEquals("", s.get(LocalGecko.ID))
        s.set(LocalGecko.ID, """["ubuntu"]""")
        s.set("devbox-id", """["gpu","lab"]""")
        assertEquals("""["ubuntu"]""", s.get(LocalGecko.ID))
        assertEquals("""["gpu","lab"]""", s.get("devbox-id"))
    }

    @Test fun switchingBackClearsIt() {
        val s = AnchorStore(MapKV())
        s.set(LocalGecko.ID, """["ubuntu"]""")
        s.set(LocalGecko.ID, "[]")
        assertEquals("", s.get(LocalGecko.ID))
    }

    @Test fun refusesAnythingButNames() {
        val s = AnchorStore(MapKV())
        for (bad in listOf("nope", """[1]""", """["a b"]""", """["a/b"]""", """[""]""", """["a","b","c","d","e"]""", """{"x":1}""")) {
            s.set("m", """["ok"]""")
            s.set("m", bad)
            assertEquals(bad, "", s.get("m"))
        }
    }
}
