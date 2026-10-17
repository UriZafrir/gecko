import XCTest

/// 02-terminal: a shell from the phone: typing, the extra keys, scrolling
/// and a flick, pinch for the font size, a swipe to the next tab, the
/// sideways cursor swipe and the drawer.
final class TerminalDemo: GeckoUITestCase {
    func testShellKeysScrollPinchTabs() throws {
        launch(open: try link(), name: "studio-mac")
        allowNotificationsIfAsked()
        var p = waitOnline()
        let tabs = p["tabs"] as? Int ?? 0
        beat()

        // Tap for the keyboard; the extra keys ride on top of it.
        terminalPoint().tap()
        beat()
        app.typeText("ls -l /usr/bin\n")
        waitProbe("ls output", timeout: 15) { ($0["baseY"] as? Int ?? 0) > 40 }
        beat()
        shot("02-keyboard-and-keys")

        // Arrows move in the line, ctrl is sticky: ctrl then c is Ctrl+C.
        app.typeText("echo gecko on iOS")
        for _ in 0..<4 { key("left").tap() }
        beat(0.6)
        for _ in 0..<4 { key("right").tap() }
        app.typeText("\n")
        waitProbe("echo", timeout: 10) { ($0["screen"] as? String ?? "").contains("\ngecko on iOS") }
        key("up").tap()
        beat(0.8)
        key("ctrl").tap()
        waitProbe("ctrl on", timeout: 5) { ($0["ctrl"] as? Bool) == true }
        shot("02-ctrl-sticky")
        app.typeText("c")
        waitProbe("ctrl off again", timeout: 5) { ($0["ctrl"] as? Bool) == false }
        beat()
        key("hide-keyboard").tap()
        beat()

        // Scroll back by dragging down; a flick up coasts back to the end.
        let bottom = int("baseY")
        drag(from: terminalPoint(0.5, 0.3), to: terminalPoint(0.5, 0.75), hold: 0.05, velocity: .slow)
        p = waitProbe("scrolled back", timeout: 5) { ($0["viewportY"] as? Int ?? bottom) < bottom }
        beat()
        drag(from: terminalPoint(0.5, 0.25), to: terminalPoint(0.5, 0.8), hold: 0.02, velocity: .fast)
        beat(1.5)
        shot("02-scrolled-back")
        let back = int("viewportY")
        XCTAssertLessThan(back, bottom)
        drag(from: terminalPoint(0.5, 0.8), to: terminalPoint(0.5, 0.2), hold: 0.02, velocity: .fast)
        beat(2)
        XCTAssertGreaterThan(int("viewportY"), back, "a flick goes back down")
        web.swipeUp(velocity: .fast)
        web.swipeUp(velocity: .fast)
        beat(1.5)

        // Pinch: a bigger font, then back.
        let font = int("font")
        web.pinch(withScale: 1.7, velocity: 1.2)
        p = waitProbe("a bigger font", timeout: 5) { ($0["font"] as? Int ?? font) > font }
        beat()
        shot("02-pinched")
        web.pinch(withScale: 0.6, velocity: -1.2)
        waitProbe("the font back", timeout: 5) { ($0["font"] as? Int ?? 99) <= font }
        beat()

        // One finger sideways moves the cursor.
        terminalPoint().tap()
        app.typeText("echo sideways")
        let x0 = int("cursorX")
        drag(from: terminalPoint(0.7, 0.5), to: terminalPoint(0.35, 0.5), hold: 0.02, velocity: .slow)
        waitProbe("the cursor moved left", timeout: 5) { ($0["cursorX"] as? Int ?? x0) < x0 }
        beat()
        shot("02-cursor-swipe")
        key("ctrl-c").tap()
        key("hide-keyboard").tap()
        beat()

        // A swipe on the tab bar: the next tab.
        if tabs > 1 {
            let t0 = int("tab")
            let bar = web.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0)).withOffset(CGVector(dx: 0, dy: 22))
            drag(from: bar.withOffset(CGVector(dx: 120, dy: 0)), to: bar.withOffset(CGVector(dx: -120, dy: 0)), hold: 0.02, velocity: .fast)
            p = waitProbe("another tab", timeout: 5) { ($0["tab"] as? Int ?? t0) != t0 }
            beat(1.5)
            shot("02-next-tab")
            drag(from: bar.withOffset(CGVector(dx: -120, dy: 0)), to: bar.withOffset(CGVector(dx: 120, dy: 0)), hold: 0.02, velocity: .fast)
            waitProbe("the first tab again", timeout: 5) { ($0["tab"] as? Int ?? -1) == t0 }
            beat()
        }

        // In from the left edge: the drawer with the workspaces.
        drag(from: web.coordinate(withNormalizedOffset: CGVector(dx: 0, dy: 0.5)).withOffset(CGVector(dx: 2, dy: 0)), to: terminalPoint(0.8, 0.5), hold: 0.02)
        waitProbe("the drawer", timeout: 5) { ($0["drawer"] as? Bool) == true }
        beat(1.5)
        shot("02-drawer")
        drag(from: terminalPoint(0.7, 0.5), to: terminalPoint(0.05, 0.5), hold: 0.02)
        waitProbe("the drawer closed", timeout: 5) { ($0["drawer"] as? Bool) == false }
        beat()
    }
}
