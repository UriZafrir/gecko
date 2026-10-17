import Foundation

/// Something worth a notification: an agent waiting for you, or done.
struct AgentEvent: Equatable {
    let sessionId: String
    let title: String
    let body: String
    let needsInput: Bool
    /// The answers on screen: key to press ("1", "y", "enter") and what it says.
    var choices: [Choice] = []
    /// The machine the tab runs on (as that Gecko names it).
    var host: String = ""

    struct Choice: Equatable {
        let key: String
        let label: String
    }
}

/// Follows Gecko's session list (the "sessions", "session" and "closed"
/// messages) and reports when an agent starts waiting for input or finishes
/// working, the same moments the web UI notifies about (AgentWatch.kt in
/// the Android app).
final class AgentWatch {
    private(set) var status: [String: String] = [:]
    /// Sessions known to have no agent status (nil), kept apart from "unknown".
    private var known: Set<String> = []
    private var hostOf: [String: String] = [:]

    /// Sessions whose notification should go away (the agent moved on, or the tab closed).
    private(set) var resolved: [String] = []
    /// The Gecko's own name (its hello).
    private(set) var selfName = ""

    init() {}

    /// Starts from what was known before (e.g. when the app last ran), so a
    /// fresh list reports what changed since.
    init(seed: [String: String]) {
        status = seed
        known = Set(seed.keys)
    }

    func onMessage(_ text: String) -> [AgentEvent] {
        resolved = []
        guard let data = text.data(using: .utf8),
              let m = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] else { return [] }
        switch m["t"] as? String ?? "" {
        case "hello":
            selfName = m["host"] as? String ?? ""
            return []
        case "sessions":
            let arr = m["sessions"] as? [[String: Any]] ?? []
            let host = m["host"] as? String ?? ""
            var seen = Set<String>()
            var out: [AgentEvent] = []
            for s in arr {
                seen.insert(s["id"] as? String ?? "")
                // A list after reconnecting: compared with what was known.
                out += update(s)
            }
            // A list for one machine of several leaves the others alone.
            let gone = known.filter { !seen.contains($0) && (host.isEmpty || hostOf[$0] == nil || hostOf[$0] == host) }
            for id in gone { forget(id) }
            return out
        case "session":
            if let s = m["session"] as? [String: Any] { return update(s) }
            return []
        case "closed":
            forget(m["id"] as? String ?? "")
            return []
        default:
            return []
        }
    }

    private func forget(_ id: String) {
        if status.removeValue(forKey: id) == "needs-input" { resolved.append(id) }
        known.remove(id)
        hostOf.removeValue(forKey: id)
    }

    private func update(_ s: [String: Any]) -> [AgentEvent] {
        guard let id = s["id"] as? String, !id.isEmpty else { return [] }
        let agent = s["agent"] as? [String: Any]
        let st = agent?["status"] as? String
        let now = (st?.isEmpty ?? true) ? nil : st
        hostOf[id] = s["host"] as? String ?? ""
        let wasKnown = known.contains(id)
        let prev = status[id]
        known.insert(id)
        if let now { status[id] = now } else { status.removeValue(forKey: id) }
        if prev == "needs-input" && now != "needs-input" { resolved.append(id) }
        guard wasKnown, let prev, let now, prev != now, let agent else { return [] }
        let finished = prev == "working" && now == "idle"
        if now != "needs-input" && !finished { return [] }
        let name = (agent["name"] as? String).flatMap { $0.isEmpty ? nil : $0 } ?? "agent"
        let title = "\(name) \(finished ? "finished" : "needs you") · \(Self.tabLabel(s))"
        var choices: [AgentEvent.Choice] = []
        for c in agent["choices"] as? [[String: Any]] ?? [] {
            let key = c["key"] as? String ?? ""
            if key.isEmpty { continue }
            let label = c["label"] as? String ?? ""
            choices.append(.init(key: key, label: label.isEmpty ? key : label))
        }
        return [AgentEvent(sessionId: id, title: title, body: agent["detail"] as? String ?? "", needsInput: !finished,
                           choices: finished ? [] : choices, host: s["host"] as? String ?? "")]
    }

    /// The tab's title as the web UI shows it (main.js tabLabel), in short.
    static func tabLabel(_ s: [String: Any]) -> String {
        func str(_ v: Any?) -> String? { (v as? String).flatMap { $0.isEmpty ? nil : $0 } }
        if let n = str(s["name"]) { return n }
        if let n = str((s["agent"] as? [String: Any])?["name"]) { return n }
        let kind = s["kind"] as? String ?? ""
        if kind == "ssh" || kind == "mosh", let r = str(s["remote"]) { return r }
        if kind != "shell", let p = str(s["proc"]) { return p }
        if let r = str(s["repo"]) { return r }
        var cwd = s["cwd"] as? String ?? ""
        while cwd.hasSuffix("/") { cwd.removeLast() }
        let last = cwd.split(separator: "/").last.map(String.init) ?? ""
        return last.isEmpty ? "shell" : last
    }
}
