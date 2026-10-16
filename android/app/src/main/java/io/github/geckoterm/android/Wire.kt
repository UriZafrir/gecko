package io.github.geckoterm.android

/** Bits of Gecko's wire protocol (internal/proto) the app speaks itself. */
object Wire {
    private const val KIND_INPUT: Byte = 2

    /** A keyboard input frame: kind | len(id) | id | offset (8 bytes, unused for input) | data. */
    fun inputFrame(sessionId: String, data: String): ByteArray {
        val id = sessionId.toByteArray(Charsets.UTF_8)
        require(id.size < 256) { "session id too long" }
        val body = data.toByteArray(Charsets.UTF_8)
        val out = ByteArray(2 + id.size + 8 + body.size)
        out[0] = KIND_INPUT
        out[1] = id.size.toByte()
        id.copyInto(out, 2)
        body.copyInto(out, 2 + id.size + 8)
        return out
    }

    /** What pressing an answer's key sends (as the web UI's agent dock does). */
    fun keyText(key: String): String = when (key) {
        "enter" -> "\r"
        "esc" -> "\u001b"
        "up" -> "\u001b[A"
        "down" -> "\u001b[B"
        else -> key
    }
}
