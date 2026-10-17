import XCTest

/// The same walk on every simulator of the CI matrix (iPhone SE, 16,
/// 16 Pro Max, iPads): the machine list, the terminal, the keyboard with
/// the extra keys, landscape and back with the session kept, and on iPad
/// the sidebar beside the terminal and Split View. The job runs it again
/// in dark mode with the largest text size (GECKO_SHOT_PREFIX "dark-").
final class DevicesTour: GeckoUITestCase {
    override func tearDown() {
        XCUIDevice.shared.orientation = .portrait
        super.tearDown()
    }

    func testTour() throws {
        let link = try link()
        XCUIDevice.shared.orientation = .portrait
        launch()
        XCTAssertTrue(app.buttons["connect.scan"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["connect.build-id"].isHittable, "the build id fits on screen")
        beat()
        shot("01-machines")
        app.terminate()

        launch(open: link, name: "studio-mac")
        allowNotificationsIfAsked()
        var p = waitOnline()
        beat(1.5)
        shot("02-terminal")
        // The page fills the space between the safe areas, above the keys.
        let w = web.frame, k = key("esc").frame, win = app.windows.firstMatch.frame
        XCTAssertGreaterThan(w.height, win.height * 0.5, "the terminal has most of the screen")
        XCTAssertLessThanOrEqual(w.maxY, k.minY + 1, "the extra keys are below the page")
        XCTAssertLessThan(k.maxY, win.maxY, "the keys clear the home indicator")
        if isPad {
            XCTAssertEqual(p["sideFixed"] as? Bool, false, "on iPad the sidebar stays beside the terminal")
        }

        // The keyboard, with the extra keys right on top of it.
        tapTerminal()
        beat()
        app.typeText("ls -l /usr/bin | head -60; echo tour-$((40+2))\n")
        let ran: ([String: Any]) -> Bool = { ($0["screen"] as? String ?? "").contains("tour-42") }
        if !waitFor(10, ran) {
            // Seen once on a just-booted Simulator: the line typed, its Return lost.
            XCTContext.runActivity(named: "the Return was lost; pressing it again") { _ in app.typeText("\n") }
        }
        p = waitProbe("the command ran", timeout: 15, ran)
        let id = p["active"] as? String ?? ""
        beat()
        let kb = app.keyboards.firstMatch
        if kb.exists {
            XCTAssertLessThanOrEqual(key("esc").frame.maxY, kb.frame.minY + 1, "the keys ride on the keyboard")
            XCTAssertLessThanOrEqual(web.frame.maxY, kb.frame.minY + 1, "the keyboard doesn't cover the terminal")
        }
        shot("03-keyboard")

        // Landscape: the session and the tab are kept.
        XCUIDevice.shared.orientation = .landscapeLeft
        beat(2)
        p = waitProbe("the same tab after rotating", timeout: 10) { ($0["active"] as? String) == id && ($0["online"] as? String) == "online" }
        assertLandscape(p)
        shot("04-landscape-keyboard")
        key("hide-keyboard").tap()
        beat(1.5)
        p = waitProbe("the page in landscape", timeout: 8) { ($0["width"] as? Int ?? 0) > ($0["height"] as? Int ?? 0) }
        assertLandscape(p)
        shot("05-landscape")
        if isPad || ((p["width"] as? Int ?? 0) >= 720 && (p["height"] as? Int ?? 0) > 500) {
            XCTAssertEqual(p["sideFixed"] as? Bool, false, "wide: the sidebar beside the terminal")
        }
        XCUIDevice.shared.orientation = .portrait
        beat(2)
        p = waitProbe("still the same tab", timeout: 10) { ($0["active"] as? String) == id }
        XCTAssertTrue((p["screen"] as? String ?? "").contains("tour-42") || (p["baseY"] as? Int ?? 0) > 0, "the output is still there")
        shot("06-portrait-again")

        if !isPad {
            // Phones: the drawer from the left edge.
            drag(from: web.coordinate(withNormalizedOffset: CGVector(dx: 0, dy: 0.5)).withOffset(CGVector(dx: 2, dy: 0)), to: terminalPoint(0.8, 0.5), hold: 0.02)
            waitProbe("the drawer", timeout: 5) { ($0["drawer"] as? Bool) == true }
            beat()
            shot("07-drawer")
            drag(from: terminalPoint(0.7, 0.5), to: terminalPoint(0.05, 0.5), hold: 0.02)
            beat()
        }
    }

    /// iPad: Split View from the window's multitasking menu, best effort
    /// (the menu differs between iPadOS versions); the page refits to the
    /// narrower window. Last in the job: iPadOS keeps the pair afterwards.
    func testSplitView() throws {
        guard isPad else { throw XCTSkip("iPad only") }
        XCUIDevice.shared.orientation = .landscapeLeft
        launch(open: try link(), name: "studio-mac")
        allowNotificationsIfAsked()
        let p0 = waitProbe("the page in landscape", timeout: 40) { ($0["online"] as? String) == "online" && ($0["width"] as? Int ?? 0) > ($0["height"] as? Int ?? 0) }
        assertLandscape(p0)
        beat()
        shot("08-split-view-before")
        XCTContext.runActivity(named: "Split View") { _ in
            let dots = app.windows.firstMatch.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0)).withOffset(CGVector(dx: 0, dy: 12))
            dots.tap()
            let split = springboard.buttons.matching(NSPredicate(format: "label CONTAINS[c] 'Split View' OR identifier CONTAINS[c] 'split'")).firstMatch
            let menu = app.buttons.matching(NSPredicate(format: "label CONTAINS[c] 'Split View'")).firstMatch
            let button = split.waitForExistence(timeout: 3) ? split : menu
            guard button.waitForExistence(timeout: 2) else {
                XCTContext.runActivity(named: "no multitasking menu found; skipped") { _ in }
                return
            }
            button.tap()
            beat(2)
            // Pick another app for the other side (Settings, on the home screen).
            let other = springboard.icons["Settings"]
            if other.waitForExistence(timeout: 4) && other.isHittable { other.tap() }
            beat(2.5)
            let p = probe()
            let screen = springboard.frame
            XCTAssertGreaterThan(screen.width, screen.height, "the iPad is in landscape: \(screen)")
            shot("09-split-view")
            XCTContext.runActivity(named: "split view width \(p["width"] ?? "?"), sidebar fixed \(p["sideFixed"] ?? "?")") { _ in }
            if let w = p["width"] as? Int, w < 720 {
                XCTAssertEqual(p["sideFixed"] as? Bool, true, "a narrow window: the sidebar becomes a drawer")
            }
        }
    }
}
