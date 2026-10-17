import XCTest
@testable import Gecko

/// The parts with no UI: the same cases as the Android app's unit tests.
final class LinkParserTests: XCTestCase {
    let tok = "4dd99c1f25e82e4c078bc059dcab8d7f262727a88ea7ae6e"

    func testQrCodeLink() throws {
        // What openShare() puts in the QR code (server.ShareURLs).
        let l = try LinkParser.parse("http://192.168.1.20:7681/?token=\(tok)")
        XCTAssertEqual(l.baseURL, "http://192.168.1.20:7681/")
        XCTAssertEqual(l.host, "192.168.1.20")
        XCTAssertEqual(l.token, tok)
        XCTAssertFalse(l.https)
    }

    func testGeckoUrlOutput() throws {
        let l = try LinkParser.parse("http://100.101.102.103:7681/?token=\(tok)   (other devices)\n")
        XCTAssertEqual(l.baseURL, "http://100.101.102.103:7681/")
        XCTAssertEqual(l.token, tok)
    }

    func testLinkInsideText() throws {
        let l = try LinkParser.parse("Open this: http://10.0.0.5:7681/?token=\(tok).")
        XCTAssertEqual(l.baseURL, "http://10.0.0.5:7681/")
        XCTAssertEqual(l.token, tok)
    }

    func testTailscaleHttps() throws {
        let l = try LinkParser.parse("https://devbox.tail1234.ts.net/?token=\(tok)")
        XCTAssertEqual(l.baseURL, "https://devbox.tail1234.ts.net/")
        XCTAssertTrue(l.https)
    }

    func testDefaultPortsAndPathDropped() throws {
        XCTAssertEqual(try LinkParser.parse("https://box.example.com:443/some/path?token=\(tok)").baseURL, "https://box.example.com/")
        XCTAssertEqual(try LinkParser.parse("http://box.lan:80/?token=\(tok)").baseURL, "http://box.lan/")
    }

    func testBareHostGetsHttp() throws {
        let l = try LinkParser.parse("192.168.1.20:7681/?token=\(tok)")
        XCTAssertEqual(l.baseURL, "http://192.168.1.20:7681/")
        XCTAssertEqual(l.token, tok)
        XCTAssertNil(try LinkParser.parse("192.168.1.20:7681").token)
    }

    func testIPv6() throws {
        let l = try LinkParser.parse("http://[fd7a:115c:a1e0::1]:7681/?token=\(tok)")
        XCTAssertEqual(l.baseURL, "http://[fd7a:115c:a1e0::1]:7681/")
        XCTAssertEqual(l.host, "fd7a:115c:a1e0::1")
    }

    func testTokenAmongOtherParams() throws {
        XCTAssertEqual(try LinkParser.parse("http://h:1/?fit=80x24&token=\(tok)&x=1").token, tok)
        XCTAssertEqual(try LinkParser.parse("http://h:1/?token=abcd%2Befg%2Fhij%3D").token, "abcd+efg/hij=")
    }

    func testRejects() {
        for bad in ["", "   ", "ftp://host/?token=\(tok)", "javascript:alert(1)", "file:///etc/passwd", "http://user:pw@host/?token=\(tok)", "http://h/?token=<script>"] {
            XCTAssertThrowsError(try LinkParser.parse(bad), "accepted \(bad)") { e in
                XCTAssertFalse((e as? LinkError)?.message.isEmpty ?? true)
            }
        }
    }

    func testHelpers() {
        XCTAssertEqual(LinkParser.wsURL("http://h:7681/"), "ws://h:7681/ws")
        XCTAssertEqual(LinkParser.wsURL("https://h.ts.net/"), "wss://h.ts.net/ws")
        XCTAssertEqual(LinkParser.origin("http://H:7681/x?y"), "http://h:7681")
        XCTAssertEqual(LinkParser.origin("https://h:443/"), "https://h")
        XCTAssertNil(LinkParser.origin("about:blank"))
        XCTAssertEqual(LinkParser.origin(scheme: "http", host: "127.0.0.1", port: 7681), "http://127.0.0.1:7681")
        XCTAssertEqual(LinkParser.origin(scheme: "https", host: "h", port: 0), "https://h")
        XCTAssertEqual(LinkParser.origin(scheme: "http", host: "fd7a::1", port: 80), "http://[fd7a::1]")
    }
}

final class NetPolicyTests: XCTestCase {
    func testPrivateAddresses() {
        for h in ["localhost", "127.0.0.1", "10.0.2.2", "10.255.1.1", "172.16.0.1", "172.31.255.255", "192.168.1.20",
                  "169.254.3.4", "100.64.0.1", "100.127.255.254", "::1", "[::1]", "fd7a:115c:a1e0::1", "fe80::1%en0", "::ffff:192.168.0.1"] {
            XCTAssertTrue(NetPolicy.cleartextAllowed(h), h)
        }
    }

