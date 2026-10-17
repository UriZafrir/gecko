import XCTest

/// What the UI tests share: launching against the Gecko the CI job runs
/// (GECKO_LINK), reading the page's state from the app's probe, screenshots
/// for the report (GECKO_SHOTS, a folder on the Mac), and pacing for video.
class GeckoUITestCase: XCTestCase {
    var app: XCUIApplication!
    let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")

    var env: [String: String] { ProcessInfo.processInfo.environment }

    /// The link to the CI job's Gecko (gecko url), passed as TEST_RUNNER_GECKO_LINK.
    func link(_ name: String = "GECKO_LINK") throws -> String {
        guard let l = env[name], !l.isEmpty else { throw XCTSkip("no \(name): run against a Gecko daemon (see .github/workflows/ios.yml)") }
        return l
    }

    override func setUp() {
        continueAfterFailure = false
    }

    /// Starts the app with no saved machines; with open, that machine is added and opened at once.
    func launch(open: String? = nil, name: String? = nil) {
        app = XCUIApplication()
        app.launchEnvironment["GECKO_UITEST"] = "1"
        app.launchEnvironment["GECKO_UITEST_RESET"] = "1"
        if let open { app.launchEnvironment["GECKO_UITEST_OPEN"] = open }
        if let name { app.launchEnvironment["GECKO_UITEST_NAME"] = name }
        app.launch()
    }

    // MARK: the page

    var web: XCUIElement { app.webViews.firstMatch }

    /// The page's state (TerminalViewController's probe): online, sync, bundled, daemon, tab, tabs, font, viewportY, baseY, screen...
    func probe() -> [String: Any] {
        let e = app.staticTexts["gecko.probe"]
        guard e.exists, let d = e.label.data(using: .utf8) else { return [:] }
        return (try? JSONSerialization.jsonObject(with: d)) as? [String: Any] ?? [:]
    }

    @discardableResult
    func waitProbe(_ what: String, timeout: TimeInterval = 30, _ ok: ([String: Any]) -> Bool) -> [String: Any] {
        let until = Date().addingTimeInterval(timeout)
        var last: [String: Any] = [:]
        while Date() < until {
            last = probe()
            if ok(last) { return last }
            Thread.sleep(forTimeInterval: 0.3)
        }
        XCTFail("timed out waiting for \(what); page: \(last)")
        return last
    }

    func waitOnline(timeout: TimeInterval = 40) -> [String: Any] {
        waitProbe("the page to connect and sync", timeout: timeout) { p in
            (p["online"] as? String) == "online" && (p["sync"] as? Bool) == true
        }
    }

    func screen() -> String { probe()["screen"] as? String ?? "" }

    func int(_ key: String) -> Int { probe()[key] as? Int ?? -1 }

    // MARK: the system

    /// iOS asks once whether Gecko may notify.
    func allowNotificationsIfAsked(timeout: TimeInterval = 4) {
        let allow = springboard.alerts.buttons["Allow"]
        if allow.waitForExistence(timeout: timeout) { allow.tap() }
    }

    /// A pause so the video shows what just happened.
    func beat(_ seconds: Double = 1.2) {
        Thread.sleep(forTimeInterval: seconds)
    }

    func shot(_ name0: String) {
        let name = (env["GECKO_SHOT_PREFIX"] ?? "") + name0
        let s = XCUIScreen.main.screenshot()
        let a = XCTAttachment(screenshot: s)
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        if let dir = env["GECKO_SHOTS"], !dir.isEmpty {
            try? FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
            try? s.pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name + ".png"))
        }
    }

    /// The middle of the terminal (in the web view, above the extra keys).
    func terminalPoint(_ dx: CGFloat = 0.5, _ dy: CGFloat = 0.55) -> XCUICoordinate {
        web.coordinate(withNormalizedOffset: CGVector(dx: dx, dy: dy))
    }

    /// A finger drag on the screen, from one spot to another.
    func drag(from a: XCUICoordinate, to b: XCUICoordinate, hold: TimeInterval = 0.05, velocity: XCUIGestureVelocity = .default) {
        a.press(forDuration: hold, thenDragTo: b, withVelocity: velocity, thenHoldForDuration: 0)
    }

    var isPad: Bool { UIDevice.current.userInterfaceIdiom == .pad }

    func key(_ name: String) -> XCUIElement { app.descendants(matching: .any)["key." + name] }
}
