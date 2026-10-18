package io.github.geckoterm.android

import org.json.JSONArray
import org.json.JSONObject
import java.util.UUID

/** A computer running Gecko that this phone connects to. */
data class Machine(
    val id: String = UUID.randomUUID().toString(),
    val name: String,
    /** scheme://host[:port]/ */
    val url: String,
    /** Gecko's access token; empty when it has to be scanned again. */
    val token: String,
    val lastUsed: Long = 0,
) {
    val host: String get() = LinkParser.parse(url).host

    /** Without the token, so a log line or a test failure can't show it. */
    override fun toString() = "Machine(id=$id, name=$name, url=$url, token=${if (token.isEmpty()) "none" else "hidden"}, lastUsed=$lastUsed)"
}

/** Plain key-value storage (SharedPreferences on the phone, a map in tests). */
interface KeyValue {
    fun get(key: String): String?
    fun put(key: String, value: String?)
}

/** Encrypts secrets at rest (Android Keystore on the phone). */
interface SecretBox {
    fun seal(plain: String): String
    /** Null when it can't be opened, e.g. the key is gone after a restore. */
    fun open(sealed: String): String?
}

/**
 * The saved machines. Names and addresses are stored as JSON; each token is
 * sealed with a key that never leaves the Android Keystore.
 */
class MachineStore(private val kv: KeyValue, private val box: SecretBox) {
    private val key = "machines"

    @Synchronized
    fun list(): List<Machine> {
        val raw = kv.get(key) ?: return emptyList()
        val arr = try { JSONArray(raw) } catch (e: Exception) { return emptyList() }
        val out = ArrayList<Machine>()
        for (i in 0 until arr.length()) {
            val o = arr.optJSONObject(i) ?: continue
            val sealed = o.optString("token")
            out += Machine(
                id = o.optString("id").ifEmpty { continue },
                name = o.optString("name"),
                url = o.optString("url").ifEmpty { continue },
                token = if (sealed.isEmpty()) "" else box.open(sealed) ?: "",
                lastUsed = o.optLong("lastUsed"),
            )
        }
        return out
    }

    fun get(id: String): Machine? = list().find { it.id == id }

    /** The machine opened most recently, if any. */
    fun recent(): Machine? = list().maxByOrNull { it.lastUsed }?.takeIf { it.lastUsed > 0 }

    /** Adds the machine, or replaces the one with its id. */
    @Synchronized
    fun save(m: Machine) {
        val all = list().toMutableList()
        val i = all.indexOfFirst { it.id == m.id }
        if (i >= 0) all[i] = m else all += m
        write(all)
    }

    /** A saved machine at the same address, so scanning again updates it. */
    fun findByUrl(url: String): Machine? = list().find { it.url == url }

    @Synchronized
    fun remove(id: String) = write(list().filter { it.id != id })

    @Synchronized
    fun touch(id: String, now: Long = System.currentTimeMillis()) {
        write(list().map { if (it.id == id) it.copy(lastUsed = now) else it })
    }

    private fun write(all: List<Machine>) {
        val arr = JSONArray()
        for (m in all) {
            arr.put(JSONObject().apply {
                put("id", m.id)
                put("name", m.name)
                put("url", m.url)
                put("token", if (m.token.isEmpty()) "" else box.seal(m.token))
                put("lastUsed", m.lastUsed)
            })
        }
        kv.put(key, arr.toString())
    }
}
