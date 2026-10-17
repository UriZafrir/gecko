import Foundation
#if canImport(Darwin)
import Darwin
#else
import Glibc
#endif

/// Where plain http is allowed. Gecko's link carries its access token, so it
/// may travel unencrypted only inside a private network: this device, a LAN
/// (10/8, 172.16/12, 192.168/16, link-local), a Tailscale tailnet
/// (100.64/10, fd7a:115c:a1e0::/48) or IPv6 unique local addresses.
/// Anywhere else needs https.
///
/// App Transport Security can't express address ranges, so Info.plist allows
/// plain http and the app checks every machine here before it connects (the
/// same rule as the Android app's NetPolicy.kt).
enum NetPolicy {
    private static let privateSuffixes = [".local", ".lan", ".home", ".home.arpa", ".internal", ".localdomain", ".ts.net", ".localhost"]

    /// True when http to host is fine without looking the name up.
    static func cleartextAllowed(_ host: String) -> Bool {
        var h = host.lowercased().trimmingCharacters(in: .whitespaces)
        if h.hasPrefix("[") { h.removeFirst() }
        if h.hasSuffix("]") { h.removeLast() }
        while h.hasSuffix(".") { h.removeLast() }
        if h.isEmpty { return false }
        if h == "localhost" { return true }
        if let v4 = parseIPv4(h) { return privateV4(v4) }
        if h.contains(":") { return parseIPv6(h).map(privateV6) ?? false }
        // A single-label name (a LAN host, Tailscale MagicDNS) or a private suffix.
        if !h.contains(".") { return true }
        return privateSuffixes.contains { h.hasSuffix($0) }
    }

    /// For other names: allowed when every address it resolves to is private.
    /// Blocking (DNS); call it off the main thread.
    static func cleartextAllowedResolving(_ host: String, resolve: (String) -> [[UInt8]]? = resolveAll) -> Bool {
        if cleartextAllowed(host) { return true }
        guard let addrs = resolve(host), !addrs.isEmpty else { return false }
        return addrs.allSatisfy(privateAddress)
    }

    /// 4 or 16 bytes.
    static func privateAddress(_ b: [UInt8]) -> Bool {
        if b.count == 4 { return privateV4(b) }
        if b.count == 16 { return privateV6(b) }
        return false
    }

    static func privateV4(_ x: [UInt8]) -> Bool {
        guard x.count == 4 else { return false }
        return x[0] == 127 || x[0] == 10 ||
            (x[0] == 172 && (16...31).contains(x[1])) ||
            (x[0] == 192 && x[1] == 168) ||
            (x[0] == 169 && x[1] == 254) ||
            (x[0] == 100 && (64...127).contains(x[1])) // CGNAT: Tailscale
    }

    static func privateV6(_ b: [UInt8]) -> Bool {
        guard b.count == 16 else { return false }
        if b[0..<15].allSatisfy({ $0 == 0 }) && b[15] == 1 { return true } // ::1
        if b[0] & 0xfe == 0xfc { return true } // fc00::/7, Tailscale included
        if b[0] == 0xfe && b[1] & 0xc0 == 0x80 { return true } // fe80::/10
        // IPv4-mapped ::ffff:a.b.c.d
        if b[0..<10].allSatisfy({ $0 == 0 }) && b[10] == 0xff && b[11] == 0xff { return privateV4(Array(b[12..<16])) }
        return false
    }

    static func parseIPv4(_ s: String) -> [UInt8]? {
        let parts = s.split(separator: ".", omittingEmptySubsequences: false)
        guard parts.count == 4 else { return nil }
        var out: [UInt8] = []
        for p in parts {
            guard !p.isEmpty, p.count <= 3, p.allSatisfy({ $0.isASCII && $0.isNumber }), let n = Int(p), n <= 255 else { return nil }
            out.append(UInt8(n))
        }
        return out
    }

    /// An IPv6 literal to 16 bytes, without DNS; nil if it isn't one.
    static func parseIPv6(_ s0: String) -> [UInt8]? {
        let s = s0.split(separator: "%", maxSplits: 1, omittingEmptySubsequences: false).first.map(String.init) ?? s0
        var addr = in6_addr()
        let ok = s.withCString { inet_pton(AF_INET6, $0, &addr) } == 1
        guard ok else { return nil }
        return withUnsafeBytes(of: &addr) { Array($0) }
    }

    /// Every address a name resolves to (4 or 16 bytes each), nil on failure.
    static func resolveAll(_ host: String) -> [[UInt8]]? {
        var hints = addrinfo()
        hints.ai_family = AF_UNSPEC
        #if canImport(Darwin)
        hints.ai_socktype = SOCK_STREAM
        #else
        hints.ai_socktype = Int32(SOCK_STREAM.rawValue)
        #endif
        var res: UnsafeMutablePointer<addrinfo>?
        guard getaddrinfo(host, nil, &hints, &res) == 0, let first = res else { return nil }
        defer { freeaddrinfo(first) }
        var out: [[UInt8]] = []
        var p: UnsafeMutablePointer<addrinfo>? = first
        while let ai = p {
            if let sa = ai.pointee.ai_addr {
                if ai.pointee.ai_family == AF_INET {
                    sa.withMemoryRebound(to: sockaddr_in.self, capacity: 1) { sin in
                        var a = sin.pointee.sin_addr
                        out.append(withUnsafeBytes(of: &a) { Array($0) })
                    }
                } else if ai.pointee.ai_family == AF_INET6 {
                    sa.withMemoryRebound(to: sockaddr_in6.self, capacity: 1) { sin6 in
                        var a = sin6.pointee.sin6_addr
                        out.append(withUnsafeBytes(of: &a) { Array($0) })
                    }
                }
            }
            p = ai.pointee.ai_next
        }
        return out
    }
}
