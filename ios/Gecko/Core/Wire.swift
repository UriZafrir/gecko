import Foundation

/// Bits of Gecko's wire protocol (internal/proto) the app speaks itself.
enum Wire {
    private static let kindInput: UInt8 = 2

    /// The first message on a connection of the app's own.
    static let hello = #"{"t":"hello","client":"ios"}"#

    /// A keyboard input frame: kind | len(id) | id | offset (8 bytes, unused for input) | data.
    static func inputFrame(sessionId: String, data: String) -> Data? {
        let id = Array(sessionId.utf8)
        guard id.count < 256 else { return nil }
        var out = Data([kindInput, UInt8(id.count)])
        out.append(contentsOf: id)
        out.append(contentsOf: [UInt8](repeating: 0, count: 8))
        out.append(contentsOf: Array(data.utf8))
        return out
    }

    /// What pressing an answer's key sends (as the web UI's agent dock does).
    static func keyText(_ key: String) -> String {
        switch key {
        case "enter": return "\r"
        case "esc": return "\u{1b}"
        case "up": return "\u{1b}[A"
        case "down": return "\u{1b}[B"
        default: return key
        }
    }
}
