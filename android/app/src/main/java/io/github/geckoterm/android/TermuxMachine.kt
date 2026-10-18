package io.github.geckoterm.android

import java.security.MessageDigest

/**
 * Gecko in Termux as a machine of This phone: This phone's Gecko connects to
 * it at its link (`gecko host add termux --url`), so its tabs and agents show
 * up in This phone's sidebar next to the phone's own, like an SSH machine's.
 * The decisions are here, as pure functions (see TermuxMachineTest); the app
 * keeps Termux's link in its own store (sealed like every token) and hands
 * it to This phone's Gecko, which keeps it in its config (mode 0600).
 */
object TermuxMachine {
    /** Its name in This phone's machine list. */
    const val HOST = "termux"

    /**
     * An older app saved Gecko in Termux as a machine of its own, to switch
     * into. Moves it out of the machine list into the Termux store, so it is
     * handed to This phone's Gecko instead. Returns whether it moved.
     */
    fun migrate(main: MachineStore, termux: MachineStore): Boolean {
        val old = main.get(Termux.ID) ?: return false
        if (old.token.isNotEmpty() && termux.get(Termux.ID) == null) termux.save(old.copy(lastUsed = 0))
        main.remove(Termux.ID)
        return true
    }

    /** Identifies a link (address and token) without keeping the token itself. */
    fun fingerprint(m: Machine): String {
        val d = MessageDigest.getInstance("SHA-256").digest((m.url + "\n" + m.token).toByteArray(Charsets.UTF_8))
        return d.take(12).joinToString("") { "%02x".format(it) }
    }

    sealed class Step {
        object None : Step()
        /** Tell This phone's Gecko about it (added, or its link changed). */
        data class Add(val link: String) : Step()
        /** Removed from This phone's machines in Gecko itself: forget it here too. */
        object Forget : Step()
    }

    /**
     * What to do next, from Termux's saved link, the fingerprint of the link
     * last handed to This phone's Gecko, and whether its machine list has
     * Termux ([listed]; null when that couldn't be read).
     */
    fun next(saved: Machine?, handed: String?, listed: Boolean?): Step {
        if (saved == null || saved.token.isEmpty()) return Step.None
        val fp = fingerprint(saved)
        return when {
            handed == fp && listed == false -> Step.Forget
            handed != fp -> Step.Add(LinkParser.withToken(saved.url, saved.token))
            else -> Step.None
        }
    }

    /** What the connect screen says about Termux, under This phone. */
    enum class Status {
        /** Termux isn't on the phone and was never set up: nothing to show. */
        Hidden,
        /** Termux is there, Gecko in it isn't set up: offer the setup. */
        Offer,
        Running,
        /** Set up, not answering: the app starts it again. */
        Stopped,
        /** Stopped from the menu: stays stopped until started. */
        Paused,
    }

    fun status(installed: Boolean, setUp: Boolean, paused: Boolean, listening: Boolean): Status = when {
        !setUp -> if (installed) Status.Offer else Status.Hidden
        listening -> Status.Running
        paused -> Status.Paused
        else -> Status.Stopped
    }

    /**
     * Whether to start Gecko in Termux now: set up, not stopped on purpose,
     * not answering, and the last try long enough ago (10 s, doubling with
     * each failure up to 5 minutes, so a Termux that can't start it isn't
     * asked forever).
     */
    fun shouldStart(setUp: Boolean, paused: Boolean, permitted: Boolean, listening: Boolean, now: Long, lastTry: Long, failures: Int): Boolean {
        if (!setUp || paused || !permitted || listening) return false
        return now - lastTry >= retryDelay(failures)
    }

    fun retryDelay(failures: Int): Long = (10_000L shl failures.coerceIn(0, 5)).coerceAtMost(300_000L)
}
