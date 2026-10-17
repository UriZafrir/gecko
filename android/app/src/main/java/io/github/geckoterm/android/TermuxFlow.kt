package io.github.geckoterm.android

/**
 * What Termux answered to a command the app ran there (RUN_COMMAND with a
 * result PendingIntent): the "result" bundle Termux fills in. [err] is
 * Termux's own error (-1 means none: the command ran, see [exitCode]).
 */
data class TermuxResult(
    val stdout: String = "",
    val stderr: String = "",
    val exitCode: Int? = null,
    val err: Int = -1,
    val errmsg: String = "",
) {
    /** Termux ran the command (whatever its exit code). */
    val ran: Boolean get() = err == -1

    /** Termux's error without its markdown ("Error Code: `2`", the ``` fences). */
    val message: String
        get() = errmsg.lines().filter { l -> !l.startsWith("```") && !l.startsWith("Error Code:") && l.trim() != "Error Message:" }
            .joinToString("\n").trim()

    companion object {
        /** From the bundle's values (see Termux.resultOf). */
        fun fromMap(m: Map<String, Any?>) = TermuxResult(
            stdout = m["stdout"] as? String ?: "",
            stderr = m["stderr"] as? String ?: "",
            exitCode = (m["exitCode"] as? Number)?.toInt(),
            err = (m["err"] as? Number)?.toInt() ?: -1,
            errmsg = m["errmsg"] as? String ?: "",
        )
    }
}

/** Where Gecko in Termux is: what the setup screen shows. */
sealed class TermuxState {
    object Checking : TermuxState()
    /** Termux isn't on the phone. */
    object NotInstalled : TermuxState()
    /** The app may not run commands in Termux yet: ask. */
    object NoPermission : TermuxState()
    /** Asking again is blocked ("Don't allow" twice): only the app's settings help. */
    object PermissionBlocked : TermuxState()
    /** allow-external-apps isn't true in ~/.termux/termux.properties. */
    object ExternalAppsOff : TermuxState()
    /** Termux hasn't set itself up yet (it does on first open). */
    object NeedsFirstRun : TermuxState()
    /** Termux didn't answer in time. */
    object NotAnswering : TermuxState()
    /** Termux works, Gecko isn't installed in it. */
    object GeckoMissing : TermuxState()
    /** The installer runs in a Termux session the user sees. */
    object Installing : TermuxState()
    /** Gecko is installed ([version] is what `gecko version` says), not started by us yet. */
    data class Ready(val version: String) : TermuxState()
    object Starting : TermuxState()
    /** Gecko runs in Termux and answers at [link]. */
    data class Running(val link: GeckoLink) : TermuxState()
    data class Failed(val why: String) : TermuxState()
}

sealed class TermuxEvent {
    /** Whether Termux is installed and the app holds its RUN_COMMAND permission. */
    data class Checked(val installed: Boolean, val permitted: Boolean) : TermuxEvent()
    /** The answer to the permission request; canAskAgain false once Android won't ask anymore. */
    data class Permission(val granted: Boolean, val canAskAgain: Boolean) : TermuxEvent()
    /** Termux refused to start the command for want of the permission. */
    object Refused : TermuxEvent()
    /** The answer to [TermuxFlow.PROBE]; null when none came in time. */
    data class Probed(val result: TermuxResult?) : TermuxEvent()
    object InstallSent : TermuxEvent()
    object Start : TermuxEvent()
    /**
     * After starting: whether the daemon listens ([up]), and its result if
     * it already ended (a daemon that runs never sends one).
     */
    data class Started(val up: Boolean, val exited: TermuxResult?) : TermuxEvent()
    /** The answer to [TermuxFlow.URL]. */
    data class Url(val result: TermuxResult?) : TermuxEvent()
    object Stopped : TermuxEvent()
}

/**
 * The steps from "is Termux there" to a running Gecko in Termux, as a pure
 * function of the state and what just happened, so each answer Termux can
 * give maps to one screen. [TermuxActivity] runs the commands and feeds
 * the answers back in.
 */
object TermuxFlow {
    /** Port of the daemon the app starts in Termux (7681 is the computer's default and This phone's). */
    const val PORT = 7683

    // Commands, run by Termux's sh. Gecko may also be in ~/.local/bin or ~/go/bin.
    private const val PATH = "PATH=\"\$PATH:\$HOME/.local/bin:\$HOME/go/bin\"; "
    const val PROBE = PATH + "command -v gecko >/dev/null 2>&1 || exit 127; exec gecko version"
    const val START = PATH + "d=\"\${GECKO_HOME:-\$HOME/.gecko-terminal}/state\"; mkdir -p \"\$d\"; " +
        "exec gecko daemon --listen 127.0.0.1:$PORT >>\"\$d/daemon.log\" 2>&1"
    const val URL = PATH + "exec gecko url"
    const val STOP = PATH + "exec gecko stop"