    func testPublicAddresses() {
        for h in ["8.8.8.8", "172.32.0.1", "172.15.0.1", "192.169.0.1", "100.128.0.1", "100.63.0.1", "1.2.3.4", "2001:db8::1", "::ffff:8.8.8.8", "", "999.1.1.1"] {
            XCTAssertFalse(NetPolicy.cleartextAllowed(h), h)
        }
    }

    func testNames() {
        for h in ["devbox", "devbox.local", "nas.lan", "box.home.arpa", "devbox.tail1234.ts.net", "x.internal"] { XCTAssertTrue(NetPolicy.cleartextAllowed(h), h) }
        for h in ["example.com", "evil.ts.net.example.com", "local.example.org"] { XCTAssertFalse(NetPolicy.cleartextAllowed(h), h) }
    }

    func testResolvedNames() {
        XCTAssertTrue(NetPolicy.cleartextAllowedResolving("gecko.example.com") { _ in [[192, 168, 1, 9]] })
        XCTAssertFalse(NetPolicy.cleartextAllowedResolving("gecko.example.com") { _ in [[192, 168, 1, 9], [8, 8, 8, 8]] })
        XCTAssertFalse(NetPolicy.cleartextAllowedResolving("gecko.example.com") { _ in [] })
        XCTAssertFalse(NetPolicy.cleartextAllowedResolving("gecko.example.com") { _ in nil })
        XCTAssertNotNil(NetPolicy.resolveAll("localhost"))
    }
}

final class AgentWatchTests: XCTestCase {
    func session(_ id: String, _ status: String?, detail: String = "", name: String = "", choices: String = "") -> String {
        var agent = ""
        if let status {
            agent = #","agent":{"name":"claude","status":"\#(status)","detail":"\#(detail)""# + (choices.isEmpty ? "" : #","choices":\#(choices)"#) + "}"
        }
        return #"{"id":"\#(id)","host":"devbox","workspace":"default","name":"\#(name)","kind":"agent","cwd":"/home/me/app""# + agent + "}"
    }

