import Foundation

/// Which build this is: the commit (with -dirty when the tree had changes)
/// and when it was built, e.g. "a1b2c3d · Oct 17 02:15" in this phone's time
/// zone. The same format as the Android app and the web UI (web/src/build.js);
/// `make ios-web` writes the id into the app (Generated/build-id.json), the
/// same one as the web UI inside it.
enum BuildId {
    static func format(commit: String, time: String, zone: TimeZone = .current) -> String {
        let c = commit.isEmpty ? "unknown" : commit
        let iso = ISO8601DateFormatter()
        iso.formatOptions = [.withInternetDateTime]
        var date = iso.date(from: time)
        if date == nil {
            iso.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
            date = iso.date(from: time)
        }
        guard let d = date else { return c }
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        f.timeZone = zone
        f.dateFormat = "MMM d HH:mm"
        return "\(c) · \(f.string(from: d))"
    }

    /// From build-id.json: {"commit": "...", "time": "..."}.
    static func parse(json: Data?, zone: TimeZone = .current) -> String {
        guard let json, let o = (try? JSONSerialization.jsonObject(with: json)) as? [String: Any] else { return "unknown" }
        return format(commit: o["commit"] as? String ?? "", time: o["time"] as? String ?? "", zone: zone)
    }

    /// The commit alone ("a1b2c3d" or "a1b2c3d-dirty").
    static func commit(json: Data?) -> String {
        guard let json, let o = (try? JSONSerialization.jsonObject(with: json)) as? [String: Any] else { return "" }
        return o["commit"] as? String ?? ""
    }
}