    /** The one line that installs Gecko in Termux and lets this app start it. */
    const val INSTALL = "curl -fsSL https://raw.githubusercontent.com/eternaleclipse/gecko/main/packaging/termux/install.sh | sh -s -- --app"

    /** The line for ~/.termux/termux.properties, and how to make Termux read it. */
    const val PROPERTY = "allow-external-apps=true"
    const val PROPERTY_COMMAND = "mkdir -p ~/.termux && echo $PROPERTY >> ~/.termux/termux.properties && termux-reload-settings"

    fun reduce(s: TermuxState, e: TermuxEvent): TermuxState = when (e) {
        is TermuxEvent.Checked -> when {
            !e.installed -> TermuxState.NotInstalled
            !e.permitted -> TermuxState.NoPermission
            else -> TermuxState.Checking // next: probe
        }
        is TermuxEvent.Permission -> when {
            e.granted -> TermuxState.Checking
            e.canAskAgain -> TermuxState.NoPermission
            else -> TermuxState.PermissionBlocked
        }
        TermuxEvent.Refused -> TermuxState.NoPermission
        is TermuxEvent.Probed -> probed(e.result)
        TermuxEvent.InstallSent -> TermuxState.Installing
        TermuxEvent.Start -> if (s is TermuxState.Ready || s is TermuxState.Failed || s is TermuxState.Running) TermuxState.Starting else s
        is TermuxEvent.Started -> when {
            e.up -> TermuxState.Starting // next: ask for the link
            // Another Gecko already runs in Termux (the user's own `gecko`):
            // use that one.
            e.exited != null && alreadyRunning(e.exited) -> TermuxState.Starting
            e.exited != null -> failed(e.exited, "Gecko stopped right after it started")
            else -> TermuxState.Failed("Gecko didn't start in Termux in time.")
        }
        is TermuxEvent.Url -> {
            val r = e.result
            val link = r?.takeIf { it.ran && it.exitCode == 0 }?.let { localLink(it.stdout) }
            when {
                r == null -> TermuxState.NotAnswering
                link != null -> TermuxState.Running(link)
                else -> failed(r, "Gecko in Termux didn't give its link")
            }
        }
        TermuxEvent.Stopped -> if (s is TermuxState.Running || s is TermuxState.Starting) TermuxState.Checking else s
    }

    /** What the probe's answer means. */
    fun probed(r: TermuxResult?): TermuxState = when {
        r == null -> TermuxState.NotAnswering
        externalAppsOff(r) -> TermuxState.ExternalAppsOff
        !r.ran && missingFile(r.errmsg) -> TermuxState.NeedsFirstRun
        !r.ran -> TermuxState.Failed(r.message.ifBlank { "Termux couldn't run the command." })
        r.exitCode == 127 -> TermuxState.GeckoMissing
        r.exitCode == 0 && r.stdout.trim().startsWith("gecko") -> TermuxState.Ready(r.stdout.trim().lines().first())
        else -> failed(r, "gecko version failed")
    }

    /** Termux's refusal when allow-external-apps isn't set. */
    fun externalAppsOff(r: TermuxResult) = !r.ran && r.errmsg.contains("allow-external-apps")

    private fun missingFile(msg: String) =
        Regex("(?i)(not found|does not exist|no such file|not exist)").containsMatchIn(msg)

    fun alreadyRunning(r: TermuxResult) = (r.stdout + r.stderr).contains("already running")

    private fun failed(r: TermuxResult, what: String): TermuxState.Failed {
        val detail = listOf(r.message, r.stderr, r.stdout).map { it.trim() }.firstOrNull { it.isNotEmpty() }
        val code = r.exitCode?.let { " (exit code $it)" } ?: ""
        return TermuxState.Failed(if (detail == null) "$what$code." else "$what$code: ${detail.lines().take(6).joinToString("\n")}")
    }

    /**
     * The link for this phone from what `gecko url` prints: its first line
     * with a token on 127.0.0.1 (other lines are for other devices).
     */
    fun localLink(stdout: String): GeckoLink? {
        for (line in stdout.lines()) {
            if (line.isBlank() || line.contains("(other devices)")) continue
            val l = try { LinkParser.parse(line) } catch (e: LinkException) { continue }
            if (l.token != null && (l.host == "127.0.0.1" || l.host == "localhost" || l.host == "::1")) return l
        }
        return null
    }
}
