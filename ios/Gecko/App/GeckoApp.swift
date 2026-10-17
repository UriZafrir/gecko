import SwiftUI
import UIKit
import UserNotifications

@main
struct GeckoApp: App {
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    @StateObject private var model = AppModel.shared

    var body: some Scene {
        WindowGroup {
            RootView().environmentObject(model)
        }
    }
}

/// The machine list, or one machine's Gecko filling the screen.
struct RootView: View {
    @EnvironmentObject var model: AppModel
    @Environment(\.scenePhase) private var phase

    var body: some View {
        Group {
            if let m = model.openMachine {
                TerminalScreen(machine: m)
                    .id(m.id + "/" + String(model.openCount))
                    .ignoresSafeArea()
                    .preferredColorScheme(model.terminalDark ? .dark : .light)
            } else {
                ConnectView()
            }
        }
        .onChange(of: phase) { p in model.phaseChanged(p) }
    }
}

final class AppDelegate: NSObject, UIApplicationDelegate {
    func application(_ application: UIApplication, didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil) -> Bool {
        UNUserNotificationCenter.current().delegate = NotificationDelegate.shared
        Background.register()
        return true
    }
}

/// Notifications while Gecko is open show as banners; tapping one opens its
/// tab; an answer button sends that key to the tab without opening the app.
final class NotificationDelegate: NSObject, UNUserNotificationCenterDelegate {
    static let shared = NotificationDelegate()

    func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification,
                                withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void) {
        completionHandler([.banner, .list, .sound])
    }

    func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse,
                                withCompletionHandler completionHandler: @escaping () -> Void) {
        let info = response.notification.request.content.userInfo
        let machineId = info["machine"] as? String ?? ""
        let session = info["session"] as? String ?? ""
        let action = response.actionIdentifier
        if action.hasPrefix(Notifier.answerPrefix) {
            let key = String(action.dropFirst(Notifier.answerPrefix.count))
            let store = AppModel.makeStore()
            guard let m = store.get(machineId), !m.token.isEmpty, !session.isEmpty else { return completionHandler() }
            Notifier.cancel(machineId: machineId, sessionId: session)
            Answer.send(machine: m, sessionId: session, key: key) { completionHandler() }
            return
        }
        if action == UNNotificationDefaultActionIdentifier {
            Task { @MainActor in
                AppModel.shared.openFromNotification(machineId: machineId, session: session)
                completionHandler()
            }
            return
        }
        completionHandler()
    }
}
