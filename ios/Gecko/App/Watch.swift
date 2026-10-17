import Foundation
import UIKit
import BackgroundTasks
import UserNotifications

/// The app's own small connection to each saved machine: it reads only the
/// list of tabs and their agents' state (never terminal output) and posts a
/// notification when an agent needs you or finishes, the way the Android
/// app's WatchService does. iOS runs it while the app is open and for the
/// short time it gives an app that just went to the background.
final class WatchHub {
    static let shared = WatchHub()

    private let lock = NSLock()
    private var watchers: [String: MachineWatcher] = [:]
    private var _foreground: String?

    /// The machine on screen: its web UI notifies about it itself.
    var foregroundMachine: String? {
        get { lock.lock(); defer { lock.unlock() }; return _foreground }
        set { lock.lock(); _foreground = newValue; lock.unlock() }
    }

    /// Follows the saved machines: new ones join, removed or changed ones restart.
    func sync(_ machines: [Machine]) {
        lock.lock()
        let want = Dictionary(machines.filter { !$0.token.isEmpty }.map { ($0.id, $0) }, uniquingKeysWith: { a, _ in a })
        var start: [MachineWatcher] = []
        for (id, w) in watchers where want[id] == nil || !w.same(as: want[id]!) {
            w.stop()
            watchers.removeValue(forKey: id)
        }
        for (id, m) in want where watchers[id] == nil {
            let w = MachineWatcher(machine: m, seed: Background.statuses(for: id))
            watchers[id] = w
            start.append(w)
        }
        lock.unlock()
        for w in start { w.start() }
    }

    func stopAll() {
        lock.lock()
        let all = Array(watchers.values)
        watchers.removeAll()
        lock.unlock()
        for w in all { w.stop() }
    }

    /// What each connection knows now, kept for the next background check.
    func statuses() -> [String: [String: String]] {
        lock.lock(); defer { lock.unlock() }
        return watchers.mapValues { $0.statuses }
    }

    /// One look at every machine (a background refresh): connect, read the
    /// tab list once, notify about what changed since last time.
    static func checkOnce(_ machines: [Machine], timeout: TimeInterval = 20, done: @escaping () -> Void) {
        let group = DispatchGroup()
        var keep: [MachineWatcher] = []
        for m in machines where !m.token.isEmpty {
            group.enter()
            let w = MachineWatcher(machine: m, seed: Background.statuses(for: m.id), once: { w in
                Background.save(statuses: w.statuses, for: m.id)
                group.leave()
            })
            keep.append(w)
            w.start()
        }
        group.notify(queue: .main) {
            for w in keep { w.stop() }
            done()
        }
        DispatchQueue.main.asyncAfter(deadline: .now() + timeout) {
            for w in keep { w.giveUp() }
        }
    }
}

final class MachineWatcher: NSObject, URLSessionWebSocketDelegate {
    let machine: Machine
    private let watch: AgentWatch
    private let queue = OperationQueue()
    private var session: URLSession!
    private var task: URLSessionWebSocketTask?
    private var stopped = false
    private var backoff: TimeInterval = 1
    private var once: ((MachineWatcher) -> Void)?
    private var ping: Timer?

    init(machine: Machine, seed: [String: String], once: ((MachineWatcher) -> Void)? = nil) {
        self.machine = machine
        self.watch = seed.isEmpty ? AgentWatch() : AgentWatch(seed: seed)
        self.once = once
        super.init()
        queue.maxConcurrentOperationCount = 1
        session = URLSession(configuration: .ephemeral, delegate: self, delegateQueue: queue)
    }

    func same(as m: Machine) -> Bool { m.url == machine.url && m.token == machine.token && m.name == machine.name }

    var statuses: [String: String] {
        if OperationQueue.current === queue { return watch.status }
        var out: [String: String] = [:]
        queue.addOperations([BlockOperation { out = self.watch.status }], waitUntilFinished: true)
        return out
    }

