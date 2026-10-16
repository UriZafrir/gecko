package io.github.geckoterm.android

import org.json.JSONObject

/** Something worth a notification: an agent waiting for you, or done. */
data class AgentEvent(
    val sessionId: String,
    val title: String,
    val body: String,
    val needsInput: Boolean,
    /** The answers on screen: key to press ("1", "y", "enter") and what it says. */
    val choices: List<Pair<String, String>> = emptyList(),
)

/**
 * Follows Gecko's session list (the "sessions", "session" and "closed"
 * messages) and reports when an agent starts waiting for input or finishes
 * working, the same moments the web UI notifies about.
 */
class AgentWatch {
    private val status = HashMap<String, String?>()
    private val labels = HashMap<String, String>()

    /** Sessions whose notification should go away (the agent moved on, or the tab closed). */
    val resolved = ArrayList<String>()

    fun onMessage(text: String): List<AgentEvent> {
        resolved.clear()
        val m = try { JSONObject(text) } catch (e: Exception) { return emptyList() }
        return when (m.optString("t")) {
            "sessions" -> {
                val arr = m.optJSONArray("sessions")
                val host = m.optString("host")
                val seen = HashSet<String>()
                val out = ArrayList<AgentEvent>()
                if (arr != null) for (i in 0 until arr.length()) {
                    val s = arr.optJSONObject(i) ?: continue
                    seen += s.optString("id")
                    // A list after reconnecting: compare with what was known.
                    out += update(s)
                }
                // A list for one machine of several leaves the others alone.
                val gone = status.keys.filter { it !in seen && (host.isEmpty() || hostOf[it] == host) }
                for (id in gone) forget(id)
                out
            }
            "session" -> m.optJSONObject("session")?.let { update(it) } ?: emptyList()
            "closed" -> { forget(m.optString("id")); emptyList() }
            else -> emptyList()
        }
    }

    private val hostOf = HashMap<String, String>()

    private fun forget(id: String) {
        if (status.remove(id) == "needs-input") resolved += id
        labels.remove(id)
        hostOf.remove(id)
    }

    private fun update(s: JSONObject): List<AgentEvent> {
        val id = s.optString("id").ifEmpty { return emptyList() }
        val agent = s.optJSONObject("agent")
        val now = agent?.optString("status")?.ifEmpty { null }
        hostOf[id] = s.optString("host")
        val known = status.containsKey(id)
        val prev = status[id]
        status[id] = now
        if (prev == "needs-input" && now != "needs-input") resolved += id
        if (!known || prev == null || now == null || prev == now) return emptyList()
        val finished = prev == "working" && now == "idle"
        if (now != "needs-input" && !finished) return emptyList()
        val name = agent.optString("name").ifEmpty { "agent" }
        val title = "$name ${if (finished) "finished" else "needs you"} · ${tabLabel(s)}"
        val choices = ArrayList<Pair<String, String>>()
        agent.optJSONArray("choices")?.let { arr ->
            for (i in 0 until arr.length()) {
                val c = arr.optJSONObject(i) ?: continue
                val key = c.optString("key")
                if (key.isNotEmpty()) choices += key to c.optString("label").ifEmpty { key }
            }
        }
        return listOf(AgentEvent(id, title, agent.optString("detail"), !finished, if (finished) emptyList() else choices))
    }

    companion object {
        /** The tab's title as the web UI shows it (main.js tabLabel), in short. */
        fun tabLabel(s: JSONObject): String {
            s.optString("name").ifEmpty { null }?.let { return it }
            s.optJSONObject("agent")?.optString("name")?.ifEmpty { null }?.let { return it }
            val kind = s.optString("kind")
            if ((kind == "ssh" || kind == "mosh") && s.optString("remote").isNotEmpty()) return s.optString("remote")
            if (kind != "shell" && s.optString("proc").isNotEmpty()) return s.optString("proc")
            s.optString("repo").ifEmpty { null }?.let { return it }
            val cwd = s.optString("cwd").trimEnd('/')
            return cwd.substringAfterLast('/').ifEmpty { "shell" }
        }
    }
}