    func testNeedsInputAfterWorking() {
        let w = AgentWatch()
        XCTAssertTrue(w.onMessage(#"{"t":"sessions","sessions":[\#(session("a", "working"))]}"#).isEmpty)
        let ev = w.onMessage(#"{"t":"session","session":\#(session("a", "needs-input", detail: "Do you want to run this command?", choices: #"[{"key":"1","label":"Yes"},{"key":"2","label":"No"}]"#))}"#)
        XCTAssertEqual(ev.count, 1)
        XCTAssertEqual(ev[0].title, "claude needs you · claude")
        XCTAssertEqual(ev[0].body, "Do you want to run this command?")
        XCTAssertEqual(ev[0].choices, [.init(key: "1", label: "Yes"), .init(key: "2", label: "No")])
        XCTAssertTrue(ev[0].needsInput)
        XCTAssertEqual(ev[0].host, "devbox")
        // The same state again is not news.
        XCTAssertTrue(w.onMessage(#"{"t":"session","session":\#(session("a", "needs-input"))}"#).isEmpty)
    }

    func testFinished() {
        let w = AgentWatch()
        _ = w.onMessage(#"{"t":"session","session":\#(session("a", "working", name: "api"))}"#)
        let ev = w.onMessage(#"{"t":"session","session":\#(session("a", "idle", detail: "Done.", name: "api"))}"#)
        XCTAssertEqual(ev.map(\.title), ["claude finished · api"])
        XCTAssertTrue(ev[0].choices.isEmpty)
    }

    func testFirstSightIsQuiet() {
        // Already waiting when we connect: the web UI doesn't notify either.
        let w = AgentWatch()
        XCTAssertTrue(w.onMessage(#"{"t":"sessions","sessions":[\#(session("a", "needs-input"))]}"#).isEmpty)
        XCTAssertTrue(w.onMessage(#"{"t":"session","session":\#(session("b", "needs-input"))}"#).isEmpty)
    }

    func testReconnectCatchesUp() {
        let w = AgentWatch()
        _ = w.onMessage(#"{"t":"sessions","sessions":[\#(session("a", "working"))]}"#)
        XCTAssertEqual(w.onMessage(#"{"t":"sessions","sessions":[\#(session("a", "needs-input"))]}"#).count, 1)
    }

    func testSeededFromLastTime() {
        // A check in the background: what changed since the app last looked.
        let w = AgentWatch(seed: ["a": "working", "b": "needs-input"])
        let ev = w.onMessage(#"{"t":"sessions","sessions":[\#(session("a", "needs-input")),\#(session("b", "needs-input"))]}"#)
        XCTAssertEqual(ev.map(\.sessionId), ["a"])
        XCTAssertEqual(w.status, ["a": "needs-input", "b": "needs-input"])
    }

    func testResolvedWhenAnsweredOrClosed() {
        let w = AgentWatch()
        _ = w.onMessage(#"{"t":"session","session":\#(session("a", "working"))}"#)
        _ = w.onMessage(#"{"t":"session","session":\#(session("a", "needs-input"))}"#)
        _ = w.onMessage(#"{"t":"session","session":\#(session("a", "working"))}"#)
        XCTAssertEqual(w.resolved, ["a"])
        _ = w.onMessage(#"{"t":"session","session":\#(session("a", "needs-input"))}"#)
        _ = w.onMessage(#"{"t":"closed","id":"a"}"#)
        XCTAssertEqual(w.resolved, ["a"])
    }

    func testJunkIgnored() {
        let w = AgentWatch()
        XCTAssertTrue(w.onMessage("nope").isEmpty)
        XCTAssertTrue(w.onMessage(#"{"t":"hello","host":"x"}"#).isEmpty)
        XCTAssertEqual(w.selfName, "x")
    }

    func testTabLabel() {
        XCTAssertEqual(AgentWatch.tabLabel(["kind": "shell", "cwd": "/home/me/app/"]), "app")
        XCTAssertEqual(AgentWatch.tabLabel(["kind": "ssh", "remote": "me@box"]), "me@box")
        XCTAssertEqual(AgentWatch.tabLabel(["kind": "shell", "cwd": "/"]), "shell")
    }

    func testWire() {
        let f = [UInt8](Wire.inputFrame(sessionId: "devbox/abc", data: "1")!)
        XCTAssertEqual(f[0], 2)
        XCTAssertEqual(f[1], 10)
        XCTAssertEqual(String(decoding: f[2..<12], as: UTF8.self), "devbox/abc")
        XCTAssertEqual(f.count, 2 + 10 + 8 + 1)
        XCTAssertEqual(f.last, UInt8(ascii: "1"))
        XCTAssertEqual(Wire.keyText("enter"), "\r")
        XCTAssertEqual(Wire.keyText("y"), "y")
        XCTAssertNil(Wire.inputFrame(sessionId: String(repeating: "x", count: 300), data: ""))
    }
}

final class BuildIdTests: XCTestCase {
    func testCommitAndLocalTime() {
        XCTAssertEqual(BuildId.format(commit: "a1b2c3d", time: "2026-10-16T23:15:00Z", zone: TimeZone(identifier: "Asia/Jerusalem")!), "a1b2c3d · Oct 17 02:15")
        XCTAssertEqual(BuildId.format(commit: "a1b2c3d-dirty", time: "2026-10-16T23:15:00Z", zone: TimeZone(identifier: "UTC")!), "a1b2c3d-dirty · Oct 16 23:15")
        XCTAssertEqual(BuildId.format(commit: "a1b2c3d", time: "2027-01-05T09:03:59Z", zone: TimeZone(identifier: "UTC")!), "a1b2c3d · Jan 5 09:03")
    }

    func testWithoutATime() {
        let utc = TimeZone(identifier: "UTC")!
        XCTAssertEqual(BuildId.format(commit: "a1b2c3d", time: "", zone: utc), "a1b2c3d")
        XCTAssertEqual(BuildId.format(commit: "a1b2c3d", time: "yesterday", zone: utc), "a1b2c3d")
        XCTAssertEqual(BuildId.format(commit: "", time: "", zone: utc), "unknown")
        XCTAssertEqual(BuildId.parse(json: Data(#"{"commit":"abc1234","time":"2026-10-16T23:15:00Z"}"#.utf8), zone: utc), "abc1234 · Oct 16 23:15")
        XCTAssertEqual(BuildId.parse(json: nil), "unknown")
    }
}

final class MachineStoreTests: XCTestCase {
    func testTokensStayOutOfTheList() {
        let mem = MemoryStore()
        let s = MachineStore(kv: mem, vault: mem)
        let m = Machine(name: "devbox", url: "http://192.168.1.20:7681/", token: "secret-token-123")
        s.save(m)
        XCTAssertEqual(s.list(), [m])
        XCTAssertEqual(mem.tokens[m.id], "secret-token-123")
        let raw = String(decoding: mem.values["machines"]!, as: UTF8.self)
        XCTAssertFalse(raw.contains("secret-token-123"), raw)
        XCTAssertEqual(s.find(url: "http://192.168.1.20:7681/")?.id, m.id)
    }

    func testRecentRenameRemove() {
        let mem = MemoryStore()
        let s = MachineStore(kv: mem, vault: mem)
        let a = Machine(name: "a", url: "http://a:1/", token: "tttttttt")
        let b = Machine(name: "b", url: "http://b:1/", token: "uuuuuuuu")
        s.save(a)
        s.save(b)
        XCTAssertNil(s.recent())
        s.touch(b.id, now: 10)
        XCTAssertEqual(s.recent()?.id, b.id)
        var renamed = s.get(a.id)!
        renamed.name = "renamed"
        s.save(renamed)
        XCTAssertEqual(s.list().map(\.name), ["renamed", "b"])
        s.remove(a.id)
        XCTAssertEqual(s.list().map(\.id), [b.id])
        XCTAssertNil(mem.tokens[a.id])
        s.removeAll()
        XCTAssertTrue(s.list().isEmpty)
        XCTAssertTrue(mem.tokens.isEmpty)
    }
}