    func start() {
        DispatchQueue.global(qos: .utility).async { [weak self] in
            guard let self, !self.isStopped else { return }
            let m = self.machine
            // Cleartext only inside a private network (DNS, so off the main thread).
            if !m.url.hasPrefix("https:") && !NetPolicy.cleartextAllowedResolving(m.host) {
                self.finishOnce()
                return
            }
            guard let url = URL(string: LinkParser.wsURL(m.url)) else { return self.finishOnce() }
            var req = URLRequest(url: url, timeoutInterval: 15)
            req.setValue("Bearer " + m.token, forHTTPHeaderField: "Authorization")
            self.queue.addOperation {
                guard !self.stopped else { return }
                let t = self.session.webSocketTask(with: req)
                self.task = t
                t.resume()
                t.send(.string(Wire.hello)) { _ in }
                self.receive(t)
            }
        }
    }

    private var isStopped: Bool {
        var s = false
        queue.addOperations([BlockOperation { s = self.stopped }], waitUntilFinished: true)
        return s
    }

    func stop() {
        queue.addOperation {
            self.stopped = true
            self.task?.cancel(with: .normalClosure, reason: nil)
            self.task = nil
            self.session.invalidateAndCancel()
        }
    }

    /// A one-time look that took too long: done with what it has.
    func giveUp() { finishOnce() }

    private func finishOnce() {
        queue.addOperation {
            guard let f = self.once else { return }
            self.once = nil
            f(self)
        }
    }

    private func receive(_ t: URLSessionWebSocketTask) {
        t.receive { [weak self] result in
            guard let self, !self.stopped, self.task === t else { return }
            switch result {
            case .success(let msg):
                self.backoff = 1
                if case .string(let text) = msg { self.handle(text) }
                self.receive(t)
            case .failure:
                self.retry()
            }
        }
    }

    private func handle(_ text: String) {
        let events = watch.onMessage(text)
        for id in watch.resolved { Notifier.cancel(machineId: machine.id, sessionId: id) }
        let wasList = text.contains("\"t\":\"sessions\"")
        // The app in front, on this machine: the web UI shows it itself.
        if WatchHub.shared.foregroundMachine != machine.id {
            for e in events {
                Notifier.agent(machine: machine, sessionId: e.sessionId, title: e.title, body: e.body, choices: e.choices)
            }
        }
        if wasList, once != nil {
            let f = once
            once = nil
            task?.cancel(with: .normalClosure, reason: nil)
            f?(self)
        }
    }

    private func retry() {
        if once != nil { return finishOnce() }
        let delay = backoff
        backoff = min(backoff * 2, 60)
        DispatchQueue.global().asyncAfter(deadline: .now() + delay) { [weak self] in self?.start() }
    }
}

/// An answer button on a notification: sends that key to its tab, without opening the app.
enum Answer {
    static func send(machine: Machine, sessionId: String, key: String, done: @escaping () -> Void) {
        DispatchQueue.global(qos: .userInitiated).async {
            guard machine.url.hasPrefix("https:") || NetPolicy.cleartextAllowedResolving(machine.host),
                  let url = URL(string: LinkParser.wsURL(machine.url)),
                  let frame = Wire.inputFrame(sessionId: sessionId, data: Wire.keyText(key)) else { return done() }
            var req = URLRequest(url: url, timeoutInterval: 8)
            req.setValue("Bearer " + machine.token, forHTTPHeaderField: "Authorization")
            let session = URLSession(configuration: .ephemeral)
            let t = session.webSocketTask(with: req)
            var finished = false
            let lock = NSLock()
            let finish = {
                lock.lock()
                let first = !finished
                finished = true
                lock.unlock()
                guard first else { return }
                t.cancel(with: .normalClosure, reason: nil)
                session.finishTasksAndInvalidate()
                done()
            }
            t.resume()
            t.send(.string(Wire.hello)) { _ in }
            t.send(.data(frame)) { _ in
                // Let the frame leave before the connection closes.
                DispatchQueue.global().asyncAfter(deadline: .now() + 0.5) { finish() }
            }
            DispatchQueue.global().asyncAfter(deadline: .now() + 8) { finish() }
        }
    }
}

/// Notifications for agents that need you.
enum Notifier {
    static let answerPrefix = "answer:"
    private static let lock = NSLock()
    private static var recent: [String: (title: String, at: Date, answers: Bool)] = [:]

