package io.github.geckoterm.android

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class TermuxFlowTest {
    private val tok = "4dd99c1f25e82e4c078bc059dcab8d7f262727a88ea7ae6e"
    private fun ran(exit: Int, out: String = "", err: String = "") = TermuxResult(stdout = out, stderr = err, exitCode = exit)
    private fun refused(msg: String) = TermuxResult(err = 2, errmsg = msg)

    // What Termux puts in the result bundle (see Termux.resultOf).
    @Test fun resultFromBundleValues() {
        val r = TermuxResult.fromMap(mapOf("stdout" to "gecko v0.2.0 linux/arm64\n", "stderr" to "", "exitCode" to 0, "err" to -1, "errmsg" to null))
        assertEquals("gecko v0.2.0 linux/arm64\n", r.stdout)
        assertEquals(0, r.exitCode)
        assertTrue(r.ran)
        // A command Termux didn't run has no exit code, and an error.
        val no = TermuxResult.fromMap(mapOf("err" to 2, "errmsg" to "nope"))
        assertNull(no.exitCode)
        assertFalse(no.ran)
        assertEquals("", no.stdout)
        // Missing keys: nothing ran wrong as far as Termux knows.
        assertTrue(TermuxResult.fromMap(emptyMap()).ran)
    }

    @Test fun checks() {
        assertEquals(TermuxState.NotInstalled, TermuxFlow.reduce(TermuxState.Checking, TermuxEvent.Checked(installed = false, permitted = false)))
        assertEquals(TermuxState.NoPermission, TermuxFlow.reduce(TermuxState.Checking, TermuxEvent.Checked(installed = true, permitted = false)))
        assertEquals(TermuxState.Checking, TermuxFlow.reduce(TermuxState.Checking, TermuxEvent.Checked(installed = true, permitted = true)))
    }

    @Test fun permissionAnswers() {
        assertEquals(TermuxState.Checking, TermuxFlow.reduce(TermuxState.NoPermission, TermuxEvent.Permission(granted = true, canAskAgain = true)))
        assertEquals(TermuxState.NoPermission, TermuxFlow.reduce(TermuxState.NoPermission, TermuxEvent.Permission(granted = false, canAskAgain = true)))
        assertEquals(TermuxState.PermissionBlocked, TermuxFlow.reduce(TermuxState.NoPermission, TermuxEvent.Permission(granted = false, canAskAgain = false)))
        // Revoked later: Termux's service refuses us.
        assertEquals(TermuxState.NoPermission, TermuxFlow.reduce(TermuxState.Checking, TermuxEvent.Refused))
    }

    @Test fun probeAnswers() {
        assertEquals(TermuxState.NotAnswering, TermuxFlow.probed(null))
        assertEquals(
            TermuxState.ExternalAppsOff,
            TermuxFlow.probed(refused("Error Code: `2`\nError Message:\n```\nRunCommandService requires `allow-external-apps` property to be set to `true` in `~/.termux/termux.properties` file.\n```")),
        )
        assertEquals(TermuxState.NeedsFirstRun, TermuxFlow.probed(refused("The executable file at \"/data/data/com.termux/files/usr/bin/sh\" does not exist.")))
        assertEquals(TermuxState.GeckoMissing, TermuxFlow.probed(ran(127)))
        assertEquals(TermuxState.Ready("gecko v0.2.0 linux/arm64"), TermuxFlow.probed(ran(0, "gecko v0.2.0 linux/arm64\n")))
        val f = TermuxFlow.probed(ran(1, err = "Segmentation fault"))
        assertTrue(f is TermuxState.Failed && f.why.contains("Segmentation fault") && f.why.contains("exit code 1"))
        assertTrue(TermuxFlow.probed(refused("something else")) is TermuxState.Failed)
        // Termux's markdown stays out of the screen.
        val md = TermuxFlow.probed(refused("Error Code: `2`\nError Message:\n```\nThe working directory is gone.\n```"))
        assertEquals(TermuxState.Failed("The working directory is gone."), md)
    }

    @Test fun startAndLink() {
        val ready = TermuxState.Ready("gecko v0.2.0")
        assertEquals(TermuxState.Starting, TermuxFlow.reduce(ready, TermuxEvent.Start))
        // Start only from a state that can start.
        assertEquals(TermuxState.GeckoMissing, TermuxFlow.reduce(TermuxState.GeckoMissing, TermuxEvent.Start))
        assertEquals(TermuxState.Starting, TermuxFlow.reduce(TermuxState.Starting, TermuxEvent.Started(up = true, exited = null)))
        // The user's own Gecko already runs in Termux: its link is as good.
        assertEquals(
            TermuxState.Starting,
            TermuxFlow.reduce(TermuxState.Starting, TermuxEvent.Started(up = false, exited = ran(1, err = "gecko: gecko daemon already running (/data/data/com.termux/files/home/.gecko-terminal/state/gecko.sock)"))),
        )
        val died = TermuxFlow.reduce(TermuxState.Starting, TermuxEvent.Started(up = false, exited = ran(1, err = "gecko: listen tcp 127.0.0.1:7683: bind: address already in use")))
        assertTrue(died is TermuxState.Failed && died.why.contains("address already in use"))
        assertTrue(TermuxFlow.reduce(TermuxState.Starting, TermuxEvent.Started(up = false, exited = null)) is TermuxState.Failed)

        val out = "http://127.0.0.1:7683/?token=$tok\nhttp://192.168.1.20:7682/?token=other123456  (other devices)\n"
        val running = TermuxFlow.reduce(TermuxState.Starting, TermuxEvent.Url(ran(0, out)))
        assertTrue(running is TermuxState.Running)
        running as TermuxState.Running
        assertEquals("http://127.0.0.1:7683/", running.link.baseUrl)
        assertEquals(tok, running.link.token)
        assertEquals(TermuxState.NotAnswering, TermuxFlow.reduce(TermuxState.Starting, TermuxEvent.Url(null)))
        assertTrue(TermuxFlow.reduce(TermuxState.Starting, TermuxEvent.Url(ran(1, err = "gecko: daemon did not start"))) is TermuxState.Failed)
        // Stopping goes back to the checks.
        assertEquals(TermuxState.Checking, TermuxFlow.reduce(running, TermuxEvent.Stopped))
        assertEquals(TermuxState.Installing, TermuxFlow.reduce(TermuxState.GeckoMissing, TermuxEvent.InstallSent))
    }

    @Test fun localLinkOnly() {
        assertNull(TermuxFlow.localLink(""))
        assertNull(TermuxFlow.localLink("http://192.168.1.20:7681/?token=$tok  (other devices)"))
        // Without a token it's no use.
        assertNull(TermuxFlow.localLink("http://127.0.0.1:7683/"))
        assertEquals("http://127.0.0.1:7681/", TermuxFlow.localLink("some warning\nhttp://127.0.0.1:7681/?token=$tok\n")?.baseUrl)
    }

    // The commands are plain sh, the same on every Termux.
    @Test fun commands() {
        assertTrue(TermuxFlow.PROBE.contains("|| exit 127"))
        assertTrue(TermuxFlow.START.contains("gecko daemon --listen 127.0.0.1:${TermuxFlow.PORT}"))
        assertTrue(TermuxFlow.INSTALL.endsWith("| sh -s -- --app"))
        assertFalse(TermuxFlow.INSTALL.contains("—"))
    }
}
