import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

/// What a machine's /api/info says, asked with the token.
enum GeckoInfo {
    struct Info {
        /// The machine's name, as its Gecko calls itself.
        var host: String?
        /// Its build id ("a1b2c3d · Oct 17 02:15 (v0.2.0)"), "" when it's too old to say.
        var build: String
    }

    enum Result {
        case ok(Info)
        case refused
        case unreachable(String)
    }

    static func fetch(baseURL: String, token: String, timeout: TimeInterval = 8, done: @escaping (Result) -> Void) {
        guard let url = URL(string: baseURL + "api/info") else { return done(.unreachable("Bad address")) }
        var req = URLRequest(url: url, cachePolicy: .reloadIgnoringLocalCacheData, timeoutInterval: timeout)
        req.setValue("Bearer " + token, forHTTPHeaderField: "Authorization")
        let cfg = URLSessionConfiguration.ephemeral
        cfg.timeoutIntervalForResource = timeout
        let session = URLSession(configuration: cfg)
        session.dataTask(with: req) { data, resp, err in
            defer { session.finishTasksAndInvalidate() }
            if let err { return done(.unreachable(err.localizedDescription)) }
            let code = (resp as? HTTPURLResponse)?.statusCode ?? 0
            if code == 401 || code == 403 { return done(.refused) }
            guard (200..<300).contains(code) else { return done(.unreachable("HTTP \(code)")) }
            let o = data.flatMap { try? JSONSerialization.jsonObject(with: $0) } as? [String: Any] ?? [:]
            done(.ok(parse(o)))
        }.resume()
    }

    static func parse(_ o: [String: Any]) -> Info {
        let host = (o["host"] as? String).flatMap { $0.isEmpty ? nil : $0 }
        let commit = o["commit"] as? String ?? ""
        guard !commit.isEmpty else { return Info(host: host, build: "") }
        let id = BuildId.format(commit: commit, time: o["built"] as? String ?? "")
        let v = o["version"] as? String ?? ""
        return Info(host: host, build: v.isEmpty || v == "dev" ? id : "\(id) (\(v))")
    }
}
