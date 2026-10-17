import SwiftUI
import UIKit
import UserNotifications

/// The app's state: the saved machines, the one on screen, and settings.
@MainActor
final class AppModel: ObservableObject {
    static let shared = AppModel()

    let store: MachineStore
    @Published private(set) var machines: [Machine] = []
    @Published private(set) var openMachine: Machine?
    /// Bumped to load a machine again from scratch.
    @Published private(set) var openCount = 0
    /// The page's theme is dark (the status bar's text then is light).
    @Published var terminalDark = true
    @Published var keepScreenOn: Bool { didSet { defaults.set(keepScreenOn, forKey: "keepScreenOn"); applyIdleTimer() } }
    @Published var notifyInBackground: Bool { didSet { defaults.set(notifyInBackground, forKey: "notifyInBackground") } }

    /// A tab to show once the machine's page is ready (from a notification).
    var pendingSession: String?
    weak var terminal: TerminalViewController?
    private let defaults = UserDefaults.standard

    nonisolated static func makeStore() -> MachineStore {
        MachineStore(kv: DefaultsStore(), vault: Keychain())
    }

    /// UI tests: GECKO_UITEST=1 shows a probe of the page's state.
    static let uiTest = ProcessInfo.processInfo.environment["GECKO_UITEST"] == "1"

    private init() {
        store = Self.makeStore()
        keepScreenOn = UserDefaults.standard.bool(forKey: "keepScreenOn")
        notifyInBackground = UserDefaults.standard.object(forKey: "notifyInBackground") as? Bool ?? true
        let env = ProcessInfo.processInfo.environment
        if Self.uiTest && env["GECKO_UITEST_RESET"] == "1" {
            store.removeAll()
            keepScreenOn = false
        }
        reload()
        // UI tests that are not about connecting: this machine, opened at once.
        if Self.uiTest, let link = env["GECKO_UITEST_OPEN"], let l = try? LinkParser.parse(link), let token = l.token {
            let m = store.find(url: l.baseURL).map { var x = $0; x.token = token; return x }
                ?? Machine(name: env["GECKO_UITEST_NAME"] ?? "ci-mac", url: l.baseURL, token: token)
            store.save(m)
            reload()
            open(m)
        }
    }

    /// UI tests only: gecko-uitest://js?c=<script> runs a script in the page,
    /// for the steps of the feature tour a synthesized finger can't do
    /// reliably (a button deep in the page). Ignored outside UI tests.
    func uiTestURL(_ url: URL) {
        guard Self.uiTest, url.scheme == "gecko-uitest", url.host == "js",
              let c = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems?.first(where: { $0.name == "c" })?.value
        else { return }
        terminal?.eval(c)
    }

    func reload() {
        machines = store.list().sorted { $0.lastUsed > $1.lastUsed }
        WatchHub.shared.sync(store.list())
    }

    func open(_ m: Machine, session: String? = nil) {
        store.touch(m.id)
        pendingSession = session
        openMachine = store.get(m.id) ?? m
        openCount += 1
        WatchHub.shared.foregroundMachine = m.id
        applyIdleTimer()
        reload()
        askForNotificationsOnce()
    }

    func leave() {
        openMachine = nil
        terminal = nil
        WatchHub.shared.foregroundMachine = nil
        applyIdleTimer()
        reload()
    }

    /// A tapped notification: its machine, then its tab.
    func openFromNotification(machineId: String, session: String) {
        Notifier.cancel(machineId: machineId, sessionId: session)
        if openMachine?.id == machineId, let t = terminal {
            if !session.isEmpty { t.focus(session: session) }
            return
        }
        guard let m = store.get(machineId) else { return }
        open(m, session: session.isEmpty ? nil : session)
    }

    private func applyIdleTimer() {
        UIApplication.shared.isIdleTimerDisabled = keepScreenOn && openMachine != nil
    }

    // MARK: notifications

    /// The first time a machine opens: notifications are how Gecko tells
    /// you an agent needs you.
    private func askForNotificationsOnce() {
        if defaults.bool(forKey: "askedNotifications") { return }
        defaults.set(true, forKey: "askedNotifications")
        Notifier.requestPermission { _ in }
    }

    /// "Notify me when agents need input" in the web UI's palette.
    func enableNotifications(say: @escaping (String) -> Void) {
        Notifier.requestPermission { granted in
            Task { @MainActor in
                say(granted
                    ? "Gecko will notify you when an agent needs you."
                    : "Notifications are off for Gecko. Turn them on in Settings, Notifications, Gecko.")
            }
        }
    }

    // MARK: life cycle

    func phaseChanged(_ p: ScenePhase) {
        switch p {
        case .active:
            WatchHub.shared.foregroundMachine = openMachine?.id
            Background.endKeepAlive()
            WatchHub.shared.sync(store.list())
        case .background:
            // The web UI stops with the app; the app's own connections tell
            // about agents for as long as iOS lets it run (about 30 seconds),
            // then a refresh now and then, when iOS allows one.
            WatchHub.shared.foregroundMachine = nil
            if notifyInBackground {
                Background.keepAlive()
                Background.scheduleRefresh()
            } else {
                Background.saveStatuses()
                WatchHub.shared.stopAll()
            }
        default:
            break
        }
    }
}

/// UserDefaults as the machine list's storage.
final class DefaultsStore: KeyValueStore {
    func data(forKey key: String) -> Data? { UserDefaults.standard.data(forKey: key) }
    func set(_ data: Data?, forKey key: String) { UserDefaults.standard.set(data, forKey: key) }
}

/// The app's build id (Generated/build-id.json, from `make ios-web`).
enum AppBuild {
    static let json: Data? = Bundle.main.url(forResource: "build-id", withExtension: "json", subdirectory: "Generated").flatMap { try? Data(contentsOf: $0) }
    static var id: String { BuildId.parse(json: json) }
    static var commit: String { BuildId.commit(json: json) }

    /// Tapped: copies "Gecko for iOS <id>".
    static func copy() {
        UIPasteboard.general.string = "Gecko for iOS " + id
    }
}
