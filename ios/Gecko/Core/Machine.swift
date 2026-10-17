import Foundation

/// A computer running Gecko that this phone connects to.
struct Machine: Codable, Equatable, Identifiable {
    var id: String = UUID().uuidString
    var name: String
    /// scheme://host[:port]/
    var url: String
    /// Gecko's access token (kept in the Keychain, never in the list); empty
    /// when it has to be scanned again.
    var token: String = ""
    var lastUsed: Double = 0

    enum CodingKeys: String, CodingKey { case id, name, url, lastUsed }

    var host: String { (try? LinkParser.parse(url).host) ?? url }
}

/// Plain key-value storage (UserDefaults in the app, a dictionary in tests).
protocol KeyValueStore: AnyObject {
    func data(forKey key: String) -> Data?
    func set(_ data: Data?, forKey key: String)
}

/// Where tokens are kept (the Keychain in the app).
protocol TokenVault: AnyObject {
    func token(for id: String) -> String?
    @discardableResult func setToken(_ token: String, for id: String) -> Bool
    func removeToken(for id: String)
}

/// The saved machines. Names and addresses go in plain storage, each token
/// in the vault under the machine's id.
final class MachineStore {
    private let kv: KeyValueStore
    private let vault: TokenVault
    private let key = "machines"
    private let lock = NSLock()

    init(kv: KeyValueStore, vault: TokenVault) {
        self.kv = kv
        self.vault = vault
    }

    func list() -> [Machine] {
        lock.lock(); defer { lock.unlock() }
        return readAll()
    }

    private func readAll() -> [Machine] {
        guard let d = kv.data(forKey: key), var all = try? JSONDecoder().decode([Machine].self, from: d) else { return [] }
        for i in all.indices { all[i].token = vault.token(for: all[i].id) ?? "" }
        return all
    }

    func get(_ id: String) -> Machine? { list().first { $0.id == id } }

    /// The machine opened most recently, if any.
    func recent() -> Machine? { list().filter { $0.lastUsed > 0 }.max { $0.lastUsed < $1.lastUsed } }

    /// A saved machine at the same address, so scanning again updates it.
    func find(url: String) -> Machine? { list().first { $0.url == url } }

    /// Adds the machine, or replaces the one with its id.
    func save(_ m: Machine) {
        lock.lock(); defer { lock.unlock() }
        var all = readAll()
        if let i = all.firstIndex(where: { $0.id == m.id }) { all[i] = m } else { all.append(m) }
        if m.token.isEmpty { vault.removeToken(for: m.id) } else { vault.setToken(m.token, for: m.id) }
        write(all)
    }

    func remove(_ id: String) {
        lock.lock(); defer { lock.unlock() }
        vault.removeToken(for: id)
        write(readAll().filter { $0.id != id })
    }

    func touch(_ id: String, now: Double = Date().timeIntervalSince1970) {
        lock.lock(); defer { lock.unlock() }
        write(readAll().map { var m = $0; if m.id == id { m.lastUsed = now }; return m })
    }

    /// Forgets every machine and token (the UI tests start from nothing).
    func removeAll() {
        lock.lock(); defer { lock.unlock() }
        for m in readAll() { vault.removeToken(for: m.id) }
        kv.set(nil, forKey: key)
    }

    private func write(_ all: [Machine]) {
        kv.set(try? JSONEncoder().encode(all), forKey: key)
    }
}

/// In memory, for tests.
final class MemoryStore: KeyValueStore, TokenVault {
    var values: [String: Data] = [:]
    var tokens: [String: String] = [:]
    init() {}
    func data(forKey key: String) -> Data? { values[key] }
    func set(_ data: Data?, forKey key: String) { values[key] = data }
    func token(for id: String) -> String? { tokens[id] }
    func setToken(_ token: String, for id: String) -> Bool { tokens[id] = token; return true }
    func removeToken(for id: String) { tokens.removeValue(forKey: id) }
}
