import Foundation

/// A Gecko address: where the web UI is, and the access token if the link had one.
struct GeckoLink: Equatable {
    /// scheme://host[:port]/ with nothing after the slash.
    let baseURL: String
    let host: String
    let token: String?

    var https: Bool { baseURL.hasPrefix("https:") }
}

struct LinkError: Error, LocalizedError, Equatable {
    let message: String
    init(_ message: String) { self.message = message }
    var errorDescription: String? { message }
}

/// Reads what Gecko's "Open on another device" QR code holds, or a pasted
/// link: http://192.168.1.20:7681/?token=..., the lines `gecko url` prints
/// ("http://... (other devices)"), an https Tailscale address, or a bare
/// host:port. The same rules as the Android app's LinkParser.kt.
enum LinkParser {
    private static let tokenPattern = "^[A-Za-z0-9._~+/=-]{8,512}$"

    static func parse(_ input: String) throws -> GeckoLink {
        let text = input.trimmingCharacters(in: .whitespacesAndNewlines)
        if text.isEmpty { throw LinkError("Paste or scan the link Gecko shows under Open on another device.") }
        var word: String
        if let r = text.range(of: #"https?://\S+"#, options: [.regularExpression, .caseInsensitive]) {
            word = String(text[r])
        } else {
            word = text.split(whereSeparator: { $0.isWhitespace }).first.map(String.init) ?? text
        }
        while let last = word.last, ".,;)\"'>".contains(last) { word.removeLast() }
        if word.range(of: "^[a-zA-Z][a-zA-Z0-9+.-]*://", options: .regularExpression) == nil {
            if word.contains("://") { throw LinkError("Only http and https links work.") }
            word = "http://" + word
        }
        guard let c = URLComponents(string: word) else { throw LinkError("That doesn't look like a link.") }
        let scheme = c.scheme?.lowercased() ?? ""
        guard scheme == "http" || scheme == "https" else { throw LinkError("Only http and https links work.") }
        var host = (c.host ?? "").lowercased()
        if host.hasPrefix("[") { host.removeFirst() }
        if host.hasSuffix("]") { host.removeLast() }
        if host.isEmpty { throw LinkError("The link has no machine address in it.") }
        if c.user != nil || c.password != nil { throw LinkError("Links with a user name in them aren't Gecko links.") }
        var token: String?
        if let v = c.queryItems?.first(where: { $0.name == "token" })?.value, !v.isEmpty { token = v }
        if let t = token, t.range(of: tokenPattern, options: .regularExpression) == nil {
            throw LinkError("The token in that link looks damaged. Copy it again.")
        }
        let hostPart = host.contains(":") ? "[\(host)]" : host
        var port = ""
        if let p = c.port, !((scheme == "http" && p == 80) || (scheme == "https" && p == 443)) { port = ":\(p)" }
        return GeckoLink(baseURL: "\(scheme)://\(hostPart)\(port)/", host: host, token: token)
    }

    /// The WebSocket address for a base URL.
    static func wsURL(_ baseURL: String) -> String {
        if baseURL.hasPrefix("https:") { return "wss:" + baseURL.dropFirst("https:".count) + "ws" }
        return "ws:" + baseURL.dropFirst("http:".count) + "ws"
    }

    /// scheme://host[:port] of a URL, the way WebKit reports page origins.
    static func origin(_ url: String) -> String? {
        guard let c = URLComponents(string: url), let scheme = c.scheme?.lowercased(), let h = c.host?.lowercased(), !h.isEmpty else { return nil }
        return origin(scheme: scheme, host: h, port: c.port ?? 0)
    }

    /// The same from its parts (port 0: the scheme's default).
    static func origin(scheme: String, host: String, port: Int) -> String {
        let s = scheme.lowercased()
        var h = host.lowercased()
        if h.hasPrefix("[") { h.removeFirst() }
        if h.hasSuffix("]") { h.removeLast() }
        if h.contains(":") { h = "[\(h)]" }
        let p = (port == 0 || (s == "http" && port == 80) || (s == "https" && port == 443)) ? "" : ":\(port)"
        return "\(s)://\(h)\(p)"
    }
}
