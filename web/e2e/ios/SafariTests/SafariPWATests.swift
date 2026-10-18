import XCTest

/// Gecko's web UI in Safari and as a Home Screen web app, on the Simulator:
/// tapping tabs, pinching, the theme and settings, the keyboard. The page
/// reports its state with ?probe (main.js) as text this test reads from the
/// accessibility tree. Screenshots go to GECKO_SHOTS (a folder on the Mac),
/// findings to GECKO_SHOTS/results.json.
///
/// Environment (TEST_RUNNER_ prefixed in the job): GECKO_URL, the page with
/// its token; GECKO_SHOTS; GECKO_HOME_DIR, the daemon's home, whose
/// settings.json shows the font size and theme the page saved.
final class SafariPWATests: XCTestCase {
    let safari = XCUIApplication(bundleIdentifier: "com.apple.mobilesafari")
    let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
    var env: [String: String] { ProcessInfo.processInfo.environment }
    var results: [String: Any] = [:]

    override func setUp() {
        continueAfterFailure = true
    }

    override func tearDown() {
        if let dir = env["GECKO_SHOTS"], !dir.isEmpty,
           let d = try? JSONSerialization.data(withJSONObject: results, options: [.prettyPrinted, .sortedKeys]) {
            try? d.write(to: URL(fileURLWithPath: dir).appendingPathComponent("results-\(name.split(separator: " ").last!.dropLast()).json"))
        }
    }

    // MARK: helpers

