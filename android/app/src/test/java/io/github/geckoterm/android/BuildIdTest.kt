package io.github.geckoterm.android

import org.junit.Assert.assertEquals
import org.junit.Test
import java.time.ZoneId

class BuildIdTest {
    @Test fun commitAndLocalTime() {
        assertEquals("a1b2c3d · Oct 17 02:15", BuildId.format("a1b2c3d", "2026-10-16T23:15:00Z", ZoneId.of("Asia/Jerusalem")))
        assertEquals("a1b2c3d-dirty · Oct 16 23:15", BuildId.format("a1b2c3d-dirty", "2026-10-16T23:15:00Z", ZoneId.of("UTC")))
        assertEquals("a1b2c3d · Jan 5 09:03", BuildId.format("a1b2c3d", "2027-01-05T09:03:59Z", ZoneId.of("UTC")))
    }

    @Test fun withoutATime() {
        assertEquals("a1b2c3d", BuildId.format("a1b2c3d", "", ZoneId.of("UTC")))
        assertEquals("a1b2c3d", BuildId.format("a1b2c3d", "yesterday", ZoneId.of("UTC")))
        assertEquals("unknown", BuildId.format("", "", ZoneId.of("UTC")))
    }

    @Test fun thisBuildHasOne() {
        assert(BuildConfig.BUILD_COMMIT.isNotEmpty())
    }
}
