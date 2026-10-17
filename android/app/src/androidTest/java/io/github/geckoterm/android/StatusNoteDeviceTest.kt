package io.github.geckoterm.android

import android.content.Intent
import androidx.test.core.app.ActivityScenario
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.uiautomator.By
import androidx.test.uiautomator.UiDevice
import androidx.test.uiautomator.Until
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assume.assumeTrue
import org.junit.Test
import org.junit.runner.RunWith

/**
 * The ongoing notification says what is watched, read from the shade: a
 * saved machine (geckoUrl), then also Gecko on this phone, one notification.
 */
@RunWith(AndroidJUnit4::class)
class StatusNoteDeviceTest {
    private val ctx get() = ApplicationProvider.getApplicationContext<android.content.Context>()
    private val device get() = UiDevice.getInstance(InstrumentationRegistry.getInstrumentation())

    @After fun stop() {
        WatchService.stop(ctx)
        PhoneService.stop(ctx)
        device.pressBack()
    }

    @Test fun saysWhatItWatches() {
        val url = InstrumentationRegistry.getArguments().getString("geckoUrl")
        assumeTrue("no geckoUrl argument", !url.isNullOrEmpty())
        InstrumentationRegistry.getInstrumentation().uiAutomation.executeShellCommand("pm grant ${ctx.packageName} android.permission.POST_NOTIFICATIONS").close()
        val store = Gecko.machines(ctx)
        for (m in store.list()) store.remove(m.id)
        val link = LinkParser.parse(url!!)
        val host = (java.net.URL(link.baseUrl + "api/info").openConnection() as java.net.HttpURLConnection).run {
            setRequestProperty("Authorization", "Bearer " + link.token)
            org.json.JSONObject(inputStream.bufferedReader().readText()).getString("host")
        }
        store.save(Machine(name = host, url = link.baseUrl, token = link.token!!))

        val intent = Intent(ctx, ConnectActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        ActivityScenario.launch<ConnectActivity>(intent).use {
            WatchService.start(ctx)
            expect("Watching agents on $host", "You'll be notified when an agent needs you.")

            // Gecko on this phone too: the same notification, both lines.
            PhoneService.start(ctx)
            expect("Watching agents on 2 machines", "Gecko running on this phone")

            // Watching off: this phone's Gecko alone.
            WatchService.stop(ctx)
            expect("Gecko on this phone", "Gecko running on this phone")
        }
    }

    private fun expect(title: String, text: String) {
        device.openNotification()
        val t = device.wait(Until.findObject(By.text(title)), 20_000)
        val found = device.findObjects(By.textContains("atching agents")).map { it.text } + device.findObjects(By.textContains("Gecko")).map { it.text }
        assertNotNull("notification title '$title'; shade has $found", t)
        assertNotNull("notification text '$text'", device.wait(Until.findObject(By.text(text)), 5_000))
        assertEquals("one Gecko notification", 1, device.findObjects(By.text(title)).size)
        device.pressBack()
        Thread.sleep(500)
    }
}
