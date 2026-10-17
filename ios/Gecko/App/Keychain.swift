import Foundation
import Security

/// Each machine's token, in the Keychain: readable once the phone has been
/// unlocked after starting (so a background check can use it), and never
/// moved to another device by a backup or a transfer.
final class Keychain: TokenVault {
    private let service = "io.github.geckoterm.ios.token"

    /// The last error, for diagnostics.
    private(set) var lastStatus: OSStatus = errSecSuccess

    private func query(_ id: String) -> [String: Any] {
        [kSecClass as String: kSecClassGenericPassword,
         kSecAttrService as String: service,
         kSecAttrAccount as String: id]
    }

    func token(for id: String) -> String? {
        var q = query(id)
        q[kSecReturnData as String] = true
        q[kSecMatchLimit as String] = kSecMatchLimitOne
        var out: CFTypeRef?
        lastStatus = SecItemCopyMatching(q as CFDictionary, &out)
        guard lastStatus == errSecSuccess, let d = out as? Data else { return nil }
        return String(data: d, encoding: .utf8)
    }

    @discardableResult
    func setToken(_ token: String, for id: String) -> Bool {
        let attrs: [String: Any] = [
            kSecValueData as String: Data(token.utf8),
            kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly,
        ]
        lastStatus = SecItemUpdate(query(id) as CFDictionary, attrs as CFDictionary)
        if lastStatus == errSecItemNotFound {
            let q = query(id).merging(attrs) { $1 }
            lastStatus = SecItemAdd(q as CFDictionary, nil)
        }
        return lastStatus == errSecSuccess
    }

    func removeToken(for id: String) {
        lastStatus = SecItemDelete(query(id) as CFDictionary)
    }
}
