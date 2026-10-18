import UIKit
import XCTest

/// What the UI tests share: launching against the Gecko the CI job runs
/// (GECKO_LINK), reading the page's state from the app's probe, screenshots
/// for the report (GECKO_SHOTS, a folder on the Mac), and pacing for video.
class GeckoUITestCase: XCTestCase {
    var app: XCUIApplication!
    private var jsCount = 0
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
    func launch(open: String? = nil, name: String? = nil, reset: Bool = true) {
        app = XCUIApplication()
        app.launchEnvironment["GECKO_UITEST"] = "1"
        if reset { app.launchEnvironment["GECKO_UITEST_RESET"] = "1" }
        if let open { app.launchEnvironment["GECKO_UITEST_OPEN"] = open }
        if let name { app.launchEnvironment["GECKO_UITEST_NAME"] = name }
        if let dir = env["GECKO_CUES"], !dir.isEmpty {
            let js = (dir as NSString).appendingPathComponent("js")
            try? FileManager.default.createDirectory(atPath: js, withIntermediateDirectories: true)
            app.launchEnvironment["GECKO_UITEST_CMDS"] = js
        }
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

    /// Like waitProbe, without failing: whether it came within the time.
    func waitFor(_ timeout: TimeInterval, _ ok: ([String: Any]) -> Bool) -> Bool {
        let until = Date().addingTimeInterval(timeout)
        while Date() < until {
            if ok(probe()) { return true }
            Thread.sleep(forTimeInterval: 0.3)
        }
        return false
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

    /// A tap on the terminal for the keyboard, and a moment for it to come
    /// up (letters typed before it is up can be lost).
    func tapTerminal() {
        terminalPoint().tap()
        _ = app.keyboards.firstMatch.waitForExistence(timeout: 5)
        beat(0.5)
    }

    /// A finger drag on the screen, from one spot to another.
    func drag(from a: XCUICoordinate, to b: XCUICoordinate, hold: TimeInterval = 0.05, velocity: XCUIGestureVelocity = .default) {
        a.press(forDuration: hold, thenDragTo: b, withVelocity: velocity, thenHoldForDuration: 0)
    }

    var isPad: Bool { UIDevice.current.userInterfaceIdiom == .pad }

    func key(_ name: String) -> XCUIElement { app.descendants(matching: .any)["key." + name] }

    // MARK: the feature tour

    /// Runs a script in the page (AppModel.uiTestURL), for steps a
    /// synthesized finger can't do reliably, such as a button deep in the page.
    func js(_ script: String) {
        // A file the app picks up (it reads the folder twice a second)...
        if let dir = app.launchEnvironment["GECKO_UITEST_CMDS"] {
            jsCount += 1
            let path = (dir as NSString).appendingPathComponent(String(format: "%05d.js", jsCount))
            try? script.write(toFile: path, atomically: true, encoding: .utf8)
            let until = Date().addingTimeInterval(3)
            while Date() < until {
                if !FileManager.default.fileExists(atPath: path) { return }
                Thread.sleep(forTimeInterval: 0.1)
            }
            try? FileManager.default.removeItem(atPath: path)
        }
        // ...or else a link.
        var c = URLComponents()
        c.scheme = "gecko-uitest"
        c.host = "js"
        c.queryItems = [URLQueryItem(name: "c", value: script)]
        guard #available(iOS 16.4, *) else { return XCTFail("the tour needs iOS 16.4 or newer") }
        // Through the system, as a tapped link would: the app as it is
        // (XCUIApplication.open launches it again, from scratch).
        XCUIDevice.shared.system.open(c.url!)
        let open = springboard.buttons["Open"]
        if open.waitForExistence(timeout: 1.5) { open.tap() }
        if app.state != .runningForeground { app.activate() }
    }

    /// The video's caption from now on: the time and text, for the job's
    /// captions (GECKO_CHAPTERS, a file on the Mac).
    func chapter(_ caption: String) {
        XCTContext.runActivity(named: "chapter: " + caption) { _ in }
        guard let path = env["GECKO_CHAPTERS"], !path.isEmpty else { return }
        let line = String(format: "%.2f\t%@\n", Date().timeIntervalSince1970, caption)
        if let h = FileHandle(forWritingAtPath: path) {
            h.seekToEndOfFile()
            h.write(line.data(using: .utf8)!)
            try? h.close()
        } else {
            try? line.write(toFile: path, atomically: true, encoding: .utf8)
        }
    }

    /// Asks the job to do something on the Mac (ios/scripts/ci-cues.sh: a
    /// file in GECKO_CUES), and waits until it is done. False without a job.
    @discardableResult
    func cue(_ name: String, timeout: TimeInterval = 60) -> Bool {
        guard let dir = env["GECKO_CUES"], !dir.isEmpty else { return false }
        let url = URL(fileURLWithPath: dir)
        try? FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
        try? "".write(to: url.appendingPathComponent(name), atomically: true, encoding: .utf8)
        let done = url.appendingPathComponent(name + ".done").path
        let until = Date().addingTimeInterval(timeout)
        while Date() < until {
            if FileManager.default.fileExists(atPath: done) { return true }
            Thread.sleep(forTimeInterval: 0.3)
        }
        XCTFail("the job didn't do \(name)")
        return false
    }

    /// The window is in landscape and the page fills it (not portrait with
    /// the picture squeezed), before a landscape screenshot.
    func assertLandscape(_ p: [String: Any], file: StaticString = #filePath, line: UInt = #line) {
        let w = app.windows.firstMatch.frame
        XCTAssertGreaterThan(w.width, w.height, "the window is landscape: \(w)", file: file, line: line)
        let pw = CGFloat(p["width"] as? Int ?? 0), ph = CGFloat(p["height"] as? Int ?? 0)
        XCTAssertGreaterThan(pw, ph, "the page is landscape: \(pw)x\(ph)", file: file, line: line)
        XCTAssertGreaterThanOrEqual(pw, w.width * 0.85, "the page fills the window's width", file: file, line: line)
    }
}
