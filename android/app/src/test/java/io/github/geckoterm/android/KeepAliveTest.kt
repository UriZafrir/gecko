package io.github.geckoterm.android

import org.junit.Assert.assertEquals
import org.junit.Test

class KeepAliveTest {
    @Test fun makers() {
        assertEquals(KeepAlive.Maker.SAMSUNG, KeepAlive.maker("samsung", "samsung"))
        assertEquals(KeepAlive.Maker.XIAOMI, KeepAlive.maker("Xiaomi", "Redmi"))
        assertEquals(KeepAlive.Maker.XIAOMI, KeepAlive.maker("Xiaomi", "POCO"))
        assertEquals(KeepAlive.Maker.ONEPLUS, KeepAlive.maker("OnePlus", "OnePlus"))
        assertEquals(KeepAlive.Maker.OPPO, KeepAlive.maker("realme", "realme"))
        assertEquals(KeepAlive.Maker.VIVO, KeepAlive.maker("vivo", "iQOO"))
        assertEquals(KeepAlive.Maker.HUAWEI, KeepAlive.maker("HONOR", "HONOR"))
        assertEquals(null, KeepAlive.maker("Google", "google"))
    }
}