    func shot(_ name: String) {
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

    func beat(_ s: Double = 1) { Thread.sleep(forTimeInterval: s) }

    /// The page's state (main.js, ?probe), from whichever app shows it.
    func probe(_ app: XCUIApplication) -> [String: Any] {
        let e = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH 'gecko.probe '")).firstMatch
        guard e.exists else { return [:] }
        let json = String(e.label.dropFirst("gecko.probe ".count))
        return (try? JSONSerialization.jsonObject(with: Data(json.utf8))) as? [String: Any] ?? [:]
    }

    @discardableResult
    func waitProbe(_ app: XCUIApplication, _ timeout: TimeInterval = 30, _ ok: ([String: Any]) -> Bool) -> [String: Any] {
        let until = Date().addingTimeInterval(timeout)
        var last: [String: Any] = [:]
        while Date() < until {
            last = probe(app)
            if ok(last) { return last }
            beat(0.3)
        }
        return last
    }

    func settings() -> [String: Any] {
        guard let home = env["GECKO_HOME_DIR"], let d = FileManager.default.contents(atPath: home + "/settings.json") else { return [:] }
        return (try? JSONSerialization.jsonObject(with: d)) as? [String: Any] ?? [:]
    }

    func nums(_ v: Any?) -> [Double] { (v as? [Any] ?? []).compactMap { ($0 as? NSNumber)?.doubleValue } }

    /// Where CSS pixel (x, y) of the page is on the screen: found from an
    /// element whose place both the page and the accessibility tree know.
    func screenPoint(_ app: XCUIApplication, css: (Double, Double), anchor: XCUIElement, anchorCSS: [Double]) -> XCUICoordinate {
        let f = anchor.frame
        let dx = Double(f.minX) - anchorCSS[0], dy = Double(f.minY) - anchorCSS[1]
        return app.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: css.0 + dx, dy: css.1 + dy))
    }

    func openInSafari() throws -> [String: Any] {
        guard let url = env["GECKO_URL"], !url.isEmpty, let u = URL(string: url + (url.contains("?") ? "&" : "?") + "probe=1") else {
            throw XCTSkip("no GECKO_URL")
        }
        XCUIDevice.shared.system.open(u)
        XCTAssertTrue(safari.wait(for: .runningForeground, timeout: 20), "Safari opened")
        for b in ["Continue", "Not Now", "Close"] where safari.buttons[b].waitForExistence(timeout: 1) { safari.buttons[b].tap() }
        let ready: ([String: Any]) -> Bool = { ($0["online"] as? String) == "online" && ($0["sync"] as? Bool) == true }
        var p = waitProbe(safari, 15, ready)
        if p.isEmpty {
            // A cold Safari can open on its start page without loading the
            // link (seen on the iPhone SE): type it in the address bar.
            let field = safari.textFields.firstMatch
            if field.waitForExistence(timeout: 5) {
                field.tap()
                beat(0.5)
                safari.typeText(u.absoluteString + "\n")
            }
            p = waitProbe(safari, 40, ready)
        }
        results["probe"] = !p.isEmpty
        return p
    }

    // MARK: Safari

    func testSafari() throws {
        var p = try openInSafari()
        beat(1)
        shot("safari-01-start")
        let newTab = safari.buttons["New tab"].firstMatch
        XCTAssertTrue(newTab.waitForExistence(timeout: 10), "the + button")
        for _ in 0..<4 {
            newTab.tap()
            beat(1.2)
        }
        p = waitProbe(safari, 10) { ($0["tabs"] as? [Any])?.count ?? 0 >= 4 }
        shot("safari-02-tabs")
        results["tabsMade"] = (p["tabs"] as? [Any])?.count ?? -1

        // Tapping tabs, many times: each tap switches to the tab tapped.
        var misses = 0, taps = 0
        if let tabs = p["tabs"] as? [[String: Any]], tabs.count >= 2, let nb = p["newTab"] as? [Any] {
            let anchorCSS = nums(nb)
            for i in 0..<24 {
                let now = probe(safari)
                guard let list = now["tabs"] as? [[String: Any]], list.count >= 2 else { break }
                let k = (i * 3 + 1) % list.count
                let t = list[k]
                let r = nums(t["r"])
                guard r.count == 4, let id = t["id"] as? String else { continue }
                if (now["active"] as? String) == id { continue }
                // Only tabs fully in view (the strip may scroll).
                let vw = nums(now["inner"]).first ?? 0
                if r[0] < 0 || r[0] + r[2] > vw - 40 { continue }
                screenPoint(safari, css: (r[0] + min(r[2] / 2, 30), r[1] + r[3] / 2), anchor: newTab, anchorCSS: anchorCSS).tap()
                taps += 1
                let after = waitProbe(safari, 2) { ($0["active"] as? String) == id }
                if (after["active"] as? String) != id { misses += 1 }
            }
        }
        results["tabTaps"] = taps
        results["tabMisses"] = misses
        XCTAssertGreaterThan(taps, 5, "tapped tabs")
        XCTAssertEqual(misses, 0, "every tap on a tab switched to it")
        shot("safari-03-after-taps")

        // Pinch on the terminal: the text size changes, the page doesn't zoom.
        let font0 = settings()["mobileFontSize"] as? Int ?? (probe(safari)["font"] as? Int ?? 0)
        let web = safari.webViews.firstMatch
        web.pinch(withScale: 1.8, velocity: 1.5)
        beat(1.5)
        p = probe(safari)
        let font1 = settings()["mobileFontSize"] as? Int ?? (p["font"] as? Int ?? 0)
        let scale1 = nums(p["vv"]).last ?? -1
        shot("safari-04-pinch-out")
        results["pinch"] = ["fontBefore": font0, "fontAfter": font1, "pageScale": scale1]
        XCTAssertGreaterThan(font1, font0, "pinching out made the text bigger")
        XCTAssertEqual(scale1, 1, accuracy: 0.01, "the page did not zoom")
        web.pinch(withScale: 0.6, velocity: -1)
        beat(1.5)
        p = probe(safari)
        results["pinchIn"] = ["font": settings()["mobileFontSize"] ?? p["font"] ?? -1, "pageScale": nums(p["vv"]).last ?? -1]
        shot("safari-05-pinch-in")

        // Pinch on the tab bar: nothing zooms there either.
        let strip = safari.webViews.firstMatch.descendants(matching: .any).matching(NSPredicate(format: "label == 'Tabs'")).firstMatch
        if strip.exists, strip.isHittable, strip.frame.height < 120 {
            strip.pinch(withScale: 2.0, velocity: 2)
        } else {
            results["pinchTabBarSkipped"] = "no tab strip element"
        }
        beat(1.5)
        p = probe(safari)
        let scale2 = nums(p["vv"]).last ?? -1
        results["pinchTabBar"] = ["pageScale": scale2]
        shot("safari-06-pinch-tabbar")
        XCTAssertEqual(scale2, 1, accuracy: 0.01, "pinching the tab bar did not zoom the page")
        // A double tap on a tab doesn't zoom.
        if let t = (p["tabs"] as? [[String: Any]])?.first, let nb = p["newTab"] as? [Any] {
            let r = nums(t["r"])
            if r.count == 4 { screenPoint(safari, css: (r[0] + 20, r[1] + r[3] / 2), anchor: newTab, anchorCSS: nums(nb)).doubleTap() }
            beat(1.5)
            p = probe(safari)
            results["doubleTapScale"] = nums(p["vv"]).last ?? -1
            if safari.buttons["Cancel"].exists { safari.buttons["Cancel"].tap() }
        }
        // Whatever happened, a page stuck zoomed in shows here.
        shot("safari-07-after-gestures")

        // Settings and themes: a button on the tab bar.
        let gear = safari.buttons["Settings"].firstMatch
        results["settingsButton"] = gear.waitForExistence(timeout: 3)
        if gear.exists {
            gear.tap()
            beat(1)
            shot("safari-08-settings")
            let theme = safari.buttons.matching(NSPredicate(format: "label CONTAINS 'Gecko Light'")).firstMatch
            if theme.waitForExistence(timeout: 3) {
                theme.tap()
                beat(1.2)
                shot("safari-09-theme-light")
                results["themeAfterTap"] = settings()["theme"] ?? probe(safari)["theme"] ?? ""
                XCTAssertEqual(results["themeAfterTap"] as? String, "gecko-light", "the theme changed")
                safari.buttons.matching(NSPredicate(format: "label CONTAINS 'Tokyo Night'")).firstMatch.tap()
                beat(1)
            }
            let bigger = safari.buttons["Bigger text"].firstMatch
            if bigger.exists {
                let f0 = settings()["mobileFontSize"] as? Int ?? 0
                bigger.tap()
                beat(1)
                results["biggerText"] = [f0, settings()["mobileFontSize"] as? Int ?? 0]
            }
            let done = safari.buttons["Done"].firstMatch
            if done.exists { done.tap() }
            beat(1)
        }

        // The keyboard: the key bar sits on it, the tab bar stays in view,
        // and the page comes back whole when it goes.
        let before = probe(safari)
        web.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5)).tap()
        let kb = safari.keyboards.firstMatch.waitForExistence(timeout: 6)
        beat(1.5)
        p = probe(safari)
        shot("safari-10-keyboard")
        let vv = nums(p["vv"]), keybar = nums(p["keybar"]), tabbar = nums(p["tabbar"])
        results["keyboard"] = ["shown": kb, "vv": vv, "keybar": keybar, "tabbar": tabbar, "inner": nums(p["inner"])]
        if kb, vv.count == 4, keybar.count == 4, tabbar.count == 4 {
            // vv: width, height, offsetTop, scale
            XCTAssertLessThanOrEqual(keybar[1] + keybar[3], vv[2] + vv[1] + 2, "the key bar is above the keyboard")
            XCTAssertGreaterThanOrEqual(tabbar[1] + 1, vv[2], "the tab bar is in view")
        }
        // Hide it (Safari's Done above the keyboard), then check nothing stays shifted.
        if safari.buttons["Done"].exists { safari.buttons["Done"].tap() } else { safari.buttons["Esc"].firstMatch.tap() }
        beat(1.5)
        p = probe(safari)
        shot("safari-11-keyboard-closed")
        results["afterKeyboard"] = ["vv": nums(p["vv"]), "tabbar": nums(p["tabbar"]), "keybar": nums(p["keybar"]), "before": ["vv": nums(before["vv"]), "tabbar": nums(before["tabbar"]), "keybar": nums(before["keybar"])]]
        let t2 = nums(p["tabbar"]), t1 = nums(before["tabbar"])
        if t1.count == 4, t2.count == 4 { XCTAssertEqual(t2[1], t1[1], accuracy: 2, "the tab bar is back where it was") }
    }

    // MARK: the Home Screen web app

    func testHomeScreenApp() throws {
        _ = try openInSafari()
        beat(1)
        // Share, then Add to Home Screen, then Add.
        // The page may have minimized Safari's toolbar: a tap on the address brings it back.
        func shareButton() -> XCUIElement? {
            let candidates: [XCUIElement] = [safari.buttons["ShareButton"].firstMatch, safari.buttons["Share"].firstMatch,
                                             safari.buttons.matching(NSPredicate(format: "label == 'Share'")).firstMatch]
            for e in candidates where e.exists && e.isHittable { return e }
            return nil
        }
        var found = shareButton()
        for _ in 0..<3 where found == nil {
            let addr = safari.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS '127.0.0.1' OR identifier == 'TabBarItemTitle' OR identifier == 'URL'")).firstMatch
            if addr.exists { addr.tap() } else { safari.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.97)).tap() }
            beat(1.2)
            found = shareButton()
            if found == nil, safari.buttons["Cancel"].exists { safari.buttons["Cancel"].tap(); beat(0.5) }
        }
        XCTAssertNotNil(found, "Safari's Share button")
        guard let share = found else { shot("home-00-no-share"); print(safari.debugDescription); return }
        share.tap()
        beat(1.5)
        shot("home-01-share-sheet")
        var add = safari.buttons["Add to Home Screen"].firstMatch
        if !add.exists { add = safari.cells["Add to Home Screen"].firstMatch }
        if !add.exists { add = safari.staticTexts["Add to Home Screen"].firstMatch }
        var tries = 0
        while !add.isHittable && tries < 4 {
            safari.swipeUp()
            tries += 1
            beat(0.6)
        }
        XCTAssertTrue(add.exists, "Add to Home Screen in the share sheet")
        add.tap()
        beat(1.5)
        shot("home-02-add")
        let addButton = safari.buttons["Add"].firstMatch
        results["addSheetName"] = safari.textFields.firstMatch.exists ? (safari.textFields.firstMatch.value as? String ?? "") : ""
        XCTAssertTrue(addButton.waitForExistence(timeout: 5), "the Add button")
        addButton.tap()
        beat(2)
        XCUIDevice.shared.press(.home)
        beat(1.5)
        shot("home-03-springboard")
        let icon = springboard.icons["Gecko"].firstMatch
        XCTAssertTrue(icon.waitForExistence(timeout: 10), "the Gecko icon on the Home Screen")
        // A new icon can land on another Home Screen page.
        for _ in 0..<4 where !icon.isHittable {
            springboard.swipeLeft()
            beat(1)
        }
        results["iconHittable"] = icon.isHittable
        shot("home-03b-icon")
        icon.tap()
        beat(4)
        shot("home-04-standalone")
        // The web app runs as its own app; find its page through the probe.
        var app: XCUIApplication? = nil
        for id in ["com.apple.webapp", "com.apple.WebSheet", "com.apple.mobilesafari"] {
            let a = XCUIApplication(bundleIdentifier: id)
            if a.state == .runningForeground, !probe(a).isEmpty { app = a; results["standaloneApp"] = id; break }
        }
        guard let app else {
            results["standaloneApp"] = "not found"
            return
        }
        let p = waitProbe(app, 30) { ($0["online"] as? String) == "online" }
        results["standalone"] = ["standalone": p["standalone"] ?? "", "inner": nums(p["inner"]), "tabbar": nums(p["tabbar"]), "keybar": nums(p["keybar"]), "safe": p["safe"] ?? ""]
        XCTAssertEqual(p["standalone"] as? Bool, true, "display-mode standalone")
        let newTab = app.buttons["New tab"].firstMatch
        if newTab.waitForExistence(timeout: 8) {
            newTab.tap()
            beat(1.5)
            newTab.tap()
            beat(1.5)
        }
        shot("home-05-standalone-tabs")
        let gear = app.buttons["Settings"].firstMatch
        if gear.waitForExistence(timeout: 3) {
            gear.tap()
            beat(1)
            shot("home-06-standalone-settings")
            app.buttons["Done"].firstMatch.tap()
            beat(1)
        }
        app.webViews.firstMatch.pinch(withScale: 1.8, velocity: 1.5)
        beat(1.5)
        let q = probe(app)
        results["standalonePinch"] = ["font": q["font"] ?? -1, "pageScale": nums(q["vv"]).last ?? -1]
        shot("home-07-standalone-pinch")
        app.webViews.firstMatch.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5)).tap()
        _ = app.keyboards.firstMatch.waitForExistence(timeout: 6)
        beat(1.5)
        let k = probe(app)
        results["standaloneKeyboard"] = ["vv": nums(k["vv"]), "keybar": nums(k["keybar"]), "tabbar": nums(k["tabbar"])]
        shot("home-08-standalone-keyboard")
    }
}