    static func requestPermission(_ done: @escaping (Bool) -> Void) {
        UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge]) { ok, _ in done(ok) }
    }

    private static func key(_ machineId: String, _ sessionId: String) -> String { machineId + "/" + sessionId }

    /// Posts (or replaces) the notification for one tab. The web view and the
    /// app's own connection can both report the same moment; once is enough.
    static func agent(machine: Machine, sessionId: String, title: String, body: String, choices: [AgentEvent.Choice] = []) {
        guard !sessionId.isEmpty else { return }
        let k = key(machine.id, sessionId)
        let now = Date()
        lock.lock()
        if let last = recent[k], last.title == title, now.timeIntervalSince(last.at) < 20, last.answers || choices.isEmpty {
            lock.unlock()
            return
        }
        recent[k] = (title, now, !choices.isEmpty)
        lock.unlock()

        let c = UNMutableNotificationContent()
        c.title = title
        c.subtitle = machine.name
        c.body = body
        c.sound = .default
        c.threadIdentifier = machine.id
        c.userInfo = ["machine": machine.id, "session": sessionId]
        let picks = Array(choices.prefix(3))
        let center = UNUserNotificationCenter.current()
        let post = {
            center.add(UNNotificationRequest(identifier: k, content: c, trigger: nil)) { _ in }
        }
        guard !picks.isEmpty else { return post() }
        // Answer from the notification: up to three of the choices on screen.
        let id = "agent." + picks.map { $0.key + "=" + $0.label }.joined(separator: "|")
        c.categoryIdentifier = id
        center.getNotificationCategories { existing in
            if !existing.contains(where: { $0.identifier == id }) {
                let actions = picks.map { UNNotificationAction(identifier: answerPrefix + $0.key, title: String($0.label.prefix(30)), options: []) }
                var all = existing.filter { $0.identifier.hasPrefix("agent.") }
                if all.count > 30 { all = Set(all.prefix(20)) }
                all.insert(UNNotificationCategory(identifier: id, actions: actions, intentIdentifiers: [], options: []))
                center.setNotificationCategories(all)
            }
            post()
        }
    }

    static func cancel(machineId: String, sessionId: String) {
        let k = key(machineId, sessionId)
        lock.lock(); recent.removeValue(forKey: k); lock.unlock()
        let center = UNUserNotificationCenter.current()
        center.removeDeliveredNotifications(withIdentifiers: [k])
        center.removePendingNotificationRequests(withIdentifiers: [k])
    }
}

/// What iOS allows an app in the background: a little time right after it
/// leaves the screen, and a refresh now and then, when iOS decides.
enum Background {
    static let refreshId = "io.github.geckoterm.ios.refresh"
    private static var task: UIBackgroundTaskIdentifier = .invalid

    static func register() {
        BGTaskScheduler.shared.register(forTaskWithIdentifier: refreshId, using: nil) { t in
            guard let t = t as? BGAppRefreshTask else { return }
            scheduleRefresh()
            let machines = AppModel.makeStore().list()
            t.expirationHandler = { t.setTaskCompleted(success: false) }
            WatchHub.checkOnce(machines) { t.setTaskCompleted(success: true) }
        }
    }

    static func scheduleRefresh() {
        let r = BGAppRefreshTaskRequest(identifier: refreshId)
        r.earliestBeginDate = Date(timeIntervalSinceNow: 15 * 60)
        try? BGTaskScheduler.shared.submit(r)
    }

    /// Keeps the app's connections up for as long as iOS gives it.
    @MainActor static func keepAlive() {
        guard task == .invalid else { return }
        task = UIApplication.shared.beginBackgroundTask(withName: "Watch agents") {
            saveStatuses()
            WatchHub.shared.stopAll()
            endKeepAlive()
        }
    }

    @MainActor static func endKeepAlive() {
        guard task != .invalid else { return }
        UIApplication.shared.endBackgroundTask(task)
        task = .invalid
    }

    static func saveStatuses() {
        for (id, s) in WatchHub.shared.statuses() { save(statuses: s, for: id) }
    }

    static func save(statuses: [String: String], for machineId: String) {
        UserDefaults.standard.set(statuses, forKey: "agents." + machineId)
    }

    static func statuses(for machineId: String) -> [String: String] {
        UserDefaults.standard.dictionary(forKey: "agents." + machineId) as? [String: String] ?? [:]
    }
}
