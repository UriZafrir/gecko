package io.github.geckoterm.android

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ShortLandscapeTest {
    @Test fun phonesOnTheirSide() {
        // Pixel 8, small phone, Pixel Fold's outer screen, a flip phone: sideways.
        for (h in listOf(411, 360, 384, 359)) assertTrue("$h dp", TerminalActivity.shortLandscape(true, h, false))
    }

    @Test fun notTabletsFoldablesPortraitOrSplit() {
        assertFalse(TerminalActivity.shortLandscape(true, 800, false)) // Pixel Tablet
        assertFalse(TerminalActivity.shortLandscape(true, 573, false)) // 7.6" foldable, open
        assertFalse(TerminalActivity.shortLandscape(false, 411, false)) // portrait (a short split window)
        assertFalse(TerminalActivity.shortLandscape(true, 411, true)) // split screen
        assertFalse(TerminalActivity.shortLandscape(true, 0, false)) // unknown size
    }
}
